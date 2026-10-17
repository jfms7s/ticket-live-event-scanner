import { describe, expect, it } from 'vitest';
import { getInviteBadge } from './invite';

describe('getInviteBadge', () => {
  it('shows nothing for an event that is not purchased', () => {
    expect(getInviteBadge({ purchased: false, invite_status: 'sent', invite_error: null })).toBeNull();
  });

  it('shows nothing when the invite state is unknown', () => {
    expect(getInviteBadge({ purchased: true, invite_status: null, invite_error: null })).toBeNull();
    expect(getInviteBadge({ purchased: true, invite_status: undefined, invite_error: undefined })).toBeNull();
  });

  it.each(['pending', 'sent'] as const)('labels a %s invite', (status) => {
    expect(getInviteBadge({ purchased: true, invite_status: status, invite_error: null })).toEqual({
      type: status,
      label: `invite ${status}`,
      error: null,
    });
  });

  it('carries the error of a failed invite', () => {
    expect(getInviteBadge({ purchased: true, invite_status: 'failed', invite_error: 'smtp down' })).toEqual({
      type: 'failed',
      label: 'invite failed',
      error: 'smtp down',
    });
  });

  it('ignores a status it does not know', () => {
    expect(getInviteBadge({ purchased: true, invite_status: 'bogus', invite_error: null })).toBeNull();
  });
});
