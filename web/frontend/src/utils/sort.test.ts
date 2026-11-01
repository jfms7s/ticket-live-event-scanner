import { describe, expect, it } from 'vitest';
import type { Event, Notification } from '../types';
import { getSortValue } from './sort';

function event(overrides: Partial<Event>): Event {
  return {
    id: 1,
    slug: 'show',
    title: 'Show',
    venue: null,
    category: null,
    event_date: null,
    url: 'https://example.test/show',
    image_url: null,
    discovered_at: '2026-11-01T10:00:00Z',
    purchased: false,
    status: 'active',
    notifications: [],
    invite_status: null,
    invite_error: null,
    ...overrides,
  };
}

function notification(status: string): Notification {
  return {
    id: 1,
    status,
    telegram_message_id: null,
    attempted_at: '',
    confirmed_at: null,
    error: null,
    triggered_by: 'scraper',
  };
}

describe('getSortValue', () => {
  it('lowercases text keys', () => {
    const e = event({ title: 'Zebra', venue: 'Coliseu', category: 'Música', status: 'Finished' });
    expect(getSortValue(e, 'title')).toBe('zebra');
    expect(getSortValue(e, 'venue')).toBe('coliseu');
    expect(getSortValue(e, 'category')).toBe('música');
    expect(getSortValue(e, 'status')).toBe('finished');
  });

  it('maps null text fields to an empty string', () => {
    const e = event({ venue: null, category: null, event_date: null, status: '' });
    expect(getSortValue(e, 'venue')).toBe('');
    expect(getSortValue(e, 'category')).toBe('');
    expect(getSortValue(e, 'event_date')).toBe('');
    expect(getSortValue(e, 'status')).toBe('');
  });

  it('keeps event_date as-is so ISO dates sort lexically', () => {
    expect(getSortValue(event({ event_date: '2026-12-31T21:00' }), 'event_date')).toBe('2026-12-31T21:00');
  });

  it('uses the latest notification status, or "none"', () => {
    expect(getSortValue(event({ notifications: [notification('failed'), notification('sent')] }), 'notification')).toBe('failed');
    expect(getSortValue(event({ notifications: [] }), 'notification')).toBe('none');
  });

  it('maps purchased to 1 or 0', () => {
    expect(getSortValue(event({ purchased: true }), 'purchased')).toBe(1);
    expect(getSortValue(event({ purchased: false }), 'purchased')).toBe(0);
  });
});
