package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jfms7s/ticket-live-event-scanner/internal/notifier/handler"
	"github.com/jfms7s/ticket-live-event-scanner/internal/shutdown"
	"github.com/jfms7s/ticket-live-event-scanner/internal/streams"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

// drainTimeout bounds how long shutdown waits for the message in progress,
// under Kubernetes' default 30 s termination grace period.
const drainTimeout = 25 * time.Second

func main() {
	// Load environment variables
	natsURL := os.Getenv("NATS_URL")
	if natsURL == "" {
		natsURL = "nats://localhost:4222"
	}

	telegramBotToken := os.Getenv("TELEGRAM_BOT_TOKEN")
	if telegramBotToken == "" {
		log.Fatal("TELEGRAM_BOT_TOKEN environment variable is required")
	}

	telegramChatID := os.Getenv("TELEGRAM_CHAT_ID")
	if telegramChatID == "" {
		log.Fatal("TELEGRAM_CHAT_ID environment variable is required")
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

	// Setup context for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Ensure streams exist
	if err := streams.EnsureStreams(ctx, js); err != nil {
		log.Fatalf("Failed to ensure streams: %v", err)
	}

	// Get or create consumer
	consumer, err := streams.EnsureEventsConsumer(ctx, js)
	if err != nil {
		log.Fatalf("Failed to ensure consumer: %v", err)
	}

	// Create handler
	h := handler.New(telegramBotToken, telegramChatID, js)

	// Setup signal handling for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Start cache cleanup goroutine (removes stale entries every 30 seconds)
	go h.CleanupCache(ctx)

	// Start consuming messages
	go func() {
		<-sigChan
		log.Println("Received shutdown signal, gracefully stopping...")
		cancel()
	}()

	// Consume messages
	log.Println("Starting to consume events...")
	consumeMessages(ctx, consumer, h)
	log.Println("Shutdown complete")
}

func consumeMessages(ctx context.Context, consumer jetstream.Consumer, h *handler.Handler) {
	// Messages in hand run on workCtx, not ctx: a shutdown stops new
	// deliveries but lets a send in progress finish and be acked, so it
	// isn't redelivered (and sent twice) after the restart.
	workCtx, workCancel := context.WithCancel(context.Background())
	defer workCancel()

	// Consume() is non-blocking; it registers the callback and returns immediately
	consumeCtx, err := consumer.Consume(
		func(msg jetstream.Msg) {
			// Process the message in the callback
			if err := h.HandleMessage(workCtx, msg); err != nil {
				log.Printf("Error handling message: %v", err)
			}
		},
		jetstream.ConsumeErrHandler(func(consumeCtx jetstream.ConsumeContext, err error) {
			log.Printf("Consume error: %v", err)
		}),
	)

	if err != nil {
		log.Fatalf("Failed to consume messages: %v", err)
	}

	// Wait for context cancellation
	<-ctx.Done()

	// Stop pulling and wait for the callback in progress to finish and ack
	// (Stop alone doesn't wait). Stay inside Kubernetes' 30 s grace period;
	// past the bound, cancel the send and let JetStream redeliver it.
	consumeCtx.Drain()
	if shutdown.WaitOrTimeout(consumeCtx.Closed(), drainTimeout) {
		log.Println("Consumer drained")
		return
	}
	log.Printf("Consumer still busy after %v; cancelling the message in progress", drainTimeout)
	workCancel()
	if !shutdown.WaitOrTimeout(consumeCtx.Closed(), 2*time.Second) {
		log.Println("Consumer did not stop; closing the connection anyway")
	}
}
