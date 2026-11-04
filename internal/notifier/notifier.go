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
	Name     string
	Consumer jetstream.Consumer
	Handle   func(context.Context, jetstream.Msg) error
}

// Notifier is one delivery channel.
type Notifier interface {
	// Name is the channel's name in logs and metrics ("telegram", "email").
	Name() string
	// Bindings creates (or updates) the channel's durable consumers.
	Bindings(ctx context.Context, js jetstream.JetStream) ([]Binding, error)
	// Maintain runs the channel's housekeeping until ctx is done.
	Maintain(ctx context.Context)
}

// Start creates every notifier's consumers, then runs them in the
// background until ctx is cancelled. Creating the consumers is the only
// step that can fail; a consumer that stops later is restarted. The
// returned channel is closed once every consumer has drained.
func Start(ctx context.Context, js jetstream.JetStream, notifiers []Notifier) (<-chan struct{}, error) {
	type bound struct {
		n Notifier
		b Binding
	}
	var all []bound
	for _, n := range notifiers {
		bs, err := n.Bindings(ctx, js)
		if err != nil {
			return nil, fmt.Errorf("%s notifier: %w", n.Name(), err)
		}
		for _, b := range bs {
			all = append(all, bound{n, b})
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
			shutdown.RunWithRestart(ctx, x.n.Name()+" "+x.b.Name+" consumer", func(ctx context.Context) {
				if err := consume.Loop(ctx, x.n.Name()+" "+x.b.Name, x.b.Consumer, x.b.Handle); err != nil {
					log.Printf("ERROR %v", err)
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
