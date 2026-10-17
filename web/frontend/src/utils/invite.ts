import type { Event } from '../types';

export interface InviteBadge {
  type: 'pending' | 'sent' | 'failed';
  label: string;
  error: string | null;
}

const KNOWN = ['pending', 'sent', 'failed'] as const;

// getInviteBadge returns the calendar-invite badge for the Bought column, or
// null when there is nothing to show: the event isn't purchased, or it was
// purchased before invites were tracked (invite_status is null).
export function getInviteBadge(
  event: Pick<Event, 'purchased'> & { invite_status?: string | null; invite_error?: string | null }
): InviteBadge | null {
  if (!event.purchased || !event.invite_status) {
    return null;
  }
  const type = KNOWN.find((s) => s === event.invite_status);
  if (!type) {
    return null;
  }
  return {
    type,
    label: `invite ${type}`,
    error: type === 'failed' ? (event.invite_error ?? null) : null,
  };
}
