// Package consume is the JetStream consume loop the notifiers share:
// messages are handled one at a time and, on shutdown, the one in hand
// finishes and is acked.
package consume

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/jfms7s/ticket-live-event-scanner/internal/shutdown"
	"github.com/nats-io/nats.go/jetstream"
)

// DrainTimeout bounds how long shutdown waits for the message in progress.
// It leaves room, inside Kubernetes' default 30 s termination grace period,
// for the HTTP server to stop first.
const DrainTimeout = 12 * time.Second

// closeGrace is how long Loop waits for the consumer to stop after the
// message in progress has been cancelled.
const closeGrace = 2 * time.Second

// ErrStopped is returned by Loop when the consumer stopped by itself (its
// durable was deleted, the connection closed) rather than because ctx was
// cancelled. The caller should create the consumer again and call Loop.
var ErrStopped = errors.New("consumer stopped")

// Loop delivers consumer's messages to handle, one at a time, until ctx is
// cancelled, then drains. name labels the log lines. It returns an error
// when the consumer cannot be started or stops by itself (ErrStopped).
func Loop(ctx context.Context, name string, consumer jetstream.Consumer, handle func(context.Context, jetstream.Msg) error) error {
	return loop(ctx, name, consumer, handle, DrainTimeout)
}

func loop(ctx context.Context, name string, consumer jetstream.Consumer, handle func(context.Context, jetstream.Msg) error, drainTimeout time.Duration) error {
	// Messages in hand run on workCtx, not ctx: a shutdown stops new
	// deliveries but lets a send in progress finish and be acked, so it
	// isn't redelivered (and sent twice) after the restart.
	workCtx, workCancel := context.WithCancel(context.Background())
	defer workCancel()

	// Consume() is non-blocking; it registers the callback and returns immediately.
	consumeCtx, err := consumer.Consume(
		func(msg jetstream.Msg) {
			if err := handle(workCtx, msg); err != nil {
				log.Printf("Error handling %s message: %v", name, err)
			}
		},
		jetstream.ConsumeErrHandler(func(_ jetstream.ConsumeContext, err error) {
			log.Printf("Consume error (%s): %v", name, err)
		}),
		// Messages are handled one at a time, so buffer one: on shutdown
		// Drain() then finishes the message in hand (and at most one
		// more) instead of working through a prefetched backlog.
		jetstream.PullMaxMessages(1),
	)
	if err != nil {
		return fmt.Errorf("consume %s messages: %w", name, err)
	}

	select {
	case <-ctx.Done():
	case <-consumeCtx.Closed():
		// nats.go ends the consume on a deleted durable or a closed
		// connection and nothing else would notice: the loop would sit
		// here receiving nothing.
		select {
		case <-ctx.Done():
		default:
			return fmt.Errorf("%s: %w", name, ErrStopped)
		}
	}

	// Stop pulling and wait for the callback in progress to finish and ack
	// (Stop alone doesn't wait). Past the bound, cancel the send and let
	// JetStream redeliver it.
	consumeCtx.Drain()
	if shutdown.WaitOrTimeout(consumeCtx.Closed(), drainTimeout) {
		log.Printf("%s consumer drained", name)
		return nil
	}
	log.Printf("%s consumer still busy after %v; cancelling the message in progress", name, drainTimeout)
	workCancel()
	if !shutdown.WaitOrTimeout(consumeCtx.Closed(), closeGrace) {
		log.Printf("%s consumer did not stop; closing the connection anyway", name)
	}
	return nil
}
