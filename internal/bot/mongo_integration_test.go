package bot

import (
	"context"
	"fmt"
	"log/slog"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// testIntegrationDB connects to a real MongoDB instance when MONGO_TEST_URI is
// set (for example mongodb://127.0.0.1:27099) and verifies the production
// migration and flood-safety behavior against it. Without the variable the
// tests skip, so normal CI needs no database.
func testIntegrationDB(t *testing.T) *mongo.Client {
	t.Helper()
	uri := os.Getenv("MONGO_TEST_URI")
	if uri == "" {
		t.Skip("MONGO_TEST_URI not set; skipping MongoDB integration test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client, err := mongo.Connect(ctx, options.Client().ApplyURI(uri))
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	if err := client.Ping(ctx, nil); err != nil {
		t.Fatalf("ping: %v", err)
	}
	t.Cleanup(func() {
		_ = client.Database(databaseName).Drop(context.Background())
		_ = client.Disconnect(context.Background())
	})
	return client
}

// TestIntegrationLegacyMigration seeds the logs collection exactly like the
// previous release would have left it (one document per request, including
// two documents with an identical request tuple) and verifies that startup
// creates the new indexes without failure and the new bucket upserts work.
func TestIntegrationLegacyMigration(t *testing.T) {
	client := testIntegrationDB(t)
	ctx := context.Background()
	logs := client.Database(databaseName).Collection("logs")

	now := time.Now().UTC()
	legacyAt := now.Add(-48 * time.Hour)
	hourBoundary := now.Add(-24 * time.Hour).Truncate(time.Hour)
	currentBucket := now.Truncate(time.Hour)
	// Seed raw legacy-shaped documents exactly as the previous release wrote
	// them: no Count field at all.
	legacy := []any{
		// Two identical legacy request tuples must not break index creation.
		bson.M{"ChatId": int64(1), "At": legacyAt, "RequestType": int32(RequestMessage), "Department": "povo", "AvailabilityType": int32(AvailabilityFree)},
		bson.M{"ChatId": int64(1), "At": legacyAt, "RequestType": int32(RequestMessage), "Department": "povo", "AvailabilityType": int32(AvailabilityFree)},
		// A legacy timestamp exactly on an hour boundary shares every bucket
		// key with future bucket documents except Count.
		bson.M{"ChatId": int64(1), "At": hourBoundary, "RequestType": int32(RequestMessage), "Department": "povo", "AvailabilityType": int32(AvailabilityFree)},
		// Worst case: a legacy request inside the current hour bucket. A
		// sparse index would make the first new bucket write collide with it;
		// the partial index excludes the legacy document instead.
		bson.M{"ChatId": int64(1), "At": currentBucket, "RequestType": int32(RequestMessage), "Department": "povo", "AvailabilityType": int32(AvailabilityFree)},
	}
	if _, err := logs.InsertMany(ctx, legacy); err != nil {
		t.Fatalf("seed legacy logs: %v", err)
	}

	svc, err := NewDatabaseService(ctx, mustEnv(t, "MONGO_TEST_URI"), 90*24*time.Hour, slog.Default())
	if err != nil {
		t.Fatalf("NewDatabaseService on legacy data must not fail: %v", err)
	}
	defer svc.Close(context.Background())

	// TTL index must exist with the requested expiry.
	indexes, err := logs.Indexes().ListSpecifications(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var ttlFound bool
	for _, spec := range indexes {
		if spec.Name == "At_-1" && spec.ExpireAfterSeconds != nil && *spec.ExpireAfterSeconds == int32(90*24*time.Hour/time.Second) {
			ttlFound = true
		}
	}
	if !ttlFound {
		t.Fatalf("TTL index At_-1 with 90d expiry not found in %d specs", len(indexes))
	}

	// A new bucket write must succeed and create its own document without
	// touching or colliding with the legacy hour-boundary document.
	if err := svc.LogUsage(ctx, 1, RequestMessage, AvailabilityFree, &Department{Slug: "povo"}); err != nil {
		t.Fatalf("LogUsage after migration: %v", err)
	}
	bucketAt := time.Now().UTC().Truncate(time.Hour)
	count, err := logs.CountDocuments(ctx, bson.M{
		"ChatId": 1, "At": bucketAt, "Department": "povo",
		"AvailabilityType": AvailabilityFree, "RequestType": RequestMessage,
		"Count": bson.M{"$exists": true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("expected exactly 1 bucket document, got %d", count)
	}
	// The legacy document in the same hour bucket must still coexist.
	total, err := logs.CountDocuments(ctx, bson.M{
		"ChatId": 1, "At": bucketAt, "Department": "povo",
		"AvailabilityType": AvailabilityFree, "RequestType": RequestMessage,
	})
	if err != nil {
		t.Fatal(err)
	}
	if total != 2 {
		t.Fatalf("legacy and bucket documents must coexist, got %d documents", total)
	}
}

// TestIntegrationConcurrentFlood hammers the same chat with concurrent
// identical requests, forcing the duplicate-key
// retry paths, and verifies the bucket count stays exact.
func TestIntegrationConcurrentFlood(t *testing.T) {
	client := testIntegrationDB(t)
	ctx := context.Background()

	svc, err := NewDatabaseService(ctx, mustEnv(t, "MONGO_TEST_URI"), 90*24*time.Hour, slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	defer svc.Close(context.Background())

	const goroutines = 50
	var wg sync.WaitGroup
	errCh := make(chan error, goroutines)
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := svc.LogUsage(ctx, 42, RequestCallbackQuery, AvailabilityOccupied, &Department{Slug: "mesiano"}); err != nil {
				errCh <- err
			}
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Fatalf("concurrent LogUsage returned an error: %v", err)
	}

	logs := client.Database(databaseName).Collection("logs")
	bucketAt := time.Now().UTC().Truncate(time.Hour)
	var doc LogEntity
	err = logs.FindOne(ctx, bson.M{
		"ChatId": 42, "At": bucketAt, "Department": "mesiano",
		"AvailabilityType": AvailabilityOccupied, "RequestType": RequestCallbackQuery,
	}).Decode(&doc)
	if err != nil {
		t.Fatalf("bucket document not found: %v", err)
	}
	if doc.Count != goroutines {
		t.Fatalf("bucket Count = %d, want %d", doc.Count, goroutines)
	}

	// Same flood against the per-user usage document.
	var wg2 sync.WaitGroup
	for i := 0; i < goroutines; i++ {
		wg2.Add(1)
		go func() {
			defer wg2.Done()
			if err := svc.RecordInteraction(ctx, UsageChat{ID: 42, Type: "Private"}); err != nil {
				t.Errorf("concurrent RecordInteraction: %v", err)
			}
		}()
	}
	wg2.Wait()

	usage := client.Database(databaseName).Collection("usage")
	var user UsageEntity
	if err := usage.FindOne(ctx, bson.M{"_id": pseudonymousUsageID(42)}).Decode(&user); err != nil {
		t.Fatalf("usage document not found: %v", err)
	}
	if user.TotalInteractions != goroutines {
		t.Fatalf("TotalInteractions = %d, want %d", user.TotalInteractions, goroutines)
	}
	if user.FirstSeen.IsZero() || user.LastSeen.IsZero() {
		t.Fatalf("FirstSeen/LastSeen must be set, got %v/%v", user.FirstSeen, user.LastSeen)
	}
}

// TestIntegrationViewerEndToEnd verifies the localhost viewer renders real
// database content.
func TestIntegrationViewerEndToEnd(t *testing.T) {
	testIntegrationDB(t)
	ctx := context.Background()

	svc, err := NewDatabaseService(ctx, mustEnv(t, "MONGO_TEST_URI"), 90*24*time.Hour, slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = svc.Close(context.Background()) }()

	if err := svc.RecordInteraction(ctx, UsageChat{ID: 7, Type: "Private"}); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	newUsageViewerHandler(svc).ServeHTTP(rec, localUsageRequest("GET", "/usage"))
	if rec.Code != 200 {
		t.Fatalf("viewer status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, pseudonymousUsageID(7)) || !strings.Contains(body, "Bot usage") {
		t.Fatalf("viewer body missing expected content")
	}
}

func mustEnv(t *testing.T, key string) string {
	t.Helper()
	v := os.Getenv(key)
	if v == "" {
		t.Fatalf("%s must be set", key)
	}
	return v
}

func TestIntegrationUsageRetentionAndPagination(t *testing.T) {
	client := testIntegrationDB(t)
	ctx := context.Background()
	usage := client.Database(databaseName).Collection("usage")
	now := time.Now().UTC()
	old := now.Add(-120 * 24 * time.Hour)
	id := pseudonymousUsageID(101)
	_, err := usage.InsertMany(ctx, []interface{}{
		UsageEntity{ID: id, FirstSeen: old, LastSeen: now, TotalInteractions: 10, Days: map[string]int64{old.Format(usageDayLayout): 9, now.Format(usageDayLayout): 1}, Chats: map[string]UsageChatRef{"old": {LastAt: old}, "current": {LastAt: now, Title: "$literal title"}}},
		UsageEntity{ID: "expired", FirstSeen: old, LastSeen: old, TotalInteractions: 500},
	})
	if err != nil {
		t.Fatal(err)
	}
	svc, err := NewDatabaseService(ctx, mustEnv(t, "MONGO_TEST_URI"), 90*24*time.Hour, slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	defer svc.Close(ctx)
	var migrated UsageEntity
	if err := usage.FindOne(ctx, bson.M{"_id": id}).Decode(&migrated); err != nil {
		t.Fatal(err)
	}
	if len(migrated.Days) != 1 || len(migrated.Chats) != 1 || migrated.TotalInteractions != 10 {
		t.Fatalf("bad migration: %#v", migrated)
	}
	var expiry struct {
		ExpiresAt time.Time `bson:"ExpiresAt"`
	}
	if err := usage.FindOne(ctx, bson.M{"_id": id}).Decode(&expiry); err != nil {
		t.Fatal(err)
	}
	if expiry.ExpiresAt.Sub(now.Add(90*24*time.Hour)) > time.Second || expiry.ExpiresAt.Before(now.Add(90*24*time.Hour-time.Second)) {
		t.Fatalf("bad expiry %v", expiry.ExpiresAt)
	}
	// Old history added after startup must also be pruned on interaction.
	if _, err := usage.UpdateOne(ctx, bson.M{"_id": id}, bson.M{"$set": bson.M{"Days." + old.Format(usageDayLayout): 9, "Chats.old": UsageChatRef{LastAt: old}}}); err != nil {
		t.Fatal(err)
	}
	if err := svc.RecordInteraction(ctx, UsageChat{ID: 101, Type: "Private"}, UsageChat{ID: -12, Type: "Group", Title: "$secret"}); err != nil {
		t.Fatal(err)
	}
	if err := usage.FindOne(ctx, bson.M{"_id": id}).Decode(&migrated); err != nil {
		t.Fatal(err)
	}
	if len(migrated.Days) != 1 || len(migrated.Chats) != 3 || migrated.TotalInteractions != 11 || migrated.Days[now.Format(usageDayLayout)] != 2 || migrated.Chats[pseudonymousUsageID(-12)].Title != "$secret" {
		t.Fatalf("bad atomic update: %#v", migrated)
	}
	docs := make([]interface{}, 510)
	for i := range docs {
		docs[i] = UsageEntity{ID: fmt.Sprintf("%016x", i), FirstSeen: now, LastSeen: now, TotalInteractions: 1, Days: map[string]int64{now.Format(usageDayLayout): 1}}
	}
	if _, err := usage.InsertMany(ctx, docs); err != nil {
		t.Fatal(err)
	}
	page, err := svc.usageDashboard(ctx, usageQuery{sort: "total", page: 1}, now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if page.Summary.Users != 511 || page.Summary.Interactions != 521 || page.Summary.Today != 511 || len(page.Rows) != 50 || page.Next == "" {
		t.Fatalf("wrong global totals/pagination: %+v, rows %d", page.Summary, len(page.Rows))
	}
	if page.Bars[29].Count != 512 {
		t.Fatalf("daily total %d", page.Bars[29].Count)
	}
	filtered, err := svc.usageDashboard(ctx, usageQuery{search: id, sort: "recent", page: 1}, now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if len(filtered.Rows) != 1 || filtered.Summary != page.Summary {
		t.Fatal("filter altered global summary")
	}
	indexes, err := usage.Indexes().ListSpecifications(ctx)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, index := range indexes {
		if index.Name == "ExpiresAt_1" && index.ExpireAfterSeconds != nil && *index.ExpireAfterSeconds == 0 {
			found = true
		}
	}
	if !found {
		t.Fatal("missing usage TTL")
	}
}
