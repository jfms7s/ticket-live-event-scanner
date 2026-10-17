package streams

import (
	"github.com/nats-io/nats.go/jetstream"

	"testing"
	"time"
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

func TestEventsConsumerConfig(t *testing.T) {
	if EventsConsumerDurableName != "telegram-notifier" {
		t.Errorf("EventsConsumerDurableName = %q, want %q", EventsConsumerDurableName, "telegram-notifier")
	}
	if EventsConsumerMaxDeliver != 5 {
		t.Errorf("EventsConsumerMaxDeliver = %d, want %d", EventsConsumerMaxDeliver, 5)
	}
	if EventsConsumerAckWait != 30*time.Second {
		t.Errorf("EventsConsumerAckWait = %v, want %v", EventsConsumerAckWait, 30*time.Second)
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
