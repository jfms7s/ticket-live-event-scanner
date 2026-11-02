package api

import (
	"context"
	"database/sql"
	"github.com/jfms7s/ticket-live-event-scanner/internal/store"
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	_ "modernc.org/sqlite"
)

// setupTestDB creates a file-based SQLite database for testing.
// Using file-based DB instead of :memory: to avoid connection pool issues
func setupTestDB(t *testing.T) *sql.DB {
	// Use file-based SQLite database for unit tests with URI=true for better control
	tempDir := t.TempDir()
	dbPath := "file:" + tempDir + "/test.db?cache=shared"

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("Failed to open test database: %v", err)
	}

	// Limit connection pool to avoid database locking issues in tests
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	// Set reasonable timeouts
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Create tables directly for testing
	if err := store.Migrate(ctx, db); err != nil {
		t.Fatalf("Failed to setup test schema: %v", err)
	}

	return db
}

// MockJetStream is a minimal mock that records published messages.
type MockJetStream struct {
	publishedMessages []publishedMessage
	// publishErr, when set, is returned by every Publish (nothing is recorded).
	publishErr error
}

type publishedMessage struct {
	subject string
	data    []byte
}

func NewMockJetStream() *MockJetStream {
	return &MockJetStream{
		publishedMessages: []publishedMessage{},
	}
}

func (m *MockJetStream) Publish(ctx context.Context, subject string, data []byte, opts ...jetstream.PublishOpt) (*jetstream.PubAck, error) {
	// opts are jetstream.PublishOpt values; in a real test we'd extract msgID,
	// but for this test we just verify the message was published
	_ = opts
	if m.publishErr != nil {
		return nil, m.publishErr
	}
	m.publishedMessages = append(m.publishedMessages, publishedMessage{
		subject: subject,
		data:    data,
	})
	return &jetstream.PubAck{}, nil
}
