package parse

import (
	"os"
	"testing"
)

// The testdata pages were captured once from ticketline.pt on 2026-11-01
// (identifying User-Agent, one request each). They pin the parser to the
// site's real markup, which synthetic snippets can't.

func readTestdata(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestParseHubSessionsRealPage(t *testing.T) {
	sessions, err := ParseHubSessions(readTestdata(t, "hub-auchan-live-academia-maia-98164.html"))
	if err != nil {
		t.Fatal(err)
	}
	wantIDs := []int64{108659, 108661, 108662, 108812, 108729, 108884, 108679}
	if len(sessions) != len(wantIDs) {
		t.Fatalf("parsed %d sessions, want %d: %+v", len(sessions), len(wantIDs), sessions)
	}
	for i, id := range wantIDs {
		if sessions[i].EventID != id {
			t.Errorf("session %d ID = %d, want %d", i, sessions[i].EventID, id)
		}
		if sessions[i].Slug == "" || sessions[i].Title == "" || sessions[i].EventDate == "" {
			t.Errorf("session %d incomplete: %+v", id, sessions[i])
		}
	}
	if s := sessions[3]; s.Slug != "menu-de-natal-108812" || s.Title != "MENU DE NATAL" || s.EventDate != "2026-11-21" {
		t.Errorf("session 108812 = %+v", s)
	}
}

func TestParseEventDetailRealPage(t *testing.T) {
	e, err := ParseEventDetail(readTestdata(t, "detail-menu-de-natal-108812.html"), 108812)
	if err != nil {
		t.Fatal(err)
	}
	if e.Title != "Menu De Natal" {
		t.Errorf("Title = %q", e.Title)
	}
	if e.EventDate != "2026-11-21T18:00" {
		t.Errorf("EventDate = %q", e.EventDate)
	}
	if e.Venue != "Academia Auchan Live | Loja Da Maia" {
		t.Errorf("Venue = %q", e.Venue)
	}
	if e.ImageURL == "" {
		t.Error("ImageURL empty, want the poster")
	}
}
