// Package streams centralizes NATS JetStream stream/subject/consumer
// names and provides idempotent setup so every service (and local dev
// tooling) agrees on the same topology described in
// the project design doc (kept outside this repo), §6.2.
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

	// Durable consumer names. Changing one makes JetStream create a new
	// consumer that replays the stream, so they never change.
	TelegramConsumerDurableName           = "telegram-notifier"
	EmailConsumerDurableName              = "email-notifier"
	WebUIEventsConsumerDurableName        = "web-ui-api-events"
	WebUINotificationsConsumerDurableName = "web-ui-api-notifications"

	// DefaultMaxDeliver and DefaultAckWait are the redelivery policy every
	// consumer uses; the handlers' nak/term decisions read DefaultMaxDeliver.
	DefaultMaxDeliver = 5
	DefaultAckWait    = 30 * time.Second

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

// ConsumerSpec is one durable pull consumer: the stream it reads and its
// config.
type ConsumerSpec struct {
	Stream string
	Config jetstream.ConsumerConfig
}

// consumerConfig is the shared policy with the given durable name; the
// caller sets the filter.
func consumerConfig(durable string) jetstream.ConsumerConfig {
	return jetstream.ConsumerConfig{
		Durable:    durable,
		AckPolicy:  jetstream.AckExplicitPolicy,
		MaxDeliver: DefaultMaxDeliver,
		AckWait:    DefaultAckWait,
	}
}

// TelegramConsumer is telegram-notifier's consumer of events.discovered,
// with the redelivery policy from the project design doc (kept outside
// this repo), §6.2/§6.3.
func TelegramConsumer() ConsumerSpec {
	cfg := consumerConfig(TelegramConsumerDurableName)
	cfg.FilterSubject = EventsSubject
	return ConsumerSpec{Stream: EventsStreamName, Config: cfg}
}

// EmailConsumer is email-notifier's consumer of events.purchased.
func EmailConsumer() ConsumerSpec {
	cfg := consumerConfig(EmailConsumerDurableName)
	cfg.FilterSubject = PurchasedSubject
	return ConsumerSpec{Stream: EventsStreamName, Config: cfg}
}

// WebUIEventsConsumer is web-ui-api's consumer of events.discovered.
func WebUIEventsConsumer() ConsumerSpec {
	cfg := consumerConfig(WebUIEventsConsumerDurableName)
	cfg.FilterSubject = EventsSubject
	return ConsumerSpec{Stream: EventsStreamName, Config: cfg}
}

// WebUINotificationsConsumer is web-ui-api's consumer of Telegram and email
// delivery results. It lists every subject it handles: the email results
// have three tokens, which a "notifications.*" filter never matched.
// JetStream lets CreateOrUpdateConsumer change the filter of an existing
// durable pull consumer, which keeps its position.
func WebUINotificationsConsumer() ConsumerSpec {
	cfg := consumerConfig(WebUINotificationsConsumerDurableName)
	cfg.FilterSubjects = NotificationsSubjects()
	return ConsumerSpec{Stream: NotificationsStreamName, Config: cfg}
}

// EnsureConsumer creates or updates the durable consumer spec describes.
func EnsureConsumer(ctx context.Context, js jetstream.JetStream, spec ConsumerSpec) (jetstream.Consumer, error) {
	stream, err := js.Stream(ctx, spec.Stream)
	if err != nil {
		return nil, fmt.Errorf("get %s stream: %w", spec.Stream, err)
	}
	consumer, err := stream.CreateOrUpdateConsumer(ctx, spec.Config)
	if err != nil {
		return nil, fmt.Errorf("ensure %s consumer: %w", spec.Config.Durable, err)
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

// SkippedBucketName is the JetStream KV bucket where the scraper records
// sessions whose detail page it rejected (bad date, missing title, URL or
// slug), so the hourly run doesn't fetch them again. Only the scraper
// reads or writes it.
const SkippedBucketName = "SCRAPER_SKIPPED_EVENTS"

// skippedTTL is how long a rejected session is left alone: it is fetched
// and parsed again about once a day, in case the page was fixed.
const skippedTTL = 24 * time.Hour

// SkippedBucketConfig is the skipped bucket's config. Unlike the seen
// bucket its keys expire, which is what retries a rejected session.
func SkippedBucketConfig() jetstream.KeyValueConfig {
	return jetstream.KeyValueConfig{
		Bucket:      SkippedBucketName,
		Description: "Event IDs whose detail page the scraper rejected (key: event ID, value: reason)",
		History:     1,
		TTL:         skippedTTL,
		Storage:     jetstream.FileStorage,
	}
}

// EnsureSkippedBucket creates or updates the skipped bucket and returns it.
func EnsureSkippedBucket(ctx context.Context, js jetstream.JetStream) (jetstream.KeyValue, error) {
	kv, err := js.CreateOrUpdateKeyValue(ctx, SkippedBucketConfig())
	if err != nil {
		return nil, fmt.Errorf("ensure %s bucket: %w", SkippedBucketName, err)
	}
	return kv, nil
}
