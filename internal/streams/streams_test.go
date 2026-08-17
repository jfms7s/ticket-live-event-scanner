package streams

import (
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
