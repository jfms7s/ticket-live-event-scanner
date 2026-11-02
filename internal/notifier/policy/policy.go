// Package policy decides how a failed notification delivery should be
// retried: nak with backoff, or give up and terminate the message.
package policy

import "time"

// MessageAction is the decision to make on a failed message. The zero value
// is not a valid action.
type MessageAction int

const (
	// ActionNak means retry the message after a delay
	ActionNak MessageAction = iota + 1
	// ActionTerm means stop retrying this message permanently
	ActionTerm
)

// DecideAction determines whether to nak with delay or terminate a message
// based on the number of deliveries and the maximum allowed.
func DecideAction(numDelivered, maxDeliver int) MessageAction {
	if numDelivered >= maxDeliver {
		return ActionTerm
	}
	return ActionNak
}

// BackoffDelay calculates linear backoff delay: numDelivered * 5 seconds
func BackoffDelay(numDelivered int) time.Duration {
	// Cap at 5 minutes to avoid excessively long delays
	maxDelay := 5 * time.Minute
	delay := time.Duration(numDelivered) * 5 * time.Second
	if delay > maxDelay {
		delay = maxDelay
	}
	return delay
}
