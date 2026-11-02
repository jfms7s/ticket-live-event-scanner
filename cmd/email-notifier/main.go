package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jfms7s/ticket-live-event-scanner/internal/emailnotifier/config"
	"github.com/jfms7s/ticket-live-event-scanner/internal/health"
	"github.com/jfms7s/ticket-live-event-scanner/internal/notifier/email"
	"github.com/jfms7s/ticket-live-event-scanner/internal/notifier/emailhandler"
	"github.com/jfms7s/ticket-live-event-scanner/internal/shutdown"
	"github.com/jfms7s/ticket-live-event-scanner/internal/streams"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

// drainTimeout bounds how long shutdown waits for the message in progress,
// under Kubernetes' default 30 s termination grace period.
const drainTimeout = 25 * time.Second

// healthInterval is how often the liveness file is refreshed; the probe
// allows several missed beats.
const healthInterval = 15 * time.Second

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
	consumer, err := streams.EnsureConsumer(ctx, js, streams.EmailConsumer())
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

		AllowPlaintext: cfg.SMTPAllowPlaintext,
	}
	h := emailhandler.New(smtpCfg, cfg.EmailTo, js)

	// Setup signal handling for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Start cache cleanup goroutine (removes stale entries every 30 seconds)
	go h.CleanupCache(ctx)

	// Refresh the liveness file while NATS and the consumer are reachable,
	// so the probe fails when the notifier can no longer receive messages.
	go health.Heartbeat(ctx, health.File(), healthInterval, health.NATSConsumerCheck(nc.Status, consumer))

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
	// Messages in hand run on workCtx, not ctx: a shutdown stops new
	// deliveries but lets a send in progress finish and be acked, so it
	// isn't redelivered (and sent twice) after the restart.
	workCtx, workCancel := context.WithCancel(context.Background())
	defer workCancel()

	// Consume() is non-blocking; it registers the callback and returns immediately
	consumeCtx, err := consumer.Consume(
		func(msg jetstream.Msg) {
			if err := h.HandleMessage(workCtx, msg); err != nil {
				log.Printf("Error handling message: %v", err)
			}
		},
		jetstream.ConsumeErrHandler(func(consumeCtx jetstream.ConsumeContext, err error) {
			log.Printf("Consume error: %v", err)
		}),
		// Messages are handled one at a time, so buffer one: on shutdown
		// Drain() then finishes the message in hand (and at most one
		// more) instead of working through a prefetched backlog.
		jetstream.PullMaxMessages(1),
	)

	if err != nil {
		log.Fatalf("Failed to consume messages: %v", err)
	}

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
