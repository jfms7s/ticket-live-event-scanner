package notifier

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jfms7s/ticket-live-event-scanner/internal/streams"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

// fake is a Notifier with one consumer on the EVENTS stream.
type fake struct {
	name    string
	durable string
	subject string
	bindErr error

	mu       sync.Mutex
	handled  []string
	maintain chan struct{}
}

func (f *fake) Name() string { return f.name }

func (f *fake) Bindings(ctx context.Context, js jetstream.JetStream) ([]Binding, error) {
	if f.bindErr != nil {
		return nil, f.bindErr
	}
	stream, err := js.Stream(ctx, streams.EventsStreamName)
	if err != nil {
		return nil, err
	}
	c, err := stream.CreateOrUpdateConsumer(ctx, jetstream.ConsumerConfig{
		Durable: f.durable, AckPolicy: jetstream.AckExplicitPolicy, FilterSubject: f.subject,
	})
	if err != nil {
		return nil, err
	}
	return []Binding{{Name: "q", Consumer: c, Handle: func(_ context.Context, m jetstream.Msg) error {
		f.mu.Lock()
		f.handled = append(f.handled, string(m.Data()))
		f.mu.Unlock()
		return m.Ack()
	}}}, nil
}

func (f *fake) Maintain(ctx context.Context) {
	if f.maintain != nil {
		close(f.maintain)
	}
	<-ctx.Done()
}

func (f *fake) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.handled)
}

func natsJS(t *testing.T) jetstream.JetStream {
	t.Helper()
	url := os.Getenv("NATS_TEST_URL")
	if url == "" {
		t.Skip("NATS_TEST_URL not set")
	}
	nc, err := nats.Connect(url)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(nc.Close)
	js, err := jetstream.New(nc)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = js.DeleteStream(ctx, streams.EventsStreamName)
	if err := streams.EnsureStreams(ctx, js); err != nil {
		t.Fatal(err)
	}
	return js
}

// Each channel gets exactly its own subject's messages, once, and the
// process drains when asked to stop.
func TestStartRunsEveryNotifierAndDrains(t *testing.T) {
	js := natsJS(t)
	tg := &fake{name: "telegram", durable: "t-test", subject: streams.EventsSubject, maintain: make(chan struct{})}
	em := &fake{name: "email", durable: "e-test", subject: streams.PurchasedSubject}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done, err := Start(ctx, js, []Notifier{tg, em})
	if err != nil {
		t.Fatal(err)
	}

	pctx, pcancel := context.WithTimeout(ctx, 5*time.Second)
	defer pcancel()
	if _, err := js.Publish(pctx, streams.EventsSubject, []byte("session")); err != nil {
		t.Fatal(err)
	}
	if _, err := js.Publish(pctx, streams.PurchasedSubject, []byte("invite")); err != nil {
		t.Fatal(err)
	}

	waitFor(t, func() bool { return tg.count() == 1 && em.count() == 1 })
	time.Sleep(300 * time.Millisecond)
	if tg.count() != 1 || em.count() != 1 || tg.handled[0] != "session" || em.handled[0] != "invite" {
		t.Errorf("telegram %v, email %v; want one message each on its own subject", tg.handled, em.handled)
	}
	select {
	case <-tg.maintain:
	default:
		t.Error("Maintain was not started")
	}

	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("notifiers did not drain after the context was cancelled")
	}
}

func TestStartFailsBeforeRunningAnythingWhenAConsumerCannotBeCreated(t *testing.T) {
	js := natsJS(t)
	ok := &fake{name: "ok", durable: "ok-test", subject: streams.EventsSubject, maintain: make(chan struct{})}
	bad := &fake{name: "bad", bindErr: errors.New("boom")}
	done, err := Start(context.Background(), js, []Notifier{ok, bad})
	if err == nil || done != nil {
		t.Fatalf("Start = %v, %v; want an error", done, err)
	}
	select {
	case <-ok.maintain:
		t.Error("a notifier was started although another failed to bind")
	case <-time.After(200 * time.Millisecond):
	}
}

func TestStartWithNoNotifiersIsDoneAtOnce(t *testing.T) {
	done, err := Start(context.Background(), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("done was not closed")
	}
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("condition not met in time")
}
