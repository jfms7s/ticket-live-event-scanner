package job

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

func TestShouldRecord(t *testing.T) {
	for name, tc := range map[string]struct {
		err  error
		want bool
	}{
		"success":            {nil, true},
		"a hub failed":       {errors.New("discovery failed: 1 error(s)"), true},
		"timed out":          {fmt.Errorf("discovery failed: %w", context.DeadlineExceeded), true},
		"shutdown":           {context.Canceled, false},
		"wrapped shutdown":   {fmt.Errorf("discovery failed: %w", context.Canceled), false},
		"no enabled watches": {errors.New("no enabled watch"), true},
	} {
		if got := shouldRecord(tc.err); got != tc.want {
			t.Errorf("%s: shouldRecord = %v, want %v", name, got, tc.want)
		}
	}
}
