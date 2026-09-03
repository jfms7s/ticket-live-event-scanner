package parse

import "testing"

func TestParseEventURLVariations(t *testing.T) {
	tests := []struct {
		url        string
		expectSlug string
		expectID   int64
	}{
		{"/evento/auchan-live-academia-maia-98164", "auchan-live-academia-maia-98164", 98164},
		{"/evento/simple-test-1", "simple-test-1", 1},
		{"/evento/multi-word-slug-with-many-dashes-999", "multi-word-slug-with-many-dashes-999", 999},
	}

	for _, tc := range tests {
		slug, id := parseEventURL(tc.url)
		if slug != tc.expectSlug {
			t.Errorf("parseEventURL(%s) slug: expected %s, got %s", tc.url, tc.expectSlug, slug)
		}
		if id != tc.expectID {
			t.Errorf("parseEventURL(%s) id: expected %d, got %d", tc.url, tc.expectID, id)
		}
	}
}
