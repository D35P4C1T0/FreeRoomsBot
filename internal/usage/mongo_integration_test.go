package usage

import (
	"context"
	"fmt"
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

func TestIntegrationLegacyLogRetention(t *testing.T) {
	client := testIntegrationDB(t)
	ctx := context.Background()
	logs := client.Database(databaseName).Collection("logs")
	if _, err := logs.InsertOne(ctx, bson.M{"At": time.Now().UTC(), "ChatId": 42}); err != nil {
		t.Fatal(err)
	}
	if _, err := logs.Indexes().CreateOne(ctx, mongo.IndexModel{Keys: bson.D{{Key: "At", Value: -1}}}); err != nil {
		t.Fatal(err)
	}

	start := func(retention time.Duration) {
		t.Helper()
		svc, err := Open(ctx, mustEnv(t, "MONGO_TEST_URI"), retention)
		if err != nil {
			t.Fatal(err)
		}
		defer svc.Close(ctx)
		checkTTL(t, logs, "At_-1", int32(retention/time.Second))
		checkTTL(t, svc.usage, "LastSeen_1", int32(retention/time.Second))
	}
	start(90 * 24 * time.Hour)
	start(7 * 24 * time.Hour)
	start(7 * 24 * time.Hour)
	count, err := logs.CountDocuments(ctx, bson.M{})
	if err != nil || count != 1 {
		t.Fatalf("legacy log count = %d, error = %v", count, err)
	}
}

func checkTTL(t *testing.T, collection *mongo.Collection, name string, seconds int32) {
	t.Helper()
	indexes, err := collection.Indexes().ListSpecifications(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, index := range indexes {
		if index.Name == name && index.ExpireAfterSeconds != nil && *index.ExpireAfterSeconds == seconds {
			return
		}
	}
	t.Fatalf("missing %s index with %ds expiry on %s", name, seconds, collection.Name())
}

func TestIntegrationConcurrentUsage(t *testing.T) {
	client := testIntegrationDB(t)
	ctx := context.Background()
	svc, err := Open(ctx, mustEnv(t, "MONGO_TEST_URI"), 90*24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	defer svc.Close(ctx)

	collections, err := client.Database(databaseName).ListCollectionNames(ctx, bson.M{})
	if err != nil || len(collections) != 1 || collections[0] != "usage" {
		t.Fatalf("collections = %v, error = %v", collections, err)
	}

	const goroutines = 50
	var wg sync.WaitGroup
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := svc.RecordInteraction(ctx, 42, Chat{ID: 42, Type: "Private"}); err != nil {
				t.Errorf("concurrent RecordInteraction: %v", err)
			}
		}()
	}
	wg.Wait()

	usage := svc.usage
	var user userUsage
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

	svc, err := Open(ctx, mustEnv(t, "MONGO_TEST_URI"), 90*24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = svc.Close(context.Background()) }()

	if err := svc.RecordInteraction(ctx, 7, Chat{ID: 7, Type: "Private"}); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	svc.Handler().ServeHTTP(rec, localUsageRequest("GET", "/usage"))
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
		userUsage{ID: id, FirstSeen: old, LastSeen: now, TotalInteractions: 10, Days: map[string]int64{old.Format(usageDayLayout): 9, now.Format(usageDayLayout): 1}, Chats: map[string]chatRef{"old": {LastAt: old}, "current": {LastAt: now, Title: "$literal title"}}},
		userUsage{ID: "expired", FirstSeen: old, LastSeen: old, TotalInteractions: 500},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := usage.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "ExpiresAt", Value: 1}},
		Options: options.Index().SetExpireAfterSeconds(0),
	}); err != nil {
		t.Fatal(err)
	}
	svc, err := Open(ctx, mustEnv(t, "MONGO_TEST_URI"), 90*24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	defer svc.Close(ctx)
	var migrated userUsage
	if err := usage.FindOne(ctx, bson.M{"_id": id}).Decode(&migrated); err != nil {
		t.Fatal(err)
	}
	if len(migrated.Days) != 2 || len(migrated.Chats) != 2 || migrated.TotalInteractions != 10 {
		t.Fatalf("startup changed usage history: %#v", migrated)
	}
	page, err := svc.usageDashboard(ctx, usageQuery{sort: "total", page: 1}, now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if page.Summary.Users != 1 || len(page.Rows) != 1 || len(page.Rows[0].Chats) != 1 || page.Rows[0].Chats[0].Title != "$literal title" {
		t.Fatalf("dashboard exposed expired usage: %+v", page)
	}
	if err := svc.RecordInteraction(ctx, 101, Chat{ID: -12, Type: "Group", Title: "$secret"}); err != nil {
		t.Fatal(err)
	}
	migrated = userUsage{}
	if err := usage.FindOne(ctx, bson.M{"_id": id}).Decode(&migrated); err != nil {
		t.Fatal(err)
	}
	if len(migrated.Days) != 1 || len(migrated.Chats) != 3 || migrated.TotalInteractions != 11 || migrated.Days[now.Format(usageDayLayout)] != 2 || migrated.Chats[pseudonymousUsageID(-12)].Title != "$secret" {
		t.Fatalf("bad atomic update: %#v", migrated)
	}
	docs := make([]interface{}, 510)
	for i := range docs {
		docs[i] = userUsage{ID: fmt.Sprintf("%016x", i), FirstSeen: now, LastSeen: now, TotalInteractions: 1, Days: map[string]int64{now.Format(usageDayLayout): 1}}
	}
	if _, err := usage.InsertMany(ctx, docs); err != nil {
		t.Fatal(err)
	}
	page, err = svc.usageDashboard(ctx, usageQuery{sort: "total", page: 1}, now.Add(time.Second))
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
	checkTTL(t, usage, "LastSeen_1", int32(90*24*time.Hour/time.Second))
	indexes, err := usage.Indexes().ListSpecifications(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, index := range indexes {
		if index.Name == "ExpiresAt_1" {
			t.Fatal("legacy TTL index is still present")
		}
	}
}
