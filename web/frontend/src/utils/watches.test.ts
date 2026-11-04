import { describe, expect, it } from 'vitest';
import type { HubStatus, Watch } from '../types';
import { watchName, watchRunSummary, watchUrl } from './watches';

function watch(overrides: Partial<Watch> = {}): Watch {
  return { slug: 'fado-hub-500', label: '', enabled: true, added_at: '2026-11-04T12:00:00Z', hub_id: 500, ...overrides };
}

function hub(overrides: Partial<HubStatus>): HubStatus {
  return {
    run_id: '', hub_id: 500, hub_slug: 'fado-hub-500', started_at: '', finished_at: '', sessions_seen: 3,
    future_sessions: 2, new_events: 0, last_success_at: null, upcoming_sessions: 2, consecutive_empty_runs: 0,
    ...overrides,
  };
}

describe('watchName', () => {
  it('prefers the label, else the slug without its id', () => {
    expect(watchName(watch({ label: 'Fado Lisboa' }))).toBe('Fado Lisboa');
    expect(watchName(watch())).toBe('fado-hub');
  });
});

describe('watchRunSummary', () => {
  it('says Disabled for a disabled watch, whatever the run log holds', () => {
    expect(watchRunSummary(watch({ enabled: false }), [hub({})])).toBe('Disabled');
  });
  it('says Not scraped yet when the run log has no row for it', () => {
    expect(watchRunSummary(watch(), [])).toBe('Not scraped yet');
  });
  it('reports a failed run', () => {
    expect(watchRunSummary(watch(), [hub({ error: 'timeout' })])).toBe('Last run failed');
  });
  it('counts upcoming sessions', () => {
    expect(watchRunSummary(watch(), [hub({ upcoming_sessions: 2 })])).toBe('2 upcoming sessions');
    expect(watchRunSummary(watch(), [hub({ upcoming_sessions: 1 })])).toBe('1 upcoming session');
    expect(watchRunSummary(watch(), [hub({ upcoming_sessions: 0 })])).toBe('0 upcoming sessions');
  });
});

describe('watchUrl', () => {
  it('points at the ticketline hub page', () => {
    expect(watchUrl(watch())).toBe('https://www.ticketline.pt/evento/fado-hub-500');
  });
});
