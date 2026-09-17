package bot

import (
	"context"
	"log/slog"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	tele "gopkg.in/telebot.v3"
)

// DatabaseService stores chat metadata and usage logs in MongoDB.
//
// A DatabaseService owns its mongo.Client and must be closed when the
// application shuts down.
type DatabaseService struct {
	client  *mongo.Client
	chats   *mongo.Collection
	logs    *mongo.Collection
	usage   *mongo.Collection
	logGate *interactionGate
	logger  *slog.Logger
}

// ChatEntity is the MongoDB representation of a Telegram chat or private user.
//
// BSON field names intentionally preserve the previous C# schema casing.
type ChatEntity struct {
	// ChatID is the Telegram chat or user identifier.
	ChatID int64 `bson:"ChatId"`
	// Type is the Telegram chat type using the legacy persisted casing.
	Type string `bson:"Type"`
	// Title is the group, supergroup, or channel title when present.
	Title string `bson:"Title"`
	// Username is the Telegram username when present.
	Username string `bson:"Username"`
	// FirstName is the Telegram first name when present.
	FirstName string `bson:"FirstName"`
	// LastName is the Telegram last name when present.
	LastName string `bson:"LastName"`
	// UpdatedAt is the last time the chat metadata was observed.
	UpdatedAt time.Time `bson:"UpdatedAt"`
}

// LogEntity is the MongoDB representation of a room availability request
// bucket.
//
// BSON field names intentionally preserve the previous C# schema casing.
// Requests are aggregated per chat and UTC hour so a flood of requests cannot
// grow the collection without bound; Count holds the bucketed request count.
type LogEntity struct {
	// ChatID is the Telegram chat identifier that made the request.
	ChatID int64 `bson:"ChatId"`
	// At is the UTC hour bucket this document aggregates.
	At time.Time `bson:"At"`
	// RequestType identifies whether the request came from a message or callback.
	RequestType RequestType `bson:"RequestType"`
	// Department is the department slug requested by the user.
	Department string `bson:"Department"`
	// AvailabilityType is the availability view requested by the user.
	AvailabilityType AvailabilityType `bson:"AvailabilityType"`
	// Count is the number of requests aggregated into this bucket.
	Count int64 `bson:"Count"`
}

const databaseName = "free_classrooms_bot_unitn"

// NewDatabaseService connects to MongoDB, verifies connectivity, and prepares
// indexes required by the bot.
//
// The supplied context is bounded by an internal startup timeout. If pinging
// fails, the partially opened client is disconnected before the error is
// returned. Index failures are logged as warnings and do not prevent startup:
// the bot runs correctly without them, so a deploy can never crash-loop over
// index state on an existing database. A nil logger falls back to slog.Default.
func NewDatabaseService(ctx context.Context, connectionString string, logRetention time.Duration, logger *slog.Logger) (*DatabaseService, error) {
	if logger == nil {
		logger = slog.Default()
	}
	connectCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	client, err := mongo.Connect(connectCtx, options.Client().ApplyURI(connectionString))
	if err != nil {
		return nil, err
	}
	if err := client.Ping(connectCtx, nil); err != nil {
		_ = client.Disconnect(context.Background())
		return nil, err
	}

	db := client.Database(databaseName)
	service := &DatabaseService{
		client:  client,
		chats:   db.Collection("chats"),
		logs:    db.Collection("logs"),
		usage:   db.Collection("usage"),
		logGate: newInteractionGate(time.Second),
		logger:  logger,
	}
	service.ensureIndexes(connectCtx, logRetention)
	return service, nil
}

// ensureIndexes creates or refreshes the indexes used by the bot.
//
// Every failure is logged and swallowed: indexes only affect query efficiency,
// race-safety of bucket upserts, and TTL expiry, none of which justify a
// crash loop on an existing deployment.
func (d *DatabaseService) ensureIndexes(ctx context.Context, logRetention time.Duration) {
	if _, err := d.usage.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "TotalInteractions", Value: -1}},
		Options: options.Index().SetName("TotalInteractions_-1"),
	}); err != nil {
		d.logger.Warn("usage index creation failed", "error", err)
	}

	// Bucket documents are made race-safe with a unique index. The partial
	// filter excludes legacy per-request documents (which have no Count field)
	// so index creation can never fail on pre-existing data, unlike a sparse
	// index which would still index legacy documents.
	if _, err := d.logs.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys: bson.D{
			{Key: "ChatId", Value: 1},
			{Key: "At", Value: 1},
			{Key: "RequestType", Value: 1},
			{Key: "Department", Value: 1},
			{Key: "AvailabilityType", Value: 1},
		},
		Options: options.Index().
			SetUnique(true).
			SetPartialFilterExpression(bson.M{"Count": bson.M{"$exists": true}}),
	}); err != nil {
		d.logger.Warn("log bucket index creation failed", "error", err)
	}

	retentionSeconds := int32(logRetention / time.Second)
	if _, err := d.chats.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "ChatId", Value: 1}},
		Options: options.Index().SetUnique(true),
	}); err != nil {
		d.logger.Warn("chats index creation failed", "error", err)
	}
	// Older releases created the same index without expiry. Remove it before
	// creating the TTL index so existing deployments gain bounded retention.
	indexes, err := d.logs.Indexes().ListSpecifications(ctx)
	if err != nil {
		d.logger.Warn("logs index listing failed", "error", err)
		return
	}
	for _, index := range indexes {
		if index.Name == "At_-1" && (index.ExpireAfterSeconds == nil || *index.ExpireAfterSeconds != retentionSeconds) {
			if _, err := d.logs.Indexes().DropOne(ctx, index.Name); err != nil {
				d.logger.Warn("logs TTL index drop failed", "error", err)
				return
			}
		}
	}
	if _, err := d.logs.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys: bson.D{{Key: "At", Value: -1}},
		Options: options.Index().
			SetName("At_-1").
			SetExpireAfterSeconds(retentionSeconds),
	}); err != nil {
		d.logger.Warn("logs TTL index creation failed", "error", err)
	}
}

// Close disconnects the underlying MongoDB client.
func (d *DatabaseService) Close(ctx context.Context) error {
	return d.client.Disconnect(ctx)
}

// LogUser upserts private Telegram user metadata into the chats collection.
//
// A nil user is ignored and returns nil. The database operation has its own
// short timeout derived from ctx.
func (d *DatabaseService) LogUser(ctx context.Context, user *tele.User) error {
	if user == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	updatedAt := time.Now().UTC()
	_, err := d.chats.UpdateOne(
		ctx,
		bson.M{"ChatId": int64(user.ID)},
		bson.M{
			"$set": bson.M{
				"Username":  emptyStringToNil(user.Username),
				"FirstName": emptyStringToNil(user.FirstName),
				"LastName":  emptyStringToNil(user.LastName),
				"UpdatedAt": updatedAt,
			},
			"$setOnInsert": bson.M{
				"ChatId": int64(user.ID),
				"Type":   "Private",
			},
		},
		options.Update().SetUpsert(true),
	)
	return err
}

// LogChat upserts Telegram chat metadata into the chats collection.
//
// A nil chat is ignored and returns nil. The database operation has its own
// short timeout derived from ctx.
func (d *DatabaseService) LogChat(ctx context.Context, chat *tele.Chat) error {
	if chat == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	entity := bson.M{
		"ChatId":    chat.ID,
		"Type":      chatTypeString(chat.Type),
		"Title":     emptyStringToNil(chat.Title),
		"Username":  emptyStringToNil(chat.Username),
		"FirstName": emptyStringToNil(chat.FirstName),
		"LastName":  emptyStringToNil(chat.LastName),
		"UpdatedAt": time.Now().UTC(),
	}
	_, err := d.chats.UpdateOne(
		ctx,
		bson.M{"ChatId": chat.ID},
		bson.M{"$set": entity},
		options.Update().SetUpsert(true),
	)
	return err
}

func emptyStringToNil(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func chatTypeString(chatType tele.ChatType) string {
	switch chatType {
	case tele.ChatPrivate:
		return "Private"
	case tele.ChatGroup:
		return "Group"
	case tele.ChatSuperGroup:
		return "Supergroup"
	case tele.ChatChannel:
		return "Channel"
	default:
		return string(chatType)
	}
}

// LogUsage records a room availability request in an hourly per-chat bucket.
//
// Instead of inserting one document per request, requests are aggregated per
// chat, UTC hour, department, availability view, and request type so a flood
// of requests (for example from abusive automation) cannot grow the logs
// collection without bound: at most a few hundred bucket documents can be
// created per chat per hour regardless of request volume. Count accumulates
// the request total per bucket.
//
// A per-chat interaction gate additionally throttles the writes themselves,
// collapsing sub-second request storms into at most one write per second per
// chat. The database operation has its own short timeout derived from ctx.
func (d *DatabaseService) LogUsage(ctx context.Context, chatID int64, requestType RequestType, availabilityType AvailabilityType, dep *Department) error {
	if !d.logGate.Allow(chatID) {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	bucket := time.Now().UTC().Truncate(time.Hour)
	filter := bson.M{
		"ChatId":           chatID,
		"At":               bucket,
		"RequestType":      requestType,
		"Department":       dep.Slug,
		"AvailabilityType": availabilityType,
		// Matching on Count excludes legacy per-request documents, which keeps
		// the sparse unique bucket index free of pre-existing data.
		"Count": bson.M{"$exists": true},
	}
	update := bson.M{"$inc": bson.M{"Count": 1}}
	_, err := d.logs.UpdateOne(ctx, filter, update, options.Update().SetUpsert(true))
	if err != nil && mongo.IsDuplicateKeyError(err) {
		// A concurrent request created the same bucket first; increment it.
		_, err = d.logs.UpdateOne(ctx, filter, update)
	}
	return err
}
