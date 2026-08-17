package store

import (
	"strings"
	"testing"
)

func TestSchemaContainsBothTables(t *testing.T) {
	if !strings.Contains(Schema, "CREATE TABLE IF NOT EXISTS events") {
		t.Error("Schema missing events table definition")
	}
	if !strings.Contains(Schema, "CREATE TABLE IF NOT EXISTS notifications") {
		t.Error("Schema missing notifications table definition")
	}
}

func TestSchemaContainsEventColumns(t *testing.T) {
	requiredColumns := []string{
		"id",
		"slug",
		"title",
		"venue",
		"category",
		"event_date",
		"url",
		"image_url",
		"discovered_at",
	}
	for _, col := range requiredColumns {
		if !strings.Contains(Schema, col) {
			t.Errorf("Schema missing events column: %s", col)
		}
	}
}

func TestSchemaContainsNotificationColumns(t *testing.T) {
	requiredColumns := []string{
		"id",
		"event_id",
		"status",
		"telegram_message_id",
		"attempted_at",
		"confirmed_at",
		"error",
		"triggered_by",
	}
	for _, col := range requiredColumns {
		if !strings.Contains(Schema, col) {
			t.Errorf("Schema missing notifications column: %s", col)
		}
	}
}

func TestSchemaContainsIndex(t *testing.T) {
	if !strings.Contains(Schema, "CREATE INDEX IF NOT EXISTS idx_notifications_event_id") {
		t.Error("Schema missing idx_notifications_event_id index")
	}
}
