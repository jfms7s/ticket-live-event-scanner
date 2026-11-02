package streams

import (
	"slices"
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"
)

func TestEventStreamConstants(t *testing.T) {
	if EventsStreamName != "EVENTS" {
		t.Errorf("EventsStreamName = %q, want %q", EventsStreamName, "EVENTS")
	}
	if EventsSubject != "events.discovered" {
		t.Errorf("EventsSubject = %q, want %q", EventsSubject, "events.discovered")
	}
}

func TestNotificationsStreamConstants(t *testing.T) {
	if NotificationsStreamName != "NOTIFICATIONS" {
		t.Errorf("NotificationsStreamName = %q, want %q", NotificationsStreamName, "NOTIFICATIONS")
	}
	if NotificationsSentSubject != "notifications.sent" {
		t.Errorf("NotificationsSentSubject = %q, want %q", NotificationsSentSubject, "notifications.sent")
	}
	if NotificationsFailSubject != "notifications.failed" {
		t.Errorf("NotificationsFailSubject = %q, want %q", NotificationsFailSubject, "notifications.failed")
	}
}

// TestConsumerSpecs pins every durable consumer: a changed durable name
// or filter makes JetStream create a new consumer that replays the stream.
// The values are byte-identical to the ones before the specs moved here.
func TestConsumerSpecs(t *testing.T) {
	tests := []struct {
		name           string
		spec           ConsumerSpec
		stream         string
		durable        string
		filterSubject  string
		filterSubjects []string
	}{
		{"telegram", TelegramConsumer(), "EVENTS", "telegram-notifier", "events.discovered", nil},
		{"email", EmailConsumer(), "EVENTS", "email-notifier", "events.purchased", nil},
		{"web-ui events", WebUIEventsConsumer(), "EVENTS", "web-ui-api-events", "events.discovered", nil},
		{"web-ui notifications", WebUINotificationsConsumer(), "NOTIFICATIONS", "web-ui-api-notifications", "",
			[]string{"notifications.sent", "notifications.failed", "notifications.email.sent", "notifications.email.failed"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := tt.spec.Config
			if tt.spec.Stream != tt.stream {
				t.Errorf("Stream = %q, want %q", tt.spec.Stream, tt.stream)
			}
			if c.Durable != tt.durable {
				t.Errorf("Durable = %q, want %q", c.Durable, tt.durable)
			}
			if c.FilterSubject != tt.filterSubject {
				t.Errorf("FilterSubject = %q, want %q", c.FilterSubject, tt.filterSubject)
			}
			if !slices.Equal(c.FilterSubjects, tt.filterSubjects) {
				t.Errorf("FilterSubjects = %v, want %v", c.FilterSubjects, tt.filterSubjects)
			}
			if c.AckPolicy != jetstream.AckExplicitPolicy {
				t.Errorf("AckPolicy = %v, want explicit", c.AckPolicy)
			}
			if c.MaxDeliver != 5 || c.AckWait != 30*time.Second {
				t.Errorf("MaxDeliver/AckWait = %d/%v, want 5/30s", c.MaxDeliver, c.AckWait)
			}
		})
	}
}

// TestEventsStreamDedupWindowSpansScrapeRuns pins the EVENTS stream's
// duplicate window: the scraper publishes with WithMsgID(eventID) once an
// hour, so the server's 2-minute default dedups nothing between runs.
func TestEventsStreamDedupWindowSpansScrapeRuns(t *testing.T) {
	cfg := EventsStreamConfig()
	if cfg.Duplicates != 7*24*time.Hour {
		t.Errorf("EVENTS Duplicates = %v, want %v", cfg.Duplicates, 7*24*time.Hour)
	}
	if cfg.Duplicates > cfg.MaxAge {
		t.Errorf("EVENTS Duplicates %v exceeds MaxAge %v", cfg.Duplicates, cfg.MaxAge)
	}
	if cfg.Name != EventsStreamName {
		t.Errorf("EVENTS Name = %q, want %q", cfg.Name, EventsStreamName)
	}
}

func TestSeenBucketConfig(t *testing.T) {
	cfg := SeenBucketConfig()
	if cfg.Bucket != SeenBucketName {
		t.Errorf("Bucket = %q, want %q", cfg.Bucket, SeenBucketName)
	}
	if cfg.TTL != 0 {
		t.Errorf("TTL = %v, want 0: a seen key must never expire, or the event is notified again", cfg.TTL)
	}
	if cfg.Storage != jetstream.FileStorage {
		t.Errorf("Storage = %v, want file storage", cfg.Storage)
	}
}

func TestSkippedBucketConfig(t *testing.T) {
	cfg := SkippedBucketConfig()
	if cfg.Bucket != SkippedBucketName || cfg.Bucket == SeenBucketName {
		t.Errorf("Bucket = %q, want %q (not the seen bucket)", cfg.Bucket, SkippedBucketName)
	}
	if cfg.TTL != 24*time.Hour {
		t.Errorf("TTL = %v, want 24h: a rejected session is retried about once a day", cfg.TTL)
	}
}
