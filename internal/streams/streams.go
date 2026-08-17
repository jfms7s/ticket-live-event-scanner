// Package streams centralizes NATS JetStream stream/subject/consumer
// names and provides idempotent setup so every service (and local dev
// tooling) agrees on the same topology described in docs/design.md §6.2.
package streams

import (
	"context"
	"fmt"
	"time"

	"github.com/nats-io/nats.go/jetstream"
)

const (
	EventsStreamName = "EVENTS"
	EventsSubject    = "events.discovered"

	NotificationsStreamName   = "NOTIFICATIONS"
	NotificationsSentSubject  = "notifications.sent"
	NotificationsFailSubject  = "notifications.failed"
	NotificationsAllSubjects  = "notifications.*"
	EventsConsumerDurableName = "telegram-notifier"
	EventsConsumerMaxDeliver  = 5
	EventsConsumerAckWait     = 30 * time.Second

	streamMaxAge = 90 * 24 * time.Hour
)

// EnsureStreams creates or updates the EVENTS and NOTIFICATIONS streams.
// Safe to call from every service on startup; JetStream treats it as a
// no-op when the config already matches.
func EnsureStreams(ctx context.Context, js jetstream.JetStream) error {
	if _, err := js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name:      EventsStreamName,
		Subjects:  []string{EventsSubject},
		Storage:   jetstream.FileStorage,
		Retention: jetstream.LimitsPolicy,
		MaxAge:    streamMaxAge,
	}); err != nil {
		return fmt.Errorf("ensure %s stream: %w", EventsStreamName, err)
	}

	if _, err := js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name:      NotificationsStreamName,
		Subjects:  []string{NotificationsSentSubject, NotificationsFailSubject},
		Storage:   jetstream.FileStorage,
		Retention: jetstream.LimitsPolicy,
		MaxAge:    streamMaxAge,
	}); err != nil {
		return fmt.Errorf("ensure %s stream: %w", NotificationsStreamName, err)
	}

	return nil
}

// EnsureEventsConsumer creates or updates the durable consumer used by
// telegram-notifier to read events.discovered, with the redelivery policy
// from docs/design.md §6.2/§6.3.
func EnsureEventsConsumer(ctx context.Context, js jetstream.JetStream) (jetstream.Consumer, error) {
	stream, err := js.Stream(ctx, EventsStreamName)
	if err != nil {
		return nil, fmt.Errorf("get %s stream: %w", EventsStreamName, err)
	}

	consumer, err := stream.CreateOrUpdateConsumer(ctx, jetstream.ConsumerConfig{
		Durable:       EventsConsumerDurableName,
		AckPolicy:     jetstream.AckExplicitPolicy,
		MaxDeliver:    EventsConsumerMaxDeliver,
		AckWait:       EventsConsumerAckWait,
		FilterSubject: EventsSubject,
	})
	if err != nil {
		return nil, fmt.Errorf("ensure %s consumer: %w", EventsConsumerDurableName, err)
	}

	return consumer, nil
}
