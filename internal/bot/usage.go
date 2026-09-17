package bot

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	tele "gopkg.in/telebot.v3"
)

// UsageChat describes a Telegram chat identity for usage tracking.
//
// Telegram identifiers are never persisted here; they are reduced to a
// pseudonymous internal identifier by pseudonymousUsageID.
type UsageChat struct {
	// ID is the raw Telegram chat or user identifier used only in-memory to
	// derive the pseudonymous identifier.
	ID int64
	// Type is a human-readable chat type such as "Private" or "Group".
	Type string
	// Title is a display label (group title) when present; it stays empty for
	// private users so no personal metadata is stored.
	Title string
}

// UsageChatRef is the persisted, pseudonymous representation of a chat a user
// interacted from.
type UsageChatRef struct {
	// Type is a human-readable chat type such as "Private" or "Group".
	Type string `bson:"Type"`
	// Title is a display label for group chats; empty for private users.
	Title string `bson:"Title"`
	// LastAt is the most recent interaction time observed from this chat.
	LastAt time.Time `bson:"LastAt"`
}

// UsageEntity is the MongoDB representation of a user's interaction statistics.
//
// The document _id is a pseudonymous internal identifier derived from the
// Telegram user ID; no Telegram identifier is stored in this collection.
type UsageEntity struct {
	// ID is the pseudonymous internal user identifier.
	ID string `bson:"_id"`
	// FirstSeen is the first interaction time in UTC.
	FirstSeen time.Time `bson:"FirstSeen"`
	// LastSeen is the most recent interaction time in UTC.
	LastSeen time.Time `bson:"LastSeen"`
	// TotalInteractions counts every observed interaction.
	TotalInteractions int64 `bson:"TotalInteractions"`
	// Days maps a UTC day (2006-01-02) to the interaction count for that day.
	Days map[string]int64 `bson:"Days"`
	// Chats maps pseudonymous chat identifiers to their metadata.
	Chats map[string]UsageChatRef `bson:"Chats"`
}

// usageDayLayout is the UTC day key used in UsageEntity.Days.
const usageDayLayout = "2006-01-02"

// pseudonymousUsageID derives a stable pseudonymous internal identifier from a
// Telegram chat or user identifier.
//
// This is a stable label, not anonymization: known IDs can be matched by hashing.
func pseudonymousUsageID(telegramID int64) string {
	sum := sha256.Sum256([]byte("usage:v1:" + strconv.FormatInt(telegramID, 10)))
	return hex.EncodeToString(sum[:8])
}

// usageChatLabel returns the display type label for a Telegram chat type.
func usageChatLabel(chatType tele.ChatType) string {
	if chatType == tele.ChatPrivate {
		return "Private"
	}
	return chatTypeString(chatType)
}

// usageChatForMessage builds a UsageChat for a Telegram message chat.
//
// Private chats get no title so no personal metadata is persisted; group-like
// chats keep their public title.
func usageChatForMessage(chat *tele.Chat) UsageChat {
	title := ""
	if chat.Type != tele.ChatPrivate {
		title = chat.Title
	}
	return UsageChat{ID: chat.ID, Type: usageChatLabel(chat.Type), Title: title}
}

// usagePruneStages bounds retained day and chat history, including legacy data.
func usagePruneStages(now time.Time, retention time.Duration) mongo.Pipeline {
	cutoff := now.Add(-retention)
	filterMap := func(field string, condition bson.M) bson.M {
		return bson.M{"$arrayToObject": bson.M{"$filter": bson.M{
			"input": bson.M{"$objectToArray": bson.M{"$ifNull": bson.A{"$" + field, bson.M{}}}},
			"as":    "entry", "cond": condition,
		}}}
	}
	return mongo.Pipeline{bson.D{{Key: "$set", Value: bson.M{
		"Days":      filterMap("Days", bson.M{"$gte": bson.A{"$$entry.k", cutoff.Format(usageDayLayout)}}),
		"Chats":     filterMap("Chats", bson.M{"$gte": bson.A{"$$entry.v.LastAt", cutoff}}),
		"ExpiresAt": bson.M{"$add": bson.A{"$LastSeen", int64(retention / time.Millisecond)}},
	}}}}
}

// prepareUsage migrates legacy history and requires expiry to be installed.
func (d *DatabaseService) prepareUsage(ctx context.Context) error {
	if _, err := d.usage.UpdateMany(ctx, bson.M{}, usagePruneStages(time.Now().UTC(), d.retention)); err != nil {
		return err
	}
	_, err := d.usage.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys: bson.D{{Key: "ExpiresAt", Value: 1}}, Options: options.Index().SetExpireAfterSeconds(0),
	})
	return err
}

// RecordInteraction atomically counts every successful write and prunes history.
func (d *DatabaseService) RecordInteraction(ctx context.Context, primary UsageChat, extras ...UsageChat) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	now := time.Now().UTC()
	day := now.Format(usageDayLayout)
	chats := bson.M{}
	for _, chat := range append([]UsageChat{primary}, extras...) {
		chats[pseudonymousUsageID(chat.ID)] = UsageChatRef{Type: chat.Type, Title: chat.Title, LastAt: now}
	}
	update := mongo.Pipeline{bson.D{{Key: "$set", Value: bson.M{
		"TotalInteractions": bson.M{"$add": bson.A{bson.M{"$ifNull": bson.A{"$TotalInteractions", 0}}, 1}},
		"FirstSeen":         bson.M{"$min": bson.A{bson.M{"$ifNull": bson.A{"$FirstSeen", now}}, now}},
		"LastSeen":          bson.M{"$max": bson.A{bson.M{"$ifNull": bson.A{"$LastSeen", now}}, now}},
		"Days": bson.M{"$mergeObjects": bson.A{bson.M{"$ifNull": bson.A{"$Days", bson.M{}}}, bson.M{
			day: bson.M{"$add": bson.A{bson.M{"$ifNull": bson.A{"$Days." + day, 0}}, 1}},
		}}},
		"Chats": bson.M{"$mergeObjects": bson.A{bson.M{"$ifNull": bson.A{"$Chats", bson.M{}}}, bson.M{"$literal": chats}}},
	}}}}
	update = append(update, usagePruneStages(now, d.retention)...)
	filter := bson.M{"_id": pseudonymousUsageID(primary.ID)}
	_, err := d.usage.UpdateOne(ctx, filter, update, options.Update().SetUpsert(true))
	if mongo.IsDuplicateKeyError(err) {
		_, err = d.usage.UpdateOne(ctx, filter, update)
	}
	return err
}
