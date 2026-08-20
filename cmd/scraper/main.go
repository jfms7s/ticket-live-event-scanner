package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/jfms7s/ticket-live-event-scanner/internal/scraper/config"
	"github.com/jfms7s/ticket-live-event-scanner/internal/scraper/discovery"
	"github.com/jfms7s/ticket-live-event-scanner/internal/store"
	"github.com/jfms7s/ticket-live-event-scanner/internal/streams"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()

	cfg := config.Load()

	if err := run(ctx, cfg); err != nil {
		log.Printf("ERROR: %v", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, cfg config.Config) error {
	// Initialize Turso connection
	db, err := store.Connect(cfg.TursoDatabaseURL, cfg.TursoAuthToken)
	if err != nil {
		// Wrap error but avoid exposing auth token in error messages
		return fmt.Errorf("connect to turso (check credentials): %w", err)
	}
	defer db.Close()

	// Initialize NATS connection
	nc, err := nats.Connect(cfg.NatsURL)
	if err != nil {
		return fmt.Errorf("connect to nats: %w", err)
	}
	defer nc.Close()

	js, err := jetstream.New(nc)
	if err != nil {
		return fmt.Errorf("create jetstream context: %w", err)
	}

	// Ensure streams exist
	if err := streams.EnsureStreams(ctx, js); err != nil {
		return fmt.Errorf("ensure streams: %w", err)
	}

	// Create scraper instance
	scraper := discovery.New(cfg, db, js)

	// Load known event IDs from database
	if err := scraper.LoadKnownIDs(ctx); err != nil {
		return fmt.Errorf("load known event IDs: %w", err)
	}

	// Discover events
	if err := scraper.Discover(ctx); err != nil {
		log.Printf("Discovery encountered errors: %v", err)
		return fmt.Errorf("discovery failed: %w", err)
	}

	// Log summary
	log.Printf("Scraper finished: found=%d published=%d errors=%d",
		scraper.Stats.EventsFound,
		scraper.Stats.EventsPublished,
		scraper.Stats.Errors)

	return nil
}
