// Package usage records bot interactions in MongoDB and serves the private dashboard.
package usage

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const databaseName = "free_classrooms_bot_unitn"

type Store struct {
	client    *mongo.Client
	usage     *mongo.Collection
	retention time.Duration
}

func Open(ctx context.Context, uri string, retention time.Duration) (*Store, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	client, err := mongo.Connect(ctx, options.Client().ApplyURI(uri))
	if err != nil {
		return nil, err
	}
	db := client.Database(databaseName)
	service := &Store{client: client, usage: db.Collection("usage"), retention: retention}
	if err := service.prepareRetention(ctx, db); err != nil {
		_ = client.Disconnect(context.Background())
		return nil, fmt.Errorf("configure MongoDB retention: %w", err)
	}
	return service, nil
}

func (d *Store) Close(ctx context.Context) error {
	return d.client.Disconnect(ctx)
}

func (d *Store) prepareRetention(ctx context.Context, db *mongo.Database) error {
	seconds := int32(d.retention / time.Second)
	indexes, err := d.usage.Indexes().ListSpecifications(ctx)
	if err != nil {
		return err
	}
	if err := ensureTTL(ctx, d.usage, indexes, "LastSeen", 1, seconds); err != nil {
		return err
	}
	// Install LastSeen expiry first, then remove the index on obsolete deadlines.
	for _, index := range indexes {
		if index.Name == "ExpiresAt_1" {
			if _, err := d.usage.Indexes().DropOne(ctx, index.Name); err != nil {
				var commandErr mongo.CommandError
				if !errors.As(err, &commandErr) || commandErr.Code != 27 { // IndexNotFound
					return err
				}
			}
			break
		}
	}

	logs := db.Collection("logs")
	indexes, err = logs.Indexes().ListSpecifications(ctx)
	if err != nil || len(indexes) == 0 {
		return err
	}
	return ensureTTL(ctx, logs, indexes, "At", -1, seconds)
}

func ensureTTL(ctx context.Context, collection *mongo.Collection, indexes []*mongo.IndexSpecification, field string, order int, seconds int32) error {
	name := fmt.Sprintf("%s_%d", field, order)
	for _, index := range indexes {
		if index.Name != name {
			continue
		}
		if index.ExpireAfterSeconds != nil && *index.ExpireAfterSeconds == seconds {
			return nil
		}
		return collection.Database().RunCommand(ctx, bson.D{
			{Key: "collMod", Value: collection.Name()},
			{Key: "index", Value: bson.M{"name": name, "expireAfterSeconds": seconds}},
		}).Err()
	}
	_, err := collection.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: field, Value: order}},
		Options: options.Index().SetExpireAfterSeconds(seconds),
	})
	return err
}
