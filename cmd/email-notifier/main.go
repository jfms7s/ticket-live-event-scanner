package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/jfms7s/ticket-live-event-scanner/internal/emailnotifier/config"
	"github.com/jfms7s/ticket-live-event-scanner/internal/notifier/email"
	"github.com/jfms7s/ticket-live-event-scanner/internal/notifier/emailhandler"
	"github.com/jfms7s/ticket-live-event-scanner/internal/streams"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

func main() {
	cfg := config.Load()

	// Connect to NATS
	nc, err := nats.Connect(cfg.NatsURL)
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
	consumer, err := streams.EnsureEmailConsumer(ctx, js)
	if err != nil {
		log.Fatalf("Failed to ensure consumer: %v", err)
	}

	// Create handler
	smtpCfg := email.SMTPConfig{
		Host:     cfg.SMTPHost,
		Port:     cfg.SMTPPort,
		Username: cfg.SMTPUsername,
		Password: cfg.SMTPPassword,
		From:     cfg.EmailFrom,
	}
	h := emailhandler.New(smtpCfg, cfg.EmailTo, js)

	// Setup signal handling for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Start cache cleanup goroutine (removes stale entries every 30 seconds)
	go h.CleanupCache(ctx)

	go func() {
		<-sigChan
		log.Println("Received shutdown signal, gracefully stopping...")
		cancel()
	}()

	// Consume messages
	log.Println("Starting to consume purchased events...")
	consumeMessages(ctx, consumer, h)
	log.Println("Shutdown complete")
}

func consumeMessages(ctx context.Context, consumer jetstream.Consumer, h *emailhandler.Handler) {
	// Consume() is non-blocking; it registers the callback and returns immediately
	consumeCtx, err := consumer.Consume(
		func(msg jetstream.Msg) {
			if err := h.HandleMessage(ctx, msg); err != nil {
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

	<-ctx.Done()

	// Stop message delivery cleanly before closing the connection
	consumeCtx.Stop()
}
