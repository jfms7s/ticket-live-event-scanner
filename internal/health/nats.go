package health

import (
	"context"
	"fmt"

	"github.com/jfms7s/ticket-live-event-scanner/internal/envconfig"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

// File returns HEALTH_FILE, or DefaultFile when it is unset.
func File() string {
	return envconfig.String("HEALTH_FILE", DefaultFile)
}

// consumerInfo is the part of jetstream.Consumer the check needs.
type consumerInfo interface {
	Info(ctx context.Context) (*jetstream.ConsumerInfo, error)
}

// NATSConsumerCheck reports healthy when the NATS connection is up and the
// server still answers for the durable consumer. Pass nc.Status.
func NATSConsumerCheck(status func() nats.Status, consumer consumerInfo) func(context.Context) error {
	return func(ctx context.Context) error {
		if s := status(); s != nats.CONNECTED {
			return fmt.Errorf("NATS connection is %v", s)
		}
		if _, err := consumer.Info(ctx); err != nil {
			return fmt.Errorf("consumer info: %w", err)
		}
		return nil
	}
}
