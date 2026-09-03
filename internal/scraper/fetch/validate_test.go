package fetch

import "testing"

func TestValidateURLAllowedPaths(t *testing.T) {
	baseURL := "https://www.ticketline.pt"

	tests := []struct {
		url       string
		shouldErr bool
	}{
		{baseURL + "/agenda/2026/08", false},
		{baseURL + "/pesquisa/?month=8&year=2026&page=1", false},
		{baseURL + "/evento/test-event-12345", false},
		{baseURL + "/cart/checkout", true},               // Not allowed
		{baseURL + "/carrinho", true},                    // Not allowed
		{"https://malicious.com/evento/event-123", true}, // Wrong domain
		{baseURL + "/admin/users", true},                 // Not allowed
	}

	for _, tc := range tests {
		err := validateURL(tc.url, baseURL)
		if (err != nil) != tc.shouldErr {
			t.Errorf("validateURL(%q): expected error=%v, got %v", tc.url, tc.shouldErr, err)
		}
	}
}
