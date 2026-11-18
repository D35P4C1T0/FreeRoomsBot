package usage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// Chat describes a Telegram chat before its ID is hashed.
type Chat struct {
	ID          int64
	Type, Title string
}

type chatRef struct {
	Type   string    `bson:"Type"`
	Title  string    `bson:"Title"`
	LastAt time.Time `bson:"LastAt"`
}

type userUsage struct {
	ID                string             `bson:"_id"`
	FirstSeen         time.Time          `bson:"FirstSeen"`
	LastSeen          time.Time          `bson:"LastSeen"`
	TotalInteractions int64              `bson:"TotalInteractions"`
	Days              map[string]int64   `bson:"Days"`
	Chats             map[string]chatRef `bson:"Chats"`
}

const usageDayLayout = "2006-01-02"

// pseudonymousUsageID derives a stable pseudonymous internal identifier from a
// Telegram chat or user identifier.
//
// This is a stable label, not anonymization: known IDs can be matched by hashing.
func pseudonymousUsageID(telegramID int64) string {
	sum := sha256.Sum256([]byte("usage:v1:" + strconv.FormatInt(telegramID, 10)))
	return hex.EncodeToString(sum[:8])
}

func pruneUsageHistory(cutoff time.Time) bson.D {
	filterMap := func(field string, condition bson.M) bson.M {
		return bson.M{"$arrayToObject": bson.M{"$filter": bson.M{
			"input": bson.M{"$objectToArray": bson.M{"$ifNull": bson.A{"$" + field, bson.M{}}}},
			"as":    "entry", "cond": condition,
		}}}
	}
	return bson.D{{Key: "$set", Value: bson.M{
		"Days":  filterMap("Days", bson.M{"$gte": bson.A{"$$entry.k", cutoff.Format(usageDayLayout)}}),
		"Chats": filterMap("Chats", bson.M{"$gte": bson.A{"$$entry.v.LastAt", cutoff}}),
	}}}
}

// RecordInteraction atomically counts every successful write and prunes history.
func (d *Store) RecordInteraction(ctx context.Context, userID int64, chat Chat) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	now := time.Now().UTC()
	day := now.Format(usageDayLayout)
	id, chatID := pseudonymousUsageID(userID), pseudonymousUsageID(chat.ID)
	chats := bson.M{chatID: chatRef{Type: chat.Type, Title: chat.Title, LastAt: now}}
	if chat.ID != userID {
		chats[id] = chatRef{Type: "Private", LastAt: now}
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
	update = append(update, pruneUsageHistory(now.Add(-d.retention)))
	filter := bson.M{"_id": id}
	_, err := d.usage.UpdateOne(ctx, filter, update, options.Update().SetUpsert(true))
	if mongo.IsDuplicateKeyError(err) {
		_, err = d.usage.UpdateOne(ctx, filter, update, options.Update().SetUpsert(true))
	}
	return err
}
