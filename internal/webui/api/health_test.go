package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestHealthz tests the health check endpoint.
func TestHealthz(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	app := &App{db: db, js: NewMockJetStream()}
	server := httptest.NewServer(app.NewMux())
	defer server.Close()

	resp, err := http.Get(server.URL + "/healthz")
	if err != nil {
		t.Fatalf("Failed to make request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Expected status 200, got %d", resp.StatusCode)
	}

	var result string
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if result != "ok" {
		t.Fatalf("Expected 'ok', got %q", result)
	}
}
