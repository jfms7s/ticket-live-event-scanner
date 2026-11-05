package deliver

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/jfms7s/ticket-live-event-scanner/internal/event"
	"github.com/jfms7s/ticket-live-event-scanner/internal/notifier/telegram"
	"github.com/jfms7s/ticket-live-event-scanner/internal/outbox"
	"github.com/jfms7s/ticket-live-event-scanner/internal/store"
)

var t0 = time.Date(2026, 11, 5, 10, 0, 0, 0, time.UTC)

type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *clock) add(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

type fixture struct {
	db    *sql.DB
	clk   *clock
	q     *outbox.Queue
	sleep []time.Duration
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	db, err := store.Open(context.Background(), store.Options{Path: filepath.Join(t.TempDir(), "x.db"), AllowEmpty: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	clk := &clock{t: t0}
	return &fixture{db: db, clk: clk, q: &outbox.Queue{DB: db, Now: clk.now}}
}

func (f *fixture) poller(s Senders) *Poller {
	p := New(f.db, s, 3*time.Second)
	p.Now = f.clk.now
	p.Sleep = func(_ context.Context, d time.Duration) error {
		f.sleep = append(f.sleep, d)
		f.clk.add(max(d, 0))
		return nil
	}
	return p
}

func disc(id int64) *event.Discovered {
	return &event.Discovered{EventID: id, Slug: fmt.Sprintf("s-%d", id), Title: fmt.Sprintf("Show %d", id), URL: "https://x/" + fmt.Sprint(id), EventDate: "2030-01-01", Reason: event.ReasonDiscovered}
}

type nrow struct {
	status, err, msg string
	attempts         int
	next             string
}

func (f *fixture) rows(t *testing.T, eventID int64, channel string) []nrow {
	t.Helper()
	rs, err := f.db.Query(`SELECT status, COALESCE(error,''), COALESCE(telegram_message_id,''), attempts, COALESCE(next_attempt_at,'') FROM notifications WHERE event_id = ? AND channel = ? ORDER BY id`, eventID, channel)
	if err != nil {
		t.Fatal(err)
	}
	defer rs.Close()
	var out []nrow
	for rs.Next() {
		var r nrow
		if err := rs.Scan(&r.status, &r.err, &r.msg, &r.attempts, &r.next); err != nil {
			t.Fatal(err)
		}
		out = append(out, r)
	}
	return out
}

type tg struct {
	sent  []int64
	fails []error // returned in order, then success
}

func (s *tg) send(_ context.Context, d event.Discovered) (string, error) {
	if len(s.fails) > 0 {
		err := s.fails[0]
		s.fails = s.fails[1:]
		return "", err
	}
	s.sent = append(s.sent, d.EventID)
	return fmt.Sprintf("m%d", d.EventID), nil
}

func TestEnqueueTwiceQueuesAndSendsOnce(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	for i := 0; i < 2; i++ {
		queued, err := f.q.EnqueueDiscovered(ctx, disc(1))
		if err != nil {
			t.Fatal(err)
		}
		if queued != (i == 0) {
			t.Errorf("enqueue %d queued = %v", i, queued)
		}
	}
	s := &tg{}
	n, err := f.poller(Senders{Telegram: s.send}).ProcessDue(ctx)
	if err != nil || n != 1 || len(s.sent) != 1 {
		t.Fatalf("processed %d (%v), sent %v; want one", n, err, s.sent)
	}
	got := f.rows(t, 1, "telegram")
	if len(got) != 1 || got[0].status != "sent" || got[0].msg != "m1" || got[0].attempts != 1 || got[0].next != "" {
		t.Errorf("rows = %+v", got)
	}
}

// The notification is queued with the event, in one transaction: if the
// queueing fails nothing of the event remains.
func TestEnqueueIsAtomic(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	if _, err := f.db.Exec(`CREATE TRIGGER refuse BEFORE INSERT ON notifications BEGIN SELECT RAISE(ABORT, 'no'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := f.q.EnqueueDiscovered(ctx, disc(7)); err == nil {
		t.Fatal("enqueue succeeded although the notification insert fails")
	}
	var n int
	f.db.QueryRow(`SELECT COUNT(*) FROM events WHERE id = 7`).Scan(&n)
	if n != 0 {
		t.Error("the event was stored although its notification was not queued (it would never be announced)")
	}
}

func TestRetriesFollowTheScheduleThenParks(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.q.EnqueueDiscovered(ctx, disc(1))
	boom := errors.New("boom")
	s := &tg{fails: []error{boom, boom, boom, boom, boom, boom}}
	p := f.poller(Senders{Telegram: s.send})

	want := []time.Duration{time.Minute, 5 * time.Minute, 30 * time.Minute, 2 * time.Hour, 12 * time.Hour}
	for i, wait := range want {
		if n, _ := p.ProcessDue(ctx); n != 1 {
			t.Fatalf("attempt %d: processed %d", i+1, n)
		}
		r := f.rows(t, 1, "telegram")[0]
		if r.status != "pending" || r.attempts != i+1 || r.err != "boom" {
			t.Fatalf("after attempt %d: %+v", i+1, r)
		}
		if r.next != outbox.Format(f.clk.now().Add(wait)) {
			t.Errorf("after attempt %d next = %s, want now + %v", i+1, r.next, wait)
		}
		// Not due before its time.
		if n, _ := p.ProcessDue(ctx); n != 0 {
			t.Errorf("attempt %d: a row was taken before its retry time", i+1)
		}
		f.clk.add(wait)
	}
	if n, _ := p.ProcessDue(ctx); n != 1 {
		t.Fatal("the sixth attempt was not made")
	}
	r := f.rows(t, 1, "telegram")[0]
	if r.status != "failed" || r.attempts != 6 || r.next != "" {
		t.Errorf("after six failures: %+v, want parked as failed", r)
	}
	f.clk.add(24 * time.Hour)
	if n, _ := p.ProcessDue(ctx); n != 0 {
		t.Error("a failed row was taken again")
	}
}

func TestHonoursTelegramsRetryAfter(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.q.EnqueueDiscovered(ctx, disc(1))
	s := &tg{fails: []error{&telegram.RateLimitedError{RetryAfter: 10 * time.Minute}}}
	f.poller(Senders{Telegram: s.send}).ProcessDue(ctx)
	r := f.rows(t, 1, "telegram")[0]
	if want := outbox.Format(f.clk.now().Add(10*time.Minute + time.Second)); r.next != want {
		t.Errorf("next = %s, want %s (retry_after beats the 1 m backoff)", r.next, want)
	}
}

// A crash between the send and the write leaves the row pending and due:
// after a restart it is sent again (never lost).
func TestACrashAfterTheSendSendsAgain(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.q.EnqueueDiscovered(ctx, disc(1))
	if _, err := f.db.Exec(`CREATE TRIGGER no_result BEFORE UPDATE OF status ON notifications BEGIN SELECT RAISE(ABORT, 'disk gone'); END`); err != nil {
		t.Fatal(err)
	}
	s := &tg{}
	f.poller(Senders{Telegram: s.send}).ProcessDue(ctx) // sends, cannot record
	if len(s.sent) != 1 {
		t.Fatalf("sent %v", s.sent)
	}
	r := f.rows(t, 1, "telegram")[0]
	if r.status != "pending" || r.attempts != 1 {
		t.Fatalf("row = %+v, want pending with the attempt counted", r)
	}

	f.db.Exec(`DROP TRIGGER no_result`)
	f.poller(Senders{Telegram: s.send}).ProcessDue(ctx) // the restarted process
	if len(s.sent) != 2 || f.rows(t, 1, "telegram")[0].status != "sent" {
		t.Errorf("after restart sent %v, row %+v; want it sent again and recorded", s.sent, f.rows(t, 1, "telegram")[0])
	}
}

func TestPacesTelegramSends(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	for id := int64(1); id <= 3; id++ {
		f.q.EnqueueDiscovered(ctx, disc(id))
	}
	s := &tg{}
	p := f.poller(Senders{Telegram: s.send})
	if n, _ := p.ProcessDue(ctx); n != 3 {
		t.Fatalf("processed %d", n)
	}
	if len(f.sleep) != 2 || f.sleep[0] != 3*time.Second {
		t.Errorf("waits between sends = %v, want two of 3s", f.sleep)
	}
	if fmt.Sprint(s.sent) != "[1 2 3]" {
		t.Errorf("sent order %v, want id order", s.sent)
	}
}

func TestADisabledChannelKeepsItsRowsUntouched(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.q.EnqueueDiscovered(ctx, disc(1))
	p := f.poller(Senders{}) // no Telegram sender
	if n, _ := p.ProcessDue(ctx); n != 0 {
		t.Fatalf("processed %d, want a disabled channel's rows left alone", n)
	}
	r := f.rows(t, 1, "telegram")[0]
	if r.status != "pending" || r.attempts != 0 {
		t.Errorf("row = %+v, want untouched (no attempt used)", r)
	}
	// Enabled later, it is sent.
	s := &tg{}
	f.poller(Senders{Telegram: s.send}).ProcessDue(ctx)
	if len(s.sent) != 1 {
		t.Errorf("sent %v after the channel was enabled", s.sent)
	}
}

func TestRetryQueuesAFreshRowAndKeepsTheHistory(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.q.EnqueueDiscovered(ctx, disc(1))
	s := &tg{}
	f.poller(Senders{Telegram: s.send}).ProcessDue(ctx)
	f.clk.add(time.Second)
	if err := f.q.EnqueueRetry(ctx, 1); err != nil {
		t.Fatal(err)
	}
	f.poller(Senders{Telegram: s.send}).ProcessDue(ctx)
	got := f.rows(t, 1, "telegram")
	if len(got) != 2 || got[0].status != "sent" || got[1].status != "sent" || len(s.sent) != 2 {
		t.Errorf("rows %+v, sent %v; want the original kept and a second one sent", got, s.sent)
	}
	if err := f.q.EnqueueRetry(ctx, 999); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("retry of an unknown event: %v, want sql.ErrNoRows", err)
	}
}

type mail struct {
	sent []int64
	err  error
}

func (m *mail) send(_ context.Context, p event.Purchased) (string, error) {
	if m.err != nil {
		return "", m.err
	}
	m.sent = append(m.sent, p.EventID)
	return "<id@x>", nil
}

func (f *fixture) invite(t *testing.T, eventID int64) (status, errText string) {
	t.Helper()
	var s, e sql.NullString
	if err := f.db.QueryRow(`SELECT invite_status, invite_error FROM events WHERE id = ?`, eventID).Scan(&s, &e); err != nil {
		t.Fatal(err)
	}
	return s.String, e.String
}

func TestInviteIsSentAndRecordedOnTheEvent(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.q.EnqueueDiscovered(ctx, disc(1))
	f.db.Exec(`UPDATE notifications SET status = 'sent', next_attempt_at = NULL`) // the Telegram row is done
	f.db.Exec(`UPDATE events SET purchased = 1, invite_status = 'pending' WHERE id = 1`)
	tx, _ := f.db.Begin()
	if err := outbox.EnqueueInvite(ctx, tx, 1, f.clk.now()); err != nil {
		t.Fatal(err)
	}
	tx.Commit()

	m := &mail{}
	f.poller(Senders{Email: m.send}).ProcessDue(ctx)
	if len(m.sent) != 1 {
		t.Fatalf("invites sent: %v", m.sent)
	}
	if s, _ := f.invite(t, 1); s != "sent" {
		t.Errorf("invite_status = %q, want sent", s)
	}
	if got := f.rows(t, 1, "email"); len(got) != 1 || got[0].status != "sent" {
		t.Errorf("email rows %+v", got)
	}
}

func TestAnInviteThatKeepsFailingEndsFailedOnTheEvent(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.q.EnqueueDiscovered(ctx, disc(1))
	f.db.Exec(`UPDATE notifications SET status = 'sent', next_attempt_at = NULL`)
	f.db.Exec(`UPDATE events SET purchased = 1, invite_status = 'pending' WHERE id = 1`)
	tx, _ := f.db.Begin()
	outbox.EnqueueInvite(ctx, tx, 1, f.clk.now())
	tx.Commit()

	m := &mail{err: errors.New("smtp down")}
	p := f.poller(Senders{Email: m.send})
	for i := 0; i < outbox.MaxAttempts; i++ {
		p.ProcessDue(ctx)
		f.clk.add(24 * time.Hour)
	}
	if s, e := f.invite(t, 1); s != "failed" || e != "smtp down" {
		t.Errorf("invite = %q / %q, want failed with the error", s, e)
	}
}

func TestADeletedEventParksItsNotificationAtOnce(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.q.EnqueueDiscovered(ctx, disc(1))
	f.db.Exec(`PRAGMA foreign_keys = OFF`)
	f.db.Exec(`DELETE FROM events WHERE id = 1`)
	s := &tg{}
	f.poller(Senders{Telegram: s.send}).ProcessDue(ctx)
	r := f.rows(t, 1, "telegram")
	if len(r) != 1 || r[0].status != "failed" || len(s.sent) != 0 {
		t.Errorf("rows %+v, sent %v; want parked without a send", r, s.sent)
	}
}

func TestRunStopsOnCancelAndPicksUpNudges(t *testing.T) {
	f := newFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	s := &tg{}
	p := f.poller(Senders{Telegram: s.send})
	p.Interval = time.Hour // only a nudge can wake it in time
	done := make(chan struct{})
	go func() { p.Run(ctx); close(done) }()

	f.q.Nudge = p.Nudge
	f.q.EnqueueDiscovered(ctx, disc(1))
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if rs := f.rows(t, 1, "telegram"); len(rs) == 1 && rs[0].status == "sent" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if rs := f.rows(t, 1, "telegram"); len(rs) != 1 || rs[0].status != "sent" {
		t.Fatalf("a nudged row was not sent: %+v", rs)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not stop after cancel")
	}
}

func TestADisabledChannelDoesNotStarveTheOther(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	// 25 invite rows (email disabled) in front of one Telegram row.
	for i := int64(1); i <= 25; i++ {
		f.q.EnqueueDiscovered(ctx, disc(i))
		f.db.Exec(`UPDATE notifications SET status = 'sent', next_attempt_at = NULL WHERE event_id = ?`, i)
		f.db.Exec(`UPDATE events SET purchased = 1, invite_status = 'pending' WHERE id = ?`, i)
		tx, _ := f.db.Begin()
		outbox.EnqueueInvite(ctx, tx, i, f.clk.now())
		tx.Commit()
	}
	f.q.EnqueueDiscovered(ctx, disc(100))
	s := &tg{}
	p := f.poller(Senders{Telegram: s.send})
	n, err := p.ProcessDue(ctx)
	if err != nil || n != 1 || len(s.sent) != 1 {
		t.Fatalf("processed %d (%v), sent %v; want the Telegram row sent", n, err, s.sent)
	}
	if n, _ := p.ProcessDue(ctx); n != 0 {
		t.Errorf("second pass processed %d; disabled rows must not count toward a full batch", n)
	}
}

func TestShutdownMidSendStillRecordsTheResult(t *testing.T) {
	f := newFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	f.q.EnqueueDiscovered(ctx, disc(1))
	var posts int
	send := func(context.Context, event.Discovered) (string, error) {
		posts++
		cancel() // SIGTERM arrives while the request is in flight
		return "m1", nil
	}
	f.poller(Senders{Telegram: send}).ProcessDue(ctx)
	if got := f.rows(t, 1, "telegram")[0]; got.status != "sent" {
		t.Fatalf("row = %+v, want sent despite the cancelled context", got)
	}
	f.poller(Senders{Telegram: send}).ProcessDue(context.Background())
	if posts != 1 {
		t.Errorf("posted %d times, want once", posts)
	}
}

func TestANewerInviteRowDecidesTheEventsState(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.q.EnqueueDiscovered(ctx, disc(1))
	f.db.Exec(`UPDATE notifications SET status = 'sent', next_attempt_at = NULL`)
	f.db.Exec(`UPDATE events SET purchased = 1, invite_status = 'pending' WHERE id = 1`)
	for i := 0; i < 2; i++ { // tick, untick, tick: two invite rows
		tx, _ := f.db.Begin()
		outbox.EnqueueInvite(ctx, tx, 1, f.clk.now().Add(time.Duration(i)*time.Second))
		tx.Commit()
	}
	// The older row fails for good, the newer one is sent.
	calls := 0
	send := func(context.Context, event.Purchased) (string, error) {
		calls++
		if calls == 1 || calls == 3 {
			return "", errors.New("smtp down")
		}
		return "<id@x>", nil
	}
	p := f.poller(Senders{Email: send})
	f.clk.add(2 * time.Second)
	p.ProcessDue(ctx) // row A fails once, row B sent
	f.db.Exec(`UPDATE notifications SET attempts = ? WHERE channel = 'email' AND status = 'pending'`, outbox.MaxAttempts-1)
	f.clk.add(24 * time.Hour)
	p.ProcessDue(ctx) // row A parks failed
	if s, _ := f.invite(t, 1); s != "sent" {
		t.Errorf("invite_status = %q, want sent (the newest row's outcome)", s)
	}
}
