package health

import (
	"context"
	"errors"
	"testing"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

type fakeConsumer struct{ err error }

func (f fakeConsumer) Info(context.Context) (*jetstream.ConsumerInfo, error) {
	if f.err != nil {
		return nil, f.err
	}
	return &jetstream.ConsumerInfo{}, nil
}

func TestNATSConsumerCheck(t *testing.T) {
	tests := []struct {
		name    string
		status  nats.Status
		infoErr error
		wantErr bool
	}{
		{"connected and consumer answers", nats.CONNECTED, nil, false},
		{"reconnecting", nats.RECONNECTING, nil, true},
		{"closed", nats.CLOSED, nil, true},
		{"consumer gone", nats.CONNECTED, errors.New("consumer not found"), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			check := NATSConsumerCheck(func() nats.Status { return tt.status }, fakeConsumer{err: tt.infoErr})
			if err := check(context.Background()); (err != nil) != tt.wantErr {
				t.Fatalf("check() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestFile(t *testing.T) {
	t.Setenv("HEALTH_FILE", "")
	if got := File(); got != DefaultFile {
		t.Fatalf("File() = %q, want %q", got, DefaultFile)
	}
	t.Setenv("HEALTH_FILE", "/run/x")
	if got := File(); got != "/run/x" {
		t.Fatalf("File() = %q, want /run/x", got)
	}
}
