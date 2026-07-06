package bot

import (
	"context"
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
	client *mongo.Client
	chats  *mongo.Collection
	logs   *mongo.Collection
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

// LogEntity is the MongoDB representation of a room availability request.
//
// BSON field names intentionally preserve the previous C# schema casing.
type LogEntity struct {
	// ChatID is the Telegram chat identifier that made the request.
	ChatID int64 `bson:"ChatId"`
	// At is the UTC timestamp when the request was logged.
	At time.Time `bson:"At"`
	// RequestType identifies whether the request came from a message or callback.
	RequestType RequestType `bson:"RequestType"`
	// Department is the department slug requested by the user.
	Department string `bson:"Department"`
	// AvailabilityType is the availability view requested by the user.
	AvailabilityType AvailabilityType `bson:"AvailabilityType"`
}

const databaseName = "free_classrooms_bot_unitn"

// NewDatabaseService connects to MongoDB, verifies connectivity, and prepares
// indexes required by the bot.
//
// The supplied context is bounded by an internal startup timeout. If pinging or
// index creation fails, the partially opened client is disconnected before the
// error is returned.
func NewDatabaseService(ctx context.Context, connectionString string, logRetention time.Duration) (*DatabaseService, error) {
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
		client: client,
		chats:  db.Collection("chats"),
		logs:   db.Collection("logs"),
	}
	if err := service.ensureIndexes(connectCtx, logRetention); err != nil {
		_ = client.Disconnect(context.Background())
		return nil, err
	}
	return service, nil
}

func (d *DatabaseService) ensureIndexes(ctx context.Context, logRetention time.Duration) error {
	retentionSeconds := int32(logRetention / time.Second)
	if _, err := d.chats.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "ChatId", Value: 1}},
		Options: options.Index().SetUnique(true),
	}); err != nil {
		return err
	}
	// Older releases created the same index without expiry. Remove it before
	// creating the TTL index so existing deployments gain bounded retention.
	indexes, err := d.logs.Indexes().ListSpecifications(ctx)
	if err != nil {
		return err
	}
	for _, index := range indexes {
		if index.Name == "At_-1" && (index.ExpireAfterSeconds == nil || *index.ExpireAfterSeconds != retentionSeconds) {
			if _, err := d.logs.Indexes().DropOne(ctx, index.Name); err != nil {
				return err
			}
		}
	}
	if _, err := d.logs.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys: bson.D{{Key: "At", Value: -1}},
		Options: options.Index().
			SetName("At_-1").
			SetExpireAfterSeconds(retentionSeconds),
	}); err != nil {
		return err
	}
	return nil
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

// LogUsage inserts a usage record for a room availability request.
//
// The department must be non-nil. The database operation has its own short
// timeout derived from ctx.
func (d *DatabaseService) LogUsage(ctx context.Context, chatID int64, requestType RequestType, availabilityType AvailabilityType, dep *Department) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	_, err := d.logs.InsertOne(ctx, LogEntity{
		ChatID:           chatID,
		At:               time.Now().UTC(),
		RequestType:      requestType,
		Department:       dep.Slug,
		AvailabilityType: availabilityType,
	})
	return err
}
