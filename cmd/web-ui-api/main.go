// Package main runs the web-ui-api service, a long-lived Kubernetes Deployment.
//
// Responsibilities:
//  1. Consume events.discovered messages from NATS JetStream and materialize
//     them into the Turso database (upsert events, insert pending notifications).
//  2. Consume notifications.sent and notifications.failed messages and update
//     notification statuses in the database.
//  3. Serve an HTTP API for the web-ui-frontend to query events and notifications,
//     and to trigger retries.
//
// See docs/design.md §6.4 and §8.1 for the full spec.
package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jfms7s/ticket-live-event-scanner/internal/store"
	"github.com/jfms7s/ticket-live-event-scanner/internal/streams"
	"github.com/jfms7s/ticket-live-event-scanner/internal/webui/api"
	"github.com/jfms7s/ticket-live-event-scanner/internal/webui/consumer"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

func main() {
	// Environment setup
	natsURL := os.Getenv("NATS_URL")
	if natsURL == "" {
		natsURL = "nats://localhost:4222"
	}
	tursoURL := os.Getenv("TURSO_DATABASE_URL")
	tursoToken := os.Getenv("TURSO_AUTH_TOKEN")
	if tursoURL == "" || tursoToken == "" {
		log.Fatal("TURSO_DATABASE_URL and TURSO_AUTH_TOKEN are required")
	}
	listenAddr := os.Getenv("LISTEN_ADDR")
	if listenAddr == "" {
		listenAddr = ":8080"
	}
	corsOrigin := os.Getenv("CORS_ORIGIN")
	if corsOrigin == "" {
		// Default to empty (no CORS) for cluster-internal safety
		// If frontend needs CORS, set CORS_ORIGIN=http://web-ui-frontend:3000 or similar
		corsOrigin = ""
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Connect to Turso
	db, err := store.Connect(tursoURL, tursoToken)
	if err != nil {
		log.Fatalf("Failed to connect to Turso: %v", err)
	}
	defer db.Close()

	// Migrate schema
	if err := store.Migrate(ctx, db); err != nil {
		log.Fatalf("Failed to migrate schema: %v", err)
	}

	// Connect to NATS
	nc, err := nats.Connect(natsURL)
	if err != nil {
		log.Fatalf("Failed to connect to NATS: %v", err)
	}
	defer nc.Close()

	// Get JetStream context
	js, err := jetstream.New(nc)
	if err != nil {
		log.Fatalf("Failed to create JetStream context: %v", err)
	}

	// Ensure streams exist
	if err := streams.EnsureStreams(ctx, js); err != nil {
		log.Fatalf("Failed to ensure streams: %v", err)
	}

	// Create durable consumers for web-ui-api
	// Consumer for events.discovered
	eventConsumer, err := consumer.CreateOrUpdateEventsConsumer(ctx, js)
	if err != nil {
		log.Fatalf("Failed to create events consumer: %v", err)
	}

	// Consumer for notifications.* (sent and failed)
	notifConsumer, err := consumer.CreateOrUpdateNotificationsConsumer(ctx, js)
	if err != nil {
		log.Fatalf("Failed to create notifications consumer: %v", err)
	}

	// Start the NATS consumers in background goroutines with error recovery
	// Each consumer runs in its own goroutine and logs errors
	go func() {
		for {
			consumer.ConsumeEventsDiscovered(ctx, eventConsumer, db)
			// Log error and retry after delay to avoid tight loop
			log.Println("Events consumer exited, restarting in 5 seconds...")
			time.Sleep(5 * time.Second)
		}
	}()

	go func() {
		for {
			consumer.ConsumeNotifications(ctx, notifConsumer, db)
			// Log error and retry after delay
			log.Println("Notifications consumer exited, restarting in 5 seconds...")
			time.Sleep(5 * time.Second)
		}
	}()

	// Setup HTTP server
	app := api.NewApp(db, js)
	handler := api.CORSMiddleware(app.NewMux(), corsOrigin)

	server := &http.Server{
		Addr:    listenAddr,
		Handler: handler,
	}

	// Start server in a goroutine
	go func() {
		log.Printf("Starting HTTP server on %s", listenAddr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server error: %v", err)
		}
	}()

	// Wait for shutdown signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	log.Println("Shutting down...")
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Printf("Server shutdown error: %v", err)
	}
}
