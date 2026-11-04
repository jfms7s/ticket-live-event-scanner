// Package notifier is what the delivery channels (Telegram, email) have in
// common: each owns some durable JetStream consumers and a handler per
// consumer, and the process runs them all with one consume loop.
package notifier

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/jfms7s/ticket-live-event-scanner/internal/consume"
	"github.com/jfms7s/ticket-live-event-scanner/internal/shutdown"
	"github.com/nats-io/nats.go/jetstream"
)

// restartDelay is the pause before a consumer that stopped is restarted.
const restartDelay = 5 * time.Second

// Binding is one durable consumer and the function handling its messages.
type Binding struct {
	Name string
	// Ensure creates (or updates) the durable consumer. It runs at start and
	// again whenever the consumer stops by itself, since a consumer object
	// outlives neither a deleted durable nor a reconnect.
	Ensure func(context.Context) (jetstream.Consumer, error)
	Handle func(context.Context, jetstream.Msg) error
}

// Notifier is one delivery channel.
type Notifier interface {
	// Name is the channel's name in logs and metrics ("telegram", "email").
	Name() string
	// Bindings returns the channel's durable consumers and their handlers.
	Bindings(ctx context.Context, js jetstream.JetStream) ([]Binding, error)
	// Maintain runs the channel's housekeeping until ctx is done.
	Maintain(ctx context.Context)
}

// Start creates every notifier's consumers, then runs them in the
// background until ctx is cancelled. Creating the consumers is the only
// step that can fail; a consumer that stops later is created again and
// restarted. The
// returned channel is closed once every consumer has drained.
func Start(ctx context.Context, js jetstream.JetStream, notifiers []Notifier) (<-chan struct{}, error) {
	type bound struct {
		n Notifier
		b Binding
	}
	type started struct {
		bound
		first jetstream.Consumer
	}
	var all []started
	for _, n := range notifiers {
		bs, err := n.Bindings(ctx, js)
		if err != nil {
			return nil, fmt.Errorf("%s notifier: %w", n.Name(), err)
		}
		for _, b := range bs {
			// Created now so a bad stream or credentials fails the start.
			c, err := b.Ensure(ctx)
			if err != nil {
				return nil, fmt.Errorf("%s notifier: %s consumer: %w", n.Name(), b.Name, err)
			}
			all = append(all, started{bound{n, b}, c})
		}
	}

	var wg sync.WaitGroup
	for _, n := range notifiers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			n.Maintain(ctx)
		}()
	}
	for _, x := range all {
		wg.Add(1)
		go func() {
			defer wg.Done()
			label := x.n.Name() + " " + x.b.Name
			c := x.first
			shutdown.RunWithRestart(ctx, label+" consumer", func(ctx context.Context) {
				if c == nil {
					var err error
					if c, err = x.b.Ensure(ctx); err != nil {
						log.Printf("ERROR %s consumer: %v", label, err)
						return
					}
				}
				err := consume.Loop(ctx, label, c, x.b.Handle)
				if err != nil {
					log.Printf("ERROR %v", err)
					c = nil // create it again before the next try
				}
			}, restartDelay)
		}()
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	return done, nil
}
