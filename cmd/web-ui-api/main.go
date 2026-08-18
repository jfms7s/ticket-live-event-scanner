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
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jfms7s/ticket-live-event-scanner/internal/event"
	"github.com/jfms7s/ticket-live-event-scanner/internal/store"
	"github.com/jfms7s/ticket-live-event-scanner/internal/streams"
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
	eventConsumer, err := createOrUpdateWebUIEventsConsumer(ctx, js)
	if err != nil {
		log.Fatalf("Failed to create events consumer: %v", err)
	}

	// Consumer for notifications.* (sent and failed)
	notifConsumer, err := createOrUpdateWebUINotificationsConsumer(ctx, js)
	if err != nil {
		log.Fatalf("Failed to create notifications consumer: %v", err)
	}

	// Start the NATS consumers in background goroutines with error recovery
	// Each consumer runs in its own goroutine and logs errors
	go func() {
		for {
			consumeEventsDiscovered(ctx, eventConsumer, db)
			// Log error and retry after delay to avoid tight loop
			log.Println("Events consumer exited, restarting in 5 seconds...")
			time.Sleep(5 * time.Second)
		}
	}()

	go func() {
		for {
			consumeNotifications(ctx, notifConsumer, db)
			// Log error and retry after delay
			log.Println("Notifications consumer exited, restarting in 5 seconds...")
			time.Sleep(5 * time.Second)
		}
	}()

	// Setup HTTP server
	app := &App{db: db, js: js}
	mux := http.NewServeMux()

	// Health check
	mux.HandleFunc("GET /healthz", app.handleHealthz)

	// Events API
	mux.HandleFunc("GET /api/events", app.handleListEvents)
	mux.HandleFunc("GET /api/events/{id}", app.handleGetEvent)
	mux.HandleFunc("POST /api/events/{id}/retrigger", app.handleRetrigger)
	mux.HandleFunc("PATCH /api/events/{id}/purchased", app.handleSetPurchased)
	mux.HandleFunc("DELETE /api/events/{id}", app.handleDeleteEvent)

	// Notifications API
	mux.HandleFunc("GET /api/notifications", app.handleListNotifications)

	// Wrap mux with CORS middleware (if configured)
	handler := corsMiddleware(mux, corsOrigin)

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

// createOrUpdateWebUIEventsConsumer creates or updates the durable consumer
// used by web-ui-api to read events.discovered.
func createOrUpdateWebUIEventsConsumer(ctx context.Context, js jetstream.JetStream) (jetstream.Consumer, error) {
	stream, err := js.Stream(ctx, streams.EventsStreamName)
	if err != nil {
		return nil, fmt.Errorf("get %s stream: %w", streams.EventsStreamName, err)
	}

	consumer, err := stream.CreateOrUpdateConsumer(ctx, jetstream.ConsumerConfig{
		Durable:       "web-ui-api-events",
		AckPolicy:     jetstream.AckExplicitPolicy,
		MaxDeliver:    streams.EventsConsumerMaxDeliver,
		AckWait:       streams.EventsConsumerAckWait,
		FilterSubject: streams.EventsSubject,
	})
	if err != nil {
		return nil, fmt.Errorf("create events consumer: %w", err)
	}

	return consumer, nil
}

// createOrUpdateWebUINotificationsConsumer creates or updates the durable
// consumer used by web-ui-api to read notifications.sent and notifications.failed.
func createOrUpdateWebUINotificationsConsumer(ctx context.Context, js jetstream.JetStream) (jetstream.Consumer, error) {
	stream, err := js.Stream(ctx, streams.NotificationsStreamName)
	if err != nil {
		return nil, fmt.Errorf("get %s stream: %w", streams.NotificationsStreamName, err)
	}

	consumer, err := stream.CreateOrUpdateConsumer(ctx, jetstream.ConsumerConfig{
		Durable:       "web-ui-api-notifications",
		AckPolicy:     jetstream.AckExplicitPolicy,
		MaxDeliver:    streams.EventsConsumerMaxDeliver,
		AckWait:       streams.EventsConsumerAckWait,
		FilterSubject: streams.NotificationsAllSubjects,
	})
	if err != nil {
		return nil, fmt.Errorf("create notifications consumer: %w", err)
	}

	return consumer, nil
}

// consumeEventsDiscovered reads from the events.discovered consumer and
// materializes events into the database.
func consumeEventsDiscovered(ctx context.Context, consumer jetstream.Consumer, db *sql.DB) {
	msgsChan, err := consumer.Messages()
	if err != nil {
		log.Printf("Failed to get messages from events consumer: %v", err)
		return
	}
	defer msgsChan.Stop()

	// Watcher goroutine to stop the message channel on context cancellation
	go func() {
		<-ctx.Done()
		msgsChan.Stop()
	}()

	for {
		msg, err := msgsChan.Next()
		if err != nil {
			log.Printf("Error receiving message: %v", err)
			return
		}

		var disc event.Discovered
		if err := json.Unmarshal(msg.Data(), &disc); err != nil {
			log.Printf("Failed to unmarshal Discovered message: %v", err)
			// Malformed message will never become parseable on retry, so terminate it
			msg.Term()
			continue
		}

		// Upsert event and insert pending notification in a transaction
		if err := upsertEventAndInsertNotification(ctx, db, &disc); err != nil {
			log.Printf("Failed to materialize event %d: %v", disc.EventID, err)
			msg.Nak()
			continue
		}

		msg.Ack()
	}
}

// consumeNotifications reads from the notifications consumer and updates
// notification statuses in the database.
func consumeNotifications(ctx context.Context, consumer jetstream.Consumer, db *sql.DB) {
	msgsChan, err := consumer.Messages()
	if err != nil {
		log.Printf("Failed to get messages from notifications consumer: %v", err)
		return
	}
	defer msgsChan.Stop()

	// Watcher goroutine to stop the message channel on context cancellation
	go func() {
		<-ctx.Done()
		msgsChan.Stop()
	}()

	for {
		msg, err := msgsChan.Next()
		if err != nil {
			log.Printf("Error receiving message: %v", err)
			return
		}

		// Determine message type by subject
		subject := msg.Subject()

		if subject == streams.NotificationsSentSubject {
			var sent event.NotificationSent
			if err := json.Unmarshal(msg.Data(), &sent); err != nil {
				log.Printf("Failed to unmarshal NotificationSent message: %v", err)
				// Malformed message will never become parseable on retry, so terminate it
				msg.Term()
				continue
			}

			if err := updateNotificationSent(ctx, db, &sent); err != nil {
				log.Printf("Failed to update notification sent for event %d: %v", sent.EventID, err)
				msg.Nak()
				continue
			}
		} else if subject == streams.NotificationsFailSubject {
			var failed event.NotificationFailed
			if err := json.Unmarshal(msg.Data(), &failed); err != nil {
				log.Printf("Failed to unmarshal NotificationFailed message: %v", err)
				// Malformed message will never become parseable on retry, so terminate it
				msg.Term()
				continue
			}

			if err := updateNotificationFailed(ctx, db, &failed); err != nil {
				log.Printf("Failed to update notification failed for event %d: %v", failed.EventID, err)
				msg.Nak()
				continue
			}
		}

		msg.Ack()
	}
}

// corsMiddleware adds CORS headers and handles OPTIONS preflight.
// If corsOrigin is empty, CORS is disabled (safest for cluster-internal APIs).
func corsMiddleware(next http.Handler, corsOrigin string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Only set CORS headers if a specific origin is configured
		if corsOrigin != "" {
			w.Header().Set("Access-Control-Allow-Origin", corsOrigin)
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		}

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}
