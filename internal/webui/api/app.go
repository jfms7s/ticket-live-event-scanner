// Package api implements the HTTP API served by web-ui-api for the
// web-ui-frontend to query events and notifications, and to trigger retries.
package api

import (
	"context"
	"database/sql"

	"github.com/nats-io/nats.go/jetstream"
)

// Publisher is an interface for publishing messages to JetStream.
type Publisher interface {
	Publish(ctx context.Context, subject string, data []byte, opts ...jetstream.PublishOpt) (*jetstream.PubAck, error)
}

// App holds dependencies for HTTP handlers.
type App struct {
	db *sql.DB
	js Publisher
}

// NewApp constructs an App with the given database and JetStream publisher.
func NewApp(db *sql.DB, js Publisher) *App {
	return &App{db: db, js: js}
}
