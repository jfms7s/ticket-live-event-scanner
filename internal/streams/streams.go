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
	PurchasedSubject = "events.purchased"

	NotificationsStreamName       = "NOTIFICATIONS"
	NotificationsSentSubject      = "notifications.sent"
	NotificationsFailSubject      = "notifications.failed"
	NotificationsEmailSentSubject = "notifications.email.sent"
	NotificationsEmailFailSubject = "notifications.email.failed"
	EventsConsumerDurableName     = "telegram-notifier"
	EmailConsumerDurableName      = "email-notifier"
	EventsConsumerMaxDeliver      = 5
	EventsConsumerAckWait         = 30 * time.Second

	streamMaxAge = 90 * 24 * time.Hour

	// eventsDedupWindow is how long JetStream remembers a message ID on
	// EVENTS. The scraper runs hourly and publishes each session with
	// WithMsgID(eventID), so the server's 2-minute default never catches a
	// repeat from the next run. Seven days covers a week of web-ui-api
	// being down or behind; the scraper's seen bucket covers the rest.
	// Manual retries and purchases use unique IDs, so they are unaffected.
	eventsDedupWindow = 7 * 24 * time.Hour
)

// NotificationsSubjects returns every subject on the NOTIFICATIONS stream:
// Telegram results (two tokens) and email results (three tokens). It is a
// function so callers get their own slice to keep.
func NotificationsSubjects() []string {
	return []string{
		NotificationsSentSubject,
		NotificationsFailSubject,
		NotificationsEmailSentSubject,
		NotificationsEmailFailSubject,
	}
}

// EventsStreamConfig is the EVENTS stream's config. Every service applies
// it on startup through EnsureStreams, so an image built before a change
// here resets the stream to its own copy when it starts: release all
// images together after changing it.
func EventsStreamConfig() jetstream.StreamConfig {
	return jetstream.StreamConfig{
		Name:       EventsStreamName,
		Subjects:   []string{EventsSubject, PurchasedSubject},
		Storage:    jetstream.FileStorage,
		Retention:  jetstream.LimitsPolicy,
		MaxAge:     streamMaxAge,
		Duplicates: eventsDedupWindow,
	}
}

// NotificationsStreamConfig is the NOTIFICATIONS stream's config.
func NotificationsStreamConfig() jetstream.StreamConfig {
	return jetstream.StreamConfig{
		Name:      NotificationsStreamName,
		Subjects:  NotificationsSubjects(),
		Storage:   jetstream.FileStorage,
		Retention: jetstream.LimitsPolicy,
		MaxAge:    streamMaxAge,
	}
}

// EnsureStreams creates or updates the EVENTS and NOTIFICATIONS streams.
// Safe to call from every service on startup; JetStream treats it as a
// no-op when the config already matches.
func EnsureStreams(ctx context.Context, js jetstream.JetStream) error {
	if _, err := js.CreateOrUpdateStream(ctx, EventsStreamConfig()); err != nil {
		return fmt.Errorf("ensure %s stream: %w", EventsStreamName, err)
	}

	if _, err := js.CreateOrUpdateStream(ctx, NotificationsStreamConfig()); err != nil {
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

// EnsureEmailConsumer creates or updates the durable consumer used by
// email-notifier to read events.purchased, with the same redelivery
// policy as EnsureEventsConsumer.
func EnsureEmailConsumer(ctx context.Context, js jetstream.JetStream) (jetstream.Consumer, error) {
	stream, err := js.Stream(ctx, EventsStreamName)
	if err != nil {
		return nil, fmt.Errorf("get %s stream: %w", EventsStreamName, err)
	}

	consumer, err := stream.CreateOrUpdateConsumer(ctx, jetstream.ConsumerConfig{
		Durable:       EmailConsumerDurableName,
		AckPolicy:     jetstream.AckExplicitPolicy,
		MaxDeliver:    EventsConsumerMaxDeliver,
		AckWait:       EventsConsumerAckWait,
		FilterSubject: PurchasedSubject,
	})
	if err != nil {
		return nil, fmt.Errorf("ensure %s consumer: %w", EmailConsumerDurableName, err)
	}

	return consumer, nil
}

// SeenBucketName is the JetStream KV bucket where the scraper records each
// event ID it has published, so later runs don't publish it again. Only
// the scraper reads or writes it.
const SeenBucketName = "SCRAPER_SEEN_EVENTS"

// SeenBucketConfig is the seen bucket's config. Keys never expire (no TTL):
// an expired key would let a still-listed event be notified again.
func SeenBucketConfig() jetstream.KeyValueConfig {
	return jetstream.KeyValueConfig{
		Bucket:      SeenBucketName,
		Description: "Event IDs the scraper has published (key: event ID, value: when)",
		History:     1,
		Storage:     jetstream.FileStorage,
	}
}

// EnsureSeenBucket creates or updates the seen bucket and returns it.
func EnsureSeenBucket(ctx context.Context, js jetstream.JetStream) (jetstream.KeyValue, error) {
	kv, err := js.CreateOrUpdateKeyValue(ctx, SeenBucketConfig())
	if err != nil {
		return nil, fmt.Errorf("ensure %s bucket: %w", SeenBucketName, err)
	}
	return kv, nil
}
