// Package api implements the HTTP API served by web-ui-api for the
// dashboard to query events and notifications, and to trigger retries.
package api

import (
	"context"
	"database/sql"
	"net/http"

	"github.com/jfms7s/ticket-live-event-scanner/internal/runs"

	"github.com/nats-io/nats.go/jetstream"
)

// Publisher is an interface for publishing messages to JetStream.
type Publisher interface {
	Publish(ctx context.Context, subject string, data []byte, opts ...jetstream.PublishOpt) (*jetstream.PubAck, error)
}

// RunsReader reads the scraper's run log (see package runs).
type RunsReader interface {
	Summary(ctx context.Context, limit int) (runs.Summary, error)
}

// App holds dependencies for HTTP handlers.
type App struct {
	db   *sql.DB
	js   Publisher
	runs RunsReader

	static http.Handler
	// notifiers says which delivery channels are enabled; see SetNotifierStatus.
	notifiers map[string]bool
}

// NewApp constructs an App with the given database and JetStream publisher.
func NewApp(db *sql.DB, js Publisher) *App {
	return &App{db: db, js: js}
}

// SetRuns sets the run log GET /api/runs reads. Without one the endpoint
// answers with no runs.
func (app *App) SetRuns(r RunsReader) { app.runs = r }

// SetStatic sets the handler for every GET path the API does not claim, which
// is how the dashboard is served from the same origin. Without one those
// paths answer 404.
func (app *App) SetStatic(h http.Handler) { app.static = h }

// SetNotifierStatus sets which delivery channels ("telegram", "email") are
// enabled, for /metrics. A channel with incomplete configuration is
// disabled rather than stopping the process, so its absence must be visible.
func (app *App) SetNotifierStatus(enabled map[string]bool) { app.notifiers = enabled }
