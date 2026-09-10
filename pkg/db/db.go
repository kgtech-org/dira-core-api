// Package db provides the shared MongoDB client, typed collection access and
// a transaction helper. Critical operations (tokens, payments) must go through
// WithTransaction.
package db

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.mongodb.org/mongo-driver/mongo/readpref"
)

type Mongo struct {
	Client *mongo.Client
	DB     *mongo.Database

	// txSupported is false on standalone MongoDB (e.g. the shared dev
	// container), where transactions are unavailable.
	txSupported bool
}

// Connect opens the MongoDB client, pings the server and probes replica-set
// support (required for transactions).
func Connect(ctx context.Context, uri, dbName string) (*Mongo, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	client, err := mongo.Connect(ctx, options.Client().ApplyURI(uri).SetConnectTimeout(10*time.Second))
	if err != nil {
		return nil, fmt.Errorf("db: connect: %w", err)
	}
	if err := client.Ping(ctx, readpref.Primary()); err != nil {
		return nil, fmt.Errorf("db: ping: %w", err)
	}

	var hello struct {
		SetName string `bson:"setName"`
	}
	if err := client.Database("admin").RunCommand(ctx, bson.D{{Key: "hello", Value: 1}}).Decode(&hello); err != nil {
		return nil, fmt.Errorf("db: hello: %w", err)
	}
	if hello.SetName == "" {
		slog.Warn("db: standalone MongoDB detected, transactions disabled (use a replica set in production)")
	}

	return &Mongo{Client: client, DB: client.Database(dbName), txSupported: hello.SetName != ""}, nil
}

func (m *Mongo) Close(ctx context.Context) error { return m.Client.Disconnect(ctx) }

// Collection returns a handle to the named collection.
func (m *Mongo) Collection(name string) *mongo.Collection { return m.DB.Collection(name) }

// WithTransaction opens a session, runs fn inside a MongoDB transaction and
// commits, or aborts if fn returns an error. On standalone MongoDB (dev) it
// runs fn without a transaction.
func (m *Mongo) WithTransaction(ctx context.Context, fn func(ctx context.Context) error) error {
	if !m.txSupported {
		return fn(ctx)
	}
	session, err := m.Client.StartSession()
	if err != nil {
		return fmt.Errorf("db: start session: %w", err)
	}
	defer session.EndSession(ctx)

	_, err = session.WithTransaction(ctx, func(sc mongo.SessionContext) (any, error) {
		return nil, fn(sc)
	})
	return err
}
