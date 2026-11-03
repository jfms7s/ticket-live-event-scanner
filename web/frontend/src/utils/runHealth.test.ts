import { describe, expect, it } from 'vitest';
import type { HubStatus } from '../types';
import { formatAgo, runHealth } from './runHealth';

const now = new Date('2026-11-03T12:00:00Z');

function hub(overrides: Partial<HubStatus> = {}): HubStatus {
  return {
    run_id: '2026-11-03T11:48:00.000Z',
    hub_id: 98164,
    hub_slug: 'auchan-live-academia-maia-98164',
    started_at: '2026-11-03T11:47:00Z',
    finished_at: '2026-11-03T11:48:00Z',
    sessions_seen: 6,
    future_sessions: 4,
    new_events: 0,
    last_success_at: '2026-11-03T11:48:00Z',
    upcoming_sessions: 4,
    consecutive_empty_runs: 0,
    ...overrides,
  };
}

describe('runHealth', () => {
  it('is unknown with no runs', () => {
    expect(runHealth([], now).level).toBe('unknown');
  });

  it('is ok for a recent clean run and says so', () => {
    const h = runHealth([hub(), hub({ hub_id: 98167, hub_slug: 'aveiro-98167', new_events: 2 })], now);
    expect(h.level).toBe('ok');
    expect(h.text).toBe('Last run 12 min ago · 2 hubs · 2 new · 0 errors');
  });

  it('is bad when the newest run is older than two hours', () => {
    const h = runHealth([hub({ finished_at: '2026-11-03T09:59:00Z' })], now);
    expect(h.level).toBe('bad');
    expect(h.text).toContain('Last run 2 h ago');
  });

  it('is ok just under two hours', () => {
    expect(runHealth([hub({ finished_at: '2026-11-03T10:01:00Z' })], now).level).toBe('ok');
  });

  it('is bad when a hub failed in the newest run', () => {
    const h = runHealth([hub(), hub({ hub_slug: 'aveiro-98167', error: 'fetch hub page 98167: timeout' })], now);
    expect(h.level).toBe('bad');
    expect(h.text).toContain('1 error');
    expect(h.detail).toContain('aveiro-98167: fetch hub page 98167: timeout');
  });

  it('is warn when a hub has been empty for three runs', () => {
    const h = runHealth([hub({ future_sessions: 0, consecutive_empty_runs: 3 })], now);
    expect(h.level).toBe('warn');
    expect(h.text).toContain('1 empty hub');
  });

  it('is not yet warn after two empty runs', () => {
    expect(runHealth([hub({ consecutive_empty_runs: 2 })], now).level).toBe('ok');
  });

  it('prefers bad over warn', () => {
    const h = runHealth([hub({ consecutive_empty_runs: 5 }), hub({ hub_slug: 'x', error: 'boom' })], now);
    expect(h.level).toBe('bad');
  });

  it('uses the newest finish across hubs', () => {
    const h = runHealth([hub({ finished_at: '2026-11-03T08:00:00Z' }), hub({ finished_at: '2026-11-03T11:30:00Z' })], now);
    expect(h.level).toBe('ok');
    expect(h.text).toContain('30 min ago');
  });

  it('is unknown when no finish time parses', () => {
    expect(runHealth([hub({ finished_at: 'garbage' })], now).level).toBe('unknown');
  });
});

describe('formatAgo', () => {
  it.each([
    [0, 'just now'],
    [-5000, 'just now'],
    [59 * 60000, '59 min ago'],
    [3 * 3600000, '3 h ago'],
    [3 * 86400000, '3 days ago'],
  ])('%d ms is %s', (ms, want) => {
    expect(formatAgo(ms)).toBe(want);
  });
});
