package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/jfms7s/ticket-live-event-scanner/internal/envconfig"
	"github.com/jfms7s/ticket-live-event-scanner/internal/natsconn"
	"github.com/jfms7s/ticket-live-event-scanner/internal/streams"
	"github.com/nats-io/nats.go/jetstream"
)

// runDrainCheck is `web-ui-api --nats-drain-check`: it prints, for each of
// the durable consumers the earlier releases used, how many messages are
// waiting and how many were delivered and not yet acknowledged, and returns
// 0 only when all are zero. Moving from the NATS-based release to the
// outbox one is safe only when nothing is in flight: a message already
// sent whose result is still in a stream would otherwise be sent again.
// It needs NATS_URL (and NATS_TOKEN if the broker requires one) and nothing
// else.
func runDrainCheck() int {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	nc, err := natsconn.Connect(envconfig.NatsURL(), "drain-check")
	if err != nil {
		fmt.Fprintf(os.Stderr, "connect to NATS: %v\n", err)
		return 2
	}
	defer nc.Close()
	js, err := jetstream.New(nc)
	if err != nil {
		fmt.Fprintf(os.Stderr, "JetStream: %v\n", err)
		return 2
	}

	drained := true
	for _, spec := range []streams.ConsumerSpec{
		streams.TelegramConsumer(), streams.TelegramDigestConsumer(), streams.EmailConsumer(),
		streams.WebUIEventsConsumer(), streams.WebUINotificationsConsumer(),
	} {
		name := spec.Stream + "/" + spec.Config.Durable
		pending, ackPending, err := consumerBacklog(ctx, js, spec)
		switch {
		case errors.Is(err, jetstream.ErrStreamNotFound), errors.Is(err, jetstream.ErrConsumerNotFound):
			fmt.Printf("%-45s absent (nothing to drain)\n", name)
		case err != nil:
			fmt.Fprintf(os.Stderr, "%s: %v\n", name, err)
			return 2
		default:
			fmt.Printf("%-45s pending=%d ack_pending=%d\n", name, pending, ackPending)
			if pending > 0 || ackPending > 0 {
				drained = false
			}
		}
	}
	if err := printSeenIDs(ctx, js); err != nil {
		fmt.Fprintf(os.Stderr, "seen bucket: %v\n", err)
		return 2
	}
	if !drained {
		fmt.Println("NOT drained: wait, then run the check again.")
		return 1
	}
	fmt.Println("Drained: nothing is in flight.")
	return 0
}

func consumerBacklog(ctx context.Context, js jetstream.JetStream, spec streams.ConsumerSpec) (pending uint64, ackPending int, err error) {
	stream, err := js.Stream(ctx, spec.Stream)
	if err != nil {
		return 0, 0, err
	}
	c, err := stream.Consumer(ctx, spec.Config.Durable)
	if err != nil {
		return 0, 0, err
	}
	info, err := c.Info(ctx)
	if err != nil {
		return 0, 0, err
	}
	return info.NumPending, info.NumAckPending, nil
}

// printSeenIDs lists the ids in the scraper's old "seen" bucket. The outbox
// release treats only the events table as seen, so an id that is in the
// bucket and not in the table (published, but never stored) would be
// announced again; the runbook compares the two before the rollout.
func printSeenIDs(ctx context.Context, js jetstream.JetStream) error {
	kv, err := js.KeyValue(ctx, streams.SeenBucketName)
	if errors.Is(err, jetstream.ErrBucketNotFound) {
		fmt.Println("seen bucket absent")
		return nil
	}
	if err != nil {
		return err
	}
	keys, err := kv.Keys(ctx)
	if errors.Is(err, jetstream.ErrNoKeysFound) {
		keys = nil
	} else if err != nil {
		return err
	}
	sort.Strings(keys)
	fmt.Printf("seen bucket: %d ids\nseen-ids: %s\n", len(keys), strings.Join(keys, ","))
	return nil
}
