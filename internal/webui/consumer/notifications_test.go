package consumer

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jfms7s/ticket-live-event-scanner/internal/event"
	"github.com/jfms7s/ticket-live-event-scanner/internal/store"
	"github.com/jfms7s/ticket-live-event-scanner/internal/streams"
	"github.com/jfms7s/ticket-live-event-scanner/internal/webui/api"
	"github.com/jfms7s/ticket-live-event-scanner/internal/webui/repository"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	_ "modernc.org/sqlite"
)

// fakeMsg is a jetstream.Msg with a chosen subject and body that counts
// the ack-family calls made on it.
type fakeMsg struct {
	subject string
	data    []byte

	acks, naks, terms int
}

func (m *fakeMsg) Metadata() (*jetstream.MsgMetadata, error) { return &jetstream.MsgMetadata{}, nil }
func (m *fakeMsg) Data() []byte                              { return m.data }
func (m *fakeMsg) Headers() nats.Header                      { return nil }
func (m *fakeMsg) Subject() string                           { return m.subject }
func (m *fakeMsg) Reply() string                             { return "" }
func (m *fakeMsg) Ack() error                                { m.acks++; return nil }
func (m *fakeMsg) DoubleAck(context.Context) error           { m.acks++; return nil }
func (m *fakeMsg) Nak() error                                { m.naks++; return nil }
func (m *fakeMsg) NakWithDelay(time.Duration) error          { m.naks++; return nil }
func (m *fakeMsg) InProgress() error                         { return nil }
func (m *fakeMsg) Term() error                               { m.terms++; return nil }
func (m *fakeMsg) TermWithReason(string) error               { m.terms++; return nil }

func (m *fakeMsg) assertCalls(t *testing.T, acks, naks, terms int) {
	t.Helper()
	if m.acks != acks || m.naks != naks || m.terms != terms {
		t.Fatalf("ack/nak/term = %d/%d/%d, want %d/%d/%d", m.acks, m.naks, m.terms, acks, naks, terms)
	}
}

func jsonMsg(t *testing.T, subject string, v any) *fakeMsg {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return &fakeMsg{subject: subject, data: data}
}

// setupDB opens a temp-file SQLite database with the production schema.
func setupDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+t.TempDir()+"/consumer.db")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	if err := store.Migrate(context.Background(), db); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	return db
}

// purchasedEvent inserts event id and records its purchase, leaving its
// invite pending as PATCH /api/events/{id}/purchased does.
func purchasedEvent(t *testing.T, db *sql.DB, id int64) {
	t.Helper()
	if _, err := db.Exec(`INSERT INTO events (id, slug, title, url) VALUES (?, 's', 'T', 'http://example.com')`, id); err != nil {
		t.Fatalf("insert event: %v", err)
	}
	if err := repository.SetEventPurchased(context.Background(), db, id, true); err != nil {
		t.Fatalf("SetEventPurchased: %v", err)
	}
}

// getEventViaAPI reads the event the way the dashboard does.
func getEventViaAPI(t *testing.T, db *sql.DB, id string) repository.EventResponse {
	t.Helper()
	server := httptest.NewServer(api.NewApp(db, nil).NewMux())
	defer server.Close()

	resp, err := http.Get(server.URL + "/api/events/" + id)
	if err != nil {
		t.Fatalf("GET event: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET event status = %d", resp.StatusCode)
	}
	var ev repository.EventResponse
	if err := json.NewDecoder(resp.Body).Decode(&ev); err != nil {
		t.Fatalf("decode event: %v", err)
	}
	return ev
}

func deref(p *string) string {
	if p == nil {
		return "<nil>"
	}
	return *p
}

func TestEmailFailedMarksInviteFailedInAPI(t *testing.T) {
	db := setupDB(t)
	purchasedEvent(t, db, 98164)

	msg := jsonMsg(t, streams.NotificationsEmailFailSubject, event.EmailFailed{
		EventID:  98164,
		Error:    "smtp: 421 try later",
		FailedAt: time.Date(2026, 10, 17, 10, 0, 0, 0, time.UTC),
		Attempts: 5,
	})
	handleNotification(context.Background(), db, msg)
	msg.assertCalls(t, 1, 0, 0)

	ev := getEventViaAPI(t, db, "98164")
	if deref(ev.InviteStatus) != "failed" || deref(ev.InviteError) != "smtp: 421 try later" {
		t.Fatalf("invite = (%s, %s), want (failed, smtp: 421 try later)", deref(ev.InviteStatus), deref(ev.InviteError))
	}
}

func TestEmailSentMarksInviteSent(t *testing.T) {
	db := setupDB(t)
	purchasedEvent(t, db, 7)

	msg := jsonMsg(t, streams.NotificationsEmailSentSubject, event.EmailSent{
		EventID: 7, MessageID: "<abc@example.com>", SentAt: time.Now(),
	})
	handleNotification(context.Background(), db, msg)
	msg.assertCalls(t, 1, 0, 0)

	ev := getEventViaAPI(t, db, "7")
	if deref(ev.InviteStatus) != "sent" || ev.InviteError != nil {
		t.Fatalf("invite = (%s, %s), want (sent, <nil>)", deref(ev.InviteStatus), deref(ev.InviteError))
	}
}

func TestMalformedEmailResultsAreTerminated(t *testing.T) {
	db := setupDB(t)
	for _, subject := range []string{streams.NotificationsEmailSentSubject, streams.NotificationsEmailFailSubject} {
		msg := &fakeMsg{subject: subject, data: []byte("{not json")}
		handleNotification(context.Background(), db, msg)
		msg.assertCalls(t, 0, 0, 1)
	}
}

func TestEmailResultNaksWhenDBFails(t *testing.T) {
	db := setupDB(t)
	db.Close()

	msg := jsonMsg(t, streams.NotificationsEmailFailSubject, event.EmailFailed{EventID: 1, Error: "x"})
	handleNotification(context.Background(), db, msg)
	msg.assertCalls(t, 0, 1, 0)
}

func TestTelegramResultsStillHandled(t *testing.T) {
	db := setupDB(t)
	ctx := context.Background()
	if err := repository.UpsertEventAndInsertNotification(ctx, db, &event.Discovered{
		EventID: 1, Slug: "s", Title: "T", URL: "http://example.com", Reason: event.ReasonDiscovered,
	}); err != nil {
		t.Fatalf("UpsertEventAndInsertNotification: %v", err)
	}

	msg := jsonMsg(t, streams.NotificationsSentSubject, event.NotificationSent{
		EventID: 1, TelegramMessageID: "42", SentAt: time.Now(),
	})
	handleNotification(ctx, db, msg)
	msg.assertCalls(t, 1, 0, 0)

	ev := getEventViaAPI(t, db, "1")
	if len(ev.Notifications) != 1 || ev.Notifications[0].Status != "sent" {
		t.Fatalf("notifications = %+v, want one sent", ev.Notifications)
	}
}

func TestUnhandledSubjectIsLoggedAndAcked(t *testing.T) {
	var buf bytes.Buffer
	prev := log.Writer()
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(prev) })

	db := setupDB(t)
	msg := &fakeMsg{subject: "notifications.sms.sent", data: []byte(`{}`)}
	handleNotification(context.Background(), db, msg)
	msg.assertCalls(t, 1, 0, 0)

	if !strings.Contains(buf.String(), "WARN unhandled subject notifications.sms.sent") {
		t.Fatalf("log = %q, want a WARN unhandled subject line", buf.String())
	}
}

// The consumer's filter must cover every subject it handles. A single
// "notifications.*" token wildcard silently missed the email subjects.
func TestNotificationsConsumerFiltersHandledSubjects(t *testing.T) {
	cfg := notificationsConsumerConfig()
	if cfg.FilterSubject != "" {
		t.Fatalf("FilterSubject = %q, want it unset in favour of FilterSubjects", cfg.FilterSubject)
	}
	want := []string{
		streams.NotificationsSentSubject,
		streams.NotificationsFailSubject,
		streams.NotificationsEmailSentSubject,
		streams.NotificationsEmailFailSubject,
	}
	for _, s := range want {
		if !slices.Contains(cfg.FilterSubjects, s) {
			t.Errorf("FilterSubjects %v missing %q", cfg.FilterSubjects, s)
		}
	}
	if len(cfg.FilterSubjects) != len(want) {
		t.Errorf("FilterSubjects = %v, want exactly %v", cfg.FilterSubjects, want)
	}
}
