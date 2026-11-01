package handler

import (
	"context"
	"testing"
)

// TestResultCtxSurvivesShutdown checks that a delivery result is still
// published when shutdown has cancelled the send's context.
func TestResultCtxSurvivesShutdown(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	pubCtx, pubCancel := resultCtx(ctx)
	defer pubCancel()
	if err := pubCtx.Err(); err != nil {
		t.Fatalf("result context err = %v after the send context was cancelled, want nil", err)
	}
	if _, ok := pubCtx.Deadline(); !ok {
		t.Error("result context has no deadline, want resultPublishTimeout")
	}
}
