package parse

import (
	"testing"

	"golang.org/x/net/html"
)

func TestHasClassMultipleClasses(t *testing.T) {
	// Test proper multi-class matching
	tests := []struct {
		classAttr string
		search    string
		expected  bool
	}{
		{"metadata categories", "metadata categories", true},
		{"metadata categories", "categories metadata", true},
		{"metadata categories extra", "metadata categories", true},
		{"metadata", "metadata categories", false},
		{"categories", "metadata categories", false},
		{"notametadata", "metadata", false},
		{"class-metadata", "metadata", false},
		{"a b c", "b c", true},
		{"", "metadata", false},
	}

	for _, tc := range tests {
		// Create a fake node with class attribute
		result := hasClass(&html.Node{
			Type: html.ElementNode,
			Attr: []html.Attribute{
				{Key: "class", Val: tc.classAttr},
			},
		}, tc.search)

		if result != tc.expected {
			t.Errorf("hasClass(class=%q, search=%q): expected %v, got %v",
				tc.classAttr, tc.search, tc.expected, result)
		}
	}
}
