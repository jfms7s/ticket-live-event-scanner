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

	// DigestStreamName / DigestSubject carry the weekly digest from
	// web-ui-api's weekly digest job to telegram-notifier (one message a week, already
	// formatted). A stream of its own: EnsureStreams replaces a stream's
	// whole subject list, so on EVENTS any older image starting up would
	// drop the subject; older images never touch DIGEST.
	DigestStreamName = "DIGEST"
	DigestSubject    = "scraper.digest"

	NotificationsStreamName       = "NOTIFICATIONS"
	NotificationsSentSubject      = "notifications.sent"
	NotificationsFailSubject      = "notifications.failed"
	NotificationsEmailSentSubject = "notifications.email.sent"
	NotificationsEmailFailSubject = "notifications.email.failed"

	// Durable consumer names. Changing one makes JetStream create a new
	// consumer that replays the stream, so they never change.
	TelegramConsumerDurableName           = "telegram-notifier"
	TelegramDigestConsumerDurableName     = "telegram-notifier-digest"
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

// digestDedupWindow covers a week, so a second publish with the same
// digest-<ISO week> message ID is dropped.
const digestDedupWindow = 7 * 24 * time.Hour

// DigestStreamConfig is the DIGEST stream's config.
func DigestStreamConfig() jetstream.StreamConfig {
	return jetstream.StreamConfig{
		Name:       DigestStreamName,
		Subjects:   []string{DigestSubject},
		Storage:    jetstream.FileStorage,
		Retention:  jetstream.LimitsPolicy,
		MaxAge:     30 * 24 * time.Hour,
		Duplicates: digestDedupWindow,
	}
}

// EnsureStreams creates or updates the EVENTS, NOTIFICATIONS and DIGEST streams.
// Safe to call from every service on startup; JetStream treats it as a
// no-op when the config already matches.
func EnsureStreams(ctx context.Context, js jetstream.JetStream) error {
	if _, err := js.CreateOrUpdateStream(ctx, EventsStreamConfig()); err != nil {
		return fmt.Errorf("ensure %s stream: %w", EventsStreamName, err)
	}

	if _, err := js.CreateOrUpdateStream(ctx, NotificationsStreamConfig()); err != nil {
		return fmt.Errorf("ensure %s stream: %w", NotificationsStreamName, err)
	}

	if _, err := js.CreateOrUpdateStream(ctx, DigestStreamConfig()); err != nil {
		return fmt.Errorf("ensure %s stream: %w", DigestStreamName, err)
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

// TelegramDigestConsumer is telegram-notifier's consumer of the weekly
// digest. It is separate from TelegramConsumer, so the digest has its own
// handler and the event consumer's filter and position stay as they are.
func TelegramDigestConsumer() ConsumerSpec {
	cfg := consumerConfig(TelegramDigestConsumerDurableName)
	cfg.FilterSubject = DigestSubject
	return ConsumerSpec{Stream: DigestStreamName, Config: cfg}
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

// RunsBucketName is the JetStream KV bucket where the scraper records the
// outcome of each run, one entry per hub (see package runs for the key and
// value). The scraper writes it; web-ui-api reads it for GET /api/runs and
// /metrics, and the weekly digest reads it for its "runs" line.
const RunsBucketName = "SCRAPER_RUNS"

// runsTTL keeps a little over a week of runs: the weekly digest counts the
// last seven days.
const runsTTL = 8 * 24 * time.Hour

// RunsBucketConfig is the runs bucket's config. Both the scraper and
// web-ui-api apply it, so release them together after changing it.
func RunsBucketConfig() jetstream.KeyValueConfig {
	return jetstream.KeyValueConfig{
		Bucket:      RunsBucketName,
		Description: "Scraper run outcomes (key: <hub id>.<run start unix ms>, value: JSON run record)",
		History:     1,
		TTL:         runsTTL,
		Storage:     jetstream.FileStorage,
	}
}

// EnsureRunsBucket creates or updates the runs bucket and returns it.
func EnsureRunsBucket(ctx context.Context, js jetstream.JetStream) (jetstream.KeyValue, error) {
	kv, err := js.CreateOrUpdateKeyValue(ctx, RunsBucketConfig())
	if err != nil {
		return nil, fmt.Errorf("ensure %s bucket: %w", RunsBucketName, err)
	}
	return kv, nil
}
