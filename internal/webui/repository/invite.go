package repository

import (
	"context"
	"database/sql"
	"fmt"
	"log"
)

// UpdateInviteSent resolves an event's pending calendar invite as sent. It
// is called for a notifications.email.sent message. There is at most one
// invite per purchase, so the event ID is the key. An event whose invite is
// not pending (a redelivered result, or a purchase from before invites were
// tracked) is left alone and logged.
func UpdateInviteSent(ctx context.Context, db *sql.DB, eventID int64) error {
	return resolveInvite(ctx, db, eventID, "sent", nil)
}

// UpdateInviteFailed resolves an event's pending calendar invite as failed
// with errMsg. It is called for a notifications.email.failed message, and
// by web-ui-api itself when the invite request could not be published.
// Like UpdateInviteSent, it only changes a pending invite.
func UpdateInviteFailed(ctx context.Context, db *sql.DB, eventID int64, errMsg string) error {
	return resolveInvite(ctx, db, eventID, "failed", &errMsg)
}

func resolveInvite(ctx context.Context, db *sql.DB, eventID int64, status string, errMsg *string) error {
	result, err := db.ExecContext(ctx, `
		UPDATE events SET invite_status = ?, invite_error = ?
		WHERE id = ? AND invite_status = 'pending'
	`, status, errMsg, eventID)
	if err != nil {
		return fmt.Errorf("update invite %s: %w", status, err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if rows == 0 {
		log.Printf("No pending invite found for event %d (%s status expected)", eventID, status)
	}
	return nil
}
