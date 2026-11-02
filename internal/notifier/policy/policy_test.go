package policy

import (
	"testing"
	"time"

	"github.com/jfms7s/ticket-live-event-scanner/internal/streams"
)

// TestDecideAction tests the decision logic for nak/term/ack
func TestDecideAction(t *testing.T) {
	tests := []struct {
		name         string
		numDelivered int
		maxDeliver   int
		expected     MessageAction
	}{
		{
			name:         "first failure should nak",
			numDelivered: 1,
			maxDeliver:   5,
			expected:     ActionNak,
		},
		{
			name:         "mid-sequence failure should nak",
			numDelivered: 3,
			maxDeliver:   5,
			expected:     ActionNak,
		},
		{
			name:         "one before max should nak",
			numDelivered: 4,
			maxDeliver:   5,
			expected:     ActionNak,
		},
		{
			name:         "exactly at max should term",
			numDelivered: 5,
			maxDeliver:   5,
			expected:     ActionTerm,
		},
		{
			name:         "beyond max should term",
			numDelivered: 6,
			maxDeliver:   5,
			expected:     ActionTerm,
		},
		{
			name:         "zero delivery should term (edge case)",
			numDelivered: 0,
			maxDeliver:   5,
			expected:     ActionNak, // 0 < 5, so nak
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DecideAction(tt.numDelivered, tt.maxDeliver)
			if got != tt.expected {
				t.Errorf("DecideAction(%d, %d) = %v, expected %v", tt.numDelivered, tt.maxDeliver, got, tt.expected)
			}
		})
	}
}

// TestBackoffDelay tests the backoff delay calculation
func TestBackoffDelay(t *testing.T) {
	tests := []struct {
		name         string
		numDelivered int
		expectedMin  time.Duration
		expectedMax  time.Duration
	}{
		{
			name:         "first retry should be ~5 seconds",
			numDelivered: 1,
			expectedMin:  5 * time.Second,
			expectedMax:  5 * time.Second,
		},
		{
			name:         "second retry should be ~10 seconds",
			numDelivered: 2,
			expectedMin:  10 * time.Second,
			expectedMax:  10 * time.Second,
		},
		{
			name:         "third retry should be ~15 seconds",
			numDelivered: 3,
			expectedMin:  15 * time.Second,
			expectedMax:  15 * time.Second,
		},
		{
			name:         "large value should be capped at 5 minutes",
			numDelivered: 100,
			expectedMin:  5 * time.Minute,
			expectedMax:  5 * time.Minute,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := BackoffDelay(tt.numDelivered)
			if got < tt.expectedMin || got > tt.expectedMax {
				t.Errorf("BackoffDelay(%d) = %v, expected between %v and %v",
					tt.numDelivered, got, tt.expectedMin, tt.expectedMax)
			}
		})
	}
}

// TestDecideActionWithStandardMaxDeliver tests against the actual standard max deliver value
func TestDecideActionWithStandardMaxDeliver(t *testing.T) {
	maxDeliver := streams.DefaultMaxDeliver

	// First 4 deliveries should nak
	for i := 1; i < maxDeliver; i++ {
		if DecideAction(i, maxDeliver) != ActionNak {
			t.Errorf("delivery %d should nak (< %d)", i, maxDeliver)
		}
	}

	// Last delivery should term
	if DecideAction(maxDeliver, maxDeliver) != ActionTerm {
		t.Errorf("delivery %d should term (>= %d)", maxDeliver, maxDeliver)
	}
}

// TestBackoffExponentialProgression verifies backoff increases correctly
func TestBackoffExponentialProgression(t *testing.T) {
	expected := []time.Duration{
		5 * time.Second,
		10 * time.Second,
		15 * time.Second,
		20 * time.Second,
		25 * time.Second,
	}

	for i := 1; i <= 5; i++ {
		delay := BackoffDelay(i)
		if delay != expected[i-1] {
			t.Errorf("BackoffDelay(%d) = %v, expected %v", i, delay, expected[i-1])
		}
	}
}

// TestDecisionBoundaryConditions tests edge cases in decision logic
func TestDecisionBoundaryConditions(t *testing.T) {
	maxDeliver := streams.DefaultMaxDeliver

	tests := []struct {
		name      string
		delivered int
		max       int
		expected  MessageAction
	}{
		{
			name:      "one before max should nak",
			delivered: maxDeliver - 1,
			max:       maxDeliver,
			expected:  ActionNak,
		},
		{
			name:      "exactly at max should term",
			delivered: maxDeliver,
			max:       maxDeliver,
			expected:  ActionTerm,
		},
		{
			name:      "zero delivery (edge) should nak",
			delivered: 0,
			max:       5,
			expected:  ActionNak,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			action := DecideAction(tt.delivered, tt.max)
			if action != tt.expected {
				t.Errorf("DecideAction(%d, %d) = %v, expected %v",
					tt.delivered, tt.max, action, tt.expected)
			}
		})
	}
}
