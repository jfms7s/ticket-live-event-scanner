import { describe, expect, it } from 'vitest';
import type { Event } from '../types';
import { UNKNOWN_HUB, hubLabel, hubOptions, matchesHub } from './hub';

function ev(hub_slug: string | null): Event {
  return { hub_slug } as Event;
}

describe('hubLabel', () => {
  it('drops the trailing ticketline id', () => {
    expect(hubLabel('auchan-live-academia-maia-98164')).toBe('auchan-live-academia-maia');
  });
  it('keeps a slug that is only digits', () => {
    expect(hubLabel('98164')).toBe('98164');
  });
});

describe('hubOptions', () => {
  it('lists each hub once, sorted by label, skipping events with no hub', () => {
    const options = hubOptions([ev('b-2'), ev(null), ev('a-1'), ev('b-2')]);
    expect(options).toEqual([
      { value: 'a-1', label: 'a' },
      { value: 'b-2', label: 'b' },
    ]);
  });
});

describe('hubOptions label collisions', () => {
  it('shows the full slug when two hubs share a short label', () => {
    expect(hubOptions([ev('foo-100'), ev('foo-200'), ev('bar-1')])).toEqual([
      { value: 'bar-1', label: 'bar' },
      { value: 'foo-100', label: 'foo-100' },
      { value: 'foo-200', label: 'foo-200' },
    ]);
  });
});

describe('matchesHub', () => {
  it('keeps everything when no hub is selected', () => {
    expect(matchesHub(ev('a-1'), '')).toBe(true);
    expect(matchesHub(ev(null), '')).toBe(true);
  });
  it('keeps only the selected hub', () => {
    expect(matchesHub(ev('a-1'), 'a-1')).toBe(true);
    expect(matchesHub(ev('b-2'), 'a-1')).toBe(false);
    expect(matchesHub(ev(null), 'a-1')).toBe(false);
  });
  it('selects events with no recorded hub', () => {
    expect(matchesHub(ev(null), UNKNOWN_HUB)).toBe(true);
    expect(matchesHub(ev('a-1'), UNKNOWN_HUB)).toBe(false);
  });
});
