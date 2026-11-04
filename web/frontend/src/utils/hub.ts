import type { Event } from '../types';

// The Hub filter's value for events stored before hubs were recorded.
export const UNKNOWN_HUB = '__unknown__';

// A hub slug like "auchan-live-academia-maia-98164" reads better without
// its trailing ticketline id.
export function hubLabel(slug: string): string {
  return slug.replace(/-\d+$/, '') || slug;
}

// The distinct hubs present in events, sorted by label. Events without a hub
// are not listed here; see matchesHub for how they filter.
export function hubOptions(events: Event[]): { value: string; label: string }[] {
  const slugs = new Set<string>();
  for (const event of events) {
    if (event.hub_slug) slugs.add(event.hub_slug);
  }
  return Array.from(slugs)
    .map((value) => ({ value, label: hubLabel(value) }))
    .sort((a, b) => a.label.localeCompare(b.label) || a.value.localeCompare(b.value));
}

// True when the event passes the Hub filter: '' keeps everything, UNKNOWN_HUB
// keeps events with no recorded hub, anything else keeps that hub's events.
export function matchesHub(event: Event, hubFilter: string): boolean {
  if (!hubFilter) return true;
  if (hubFilter === UNKNOWN_HUB) return !event.hub_slug;
  return event.hub_slug === hubFilter;
}
