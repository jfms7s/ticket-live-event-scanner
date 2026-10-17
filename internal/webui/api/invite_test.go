package api

import (
	"encoding/json"
	"errors"
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

func patchPurchased(t *testing.T, server *httptest.Server, id, body string) {
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
	app := &App{db: db, js: NewMockJetStream()}
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

// If the invite request never reaches NATS, no result will ever arrive,
// so the invite is marked failed rather than left pending forever.
func TestSetPurchasedPublishFailureMarksInviteFailed(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	mockJS := NewMockJetStream()
	mockJS.publishErr = errors.New("nats: no responders")
	app := &App{db: db, js: mockJS}
	server := httptest.NewServer(app.NewMux())
	defer server.Close()
	insertPlainEvent(t, app, "6")

	patchPurchased(t, server, "6", `{"purchased":true}`)

	status, errMsg := getInvite(t, server, "6")
	if status == nil || *status != "failed" {
		t.Fatalf("invite_status = %v, want failed", status)
	}
	if errMsg == nil || !strings.Contains(*errMsg, "nats: no responders") {
		t.Fatalf("invite_error = %v, want it to carry the publish error", errMsg)
	}
}
