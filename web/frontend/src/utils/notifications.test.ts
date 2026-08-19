import { describe, expect, it } from 'vitest';
import type { Notification } from '../types';
import { getLatestNotificationStatus } from './notifications';

function notification(overrides: Partial<Notification>): Notification {
  return {
    id: 0,
    status: 'pending',
    telegram_message_id: null,
    attempted_at: '',
    confirmed_at: null,
    error: null,
    triggered_by: 'system',
    ...overrides,
  };
}

describe('getLatestNotificationStatus', () => {
  it('returns "none" for empty notifications array', () => {
    const result = getLatestNotificationStatus([]);
    expect(result.status).toBe('none');
    expect(result.notif).toBeNull();
  });

  it('returns "none" for null notifications', () => {
    const result = getLatestNotificationStatus(null);
    expect(result.status).toBe('none');
    expect(result.notif).toBeNull();
  });

  it('returns the first notification status', () => {
    const notifications = [
      notification({ id: 1, status: 'sent', attempted_at: '2026-08-17T10:00:00Z' }),
      notification({ id: 2, status: 'pending', attempted_at: '2026-08-17T09:00:00Z' }),
    ];
    const result = getLatestNotificationStatus(notifications);
    expect(result.status).toBe('sent');
    expect(result.notif).toEqual(notifications[0]);
  });

  it('handles a single notification', () => {
    const notifications = [notification({ id: 1, status: 'failed' })];
    const result = getLatestNotificationStatus(notifications);
    expect(result.status).toBe('failed');
  });
});
