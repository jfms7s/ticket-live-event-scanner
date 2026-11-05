package api

import (
	"encoding/json"
	"github.com/jfms7s/ticket-live-event-scanner/internal/outbox"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jfms7s/ticket-live-event-scanner/internal/webui/repository"
)

func insertPlainEvent(t *testing.T, app *App, id string) {
	t.Helper()
	if _, err := app.db.Exec(`
		INSERT INTO events (id, slug, title, url, discovered_at)
		VALUES (?, 'slug', 'Title', 'http://example.com', '2026-08-17T09:00:00Z')
	`, id); err != nil {
		t.Fatalf("insert event: %v", err)
	}
}

// patchPurchasedResp sends the PATCH and returns the response, body closed.
func patchPurchasedResp(t *testing.T, server *httptest.Server, id, body string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodPatch, server.URL+"/api/events/"+id+"/purchased", strings.NewReader(body))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PATCH: %v", err)
	}
	resp.Body.Close()
	return resp
}

func patchPurchased(t *testing.T, server *httptest.Server, id, body string) {
	t.Helper()
	resp := patchPurchasedResp(t, server, id, body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PATCH status = %d, want 200", resp.StatusCode)
	}
}

func getInvite(t *testing.T, server *httptest.Server, id string) (status, errMsg *string) {
	t.Helper()
	resp, err := http.Get(server.URL + "/api/events/" + id)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	var ev repository.EventResponse
	if err := json.NewDecoder(resp.Body).Decode(&ev); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return ev.InviteStatus, ev.InviteError
}

func TestSetPurchasedMarksInvitePending(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	app := &App{db: db, queue: &outbox.Queue{DB: db}}
	server := httptest.NewServer(app.NewMux())
	defer server.Close()
	insertPlainEvent(t, app, "5")

	if status, _ := getInvite(t, server, "5"); status != nil {
		t.Fatalf("invite_status before purchase = %q, want null", *status)
	}

	patchPurchased(t, server, "5", `{"purchased":true}`)

	status, errMsg := getInvite(t, server, "5")
	if status == nil || *status != "pending" || errMsg != nil {
		t.Fatalf("invite = (%v, %v), want (pending, null)", status, errMsg)
	}
}

// The flag and its invite are written together: if the invite cannot be
// queued, the event is not marked purchased (it would otherwise show an
// invite that will never be sent).
func TestSetPurchasedIsAtomicWithItsInvite(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	app := &App{db: db, queue: &outbox.Queue{DB: db}}
	server := httptest.NewServer(app.NewMux())
	defer server.Close()
	insertPlainEvent(t, app, "6")
	if _, err := db.Exec(`CREATE TRIGGER refuse_invite BEFORE INSERT ON notifications BEGIN SELECT RAISE(ABORT, 'disk full'); END`); err != nil {
		t.Fatal(err)
	}

	resp := patchPurchasedResp(t, server, "6", `{"purchased":true}`)
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", resp.StatusCode)
	}
	status, _ := getInvite(t, server, "6")
	if status != nil {
		t.Fatalf("invite_status = %q after a failed purchase, want null", *status)
	}
	var purchased bool
	db.QueryRow(`SELECT purchased FROM events WHERE id = 6`).Scan(&purchased)
	if purchased {
		t.Error("the event was marked purchased although its invite could not be queued")
	}
}

// Ticking an already-purchased event (a double submit, or a client retry)
// changes nothing, so it must not send a second invite.
func TestSetPurchasedTwicePublishesOnce(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	app := &App{db: db, queue: &outbox.Queue{DB: db}}
	server := httptest.NewServer(app.NewMux())
	defer server.Close()
	insertPlainEvent(t, app, "7")

	patchPurchased(t, server, "7", `{"purchased":true}`)
	patchPurchased(t, server, "7", `{"purchased":true}`)

	if n := queued(t, db, "email"); n != 1 {
		t.Fatalf("published %d purchased messages, want 1", n)
	}
}

// Unticking and ticking again is a real re-purchase (and the way to retry
// a failed invite), so it publishes again and resets the invite to pending.
func TestSetPurchasedAgainAfterUntickPublishesAgain(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	app := &App{db: db, queue: &outbox.Queue{DB: db}}
	server := httptest.NewServer(app.NewMux())
	defer server.Close()
	insertPlainEvent(t, app, "8")

	patchPurchased(t, server, "8", `{"purchased":true}`)
	if err := repository.UpdateInviteFailed(t.Context(), db, 8, "smtp down"); err != nil {
		t.Fatalf("UpdateInviteFailed: %v", err)
	}
	patchPurchased(t, server, "8", `{"purchased":false}`)
	patchPurchased(t, server, "8", `{"purchased":true}`)

	if n := queued(t, db, "email"); n != 2 {
		t.Fatalf("published %d purchased messages, want 2", n)
	}
	status, errMsg := getInvite(t, server, "8")
	if status == nil || *status != "pending" || errMsg != nil {
		t.Fatalf("invite = (%v, %v), want (pending, null)", status, errMsg)
	}
}

// Unticking never publishes.
func TestSetNotPurchasedDoesNotPublish(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	app := &App{db: db, queue: &outbox.Queue{DB: db}}
	server := httptest.NewServer(app.NewMux())
	defer server.Close()
	insertPlainEvent(t, app, "9")

	patchPurchased(t, server, "9", `{"purchased":false}`)
	patchPurchased(t, server, "9", `{"purchased":true}`)
	patchPurchased(t, server, "9", `{"purchased":false}`)

	if n := queued(t, db, "email"); n != 1 {
		t.Fatalf("published %d purchased messages, want 1", n)
	}
}
