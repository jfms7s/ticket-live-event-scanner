import type { Event } from '../types';

// The Hub filter's value for events stored before hubs were recorded.
export const UNKNOWN_HUB = '__unknown__';

// A hub slug like "auchan-live-academia-maia-98164" reads better without
// its trailing ticketline id.
export function hubLabel(slug: string): string {
  return slug.replace(/-\d+$/, '') || slug;
}

// The distinct hubs present in events, sorted by label (the full slug when
// two hubs share a short label). Events without a hub
// are not listed here; see matchesHub for how they filter.
export function hubOptions(events: Event[]): { value: string; label: string }[] {
  const slugs = new Set<string>();
  for (const event of events) {
    if (event.hub_slug) slugs.add(event.hub_slug);
  }
  const labelCounts = new Map<string, number>();
  for (const slug of slugs) {
    const label = hubLabel(slug);
    labelCounts.set(label, (labelCounts.get(label) ?? 0) + 1);
  }
  // Two hubs whose slugs differ only by id would read the same: show the
  // full slug for those.
  return Array.from(slugs)
    .map((value) => {
      const label = hubLabel(value);
      return { value, label: (labelCounts.get(label) ?? 0) > 1 ? value : label };
    })
    .sort((a, b) => a.label.localeCompare(b.label) || a.value.localeCompare(b.value));
}

// True when the event passes the Hub filter: '' keeps everything, UNKNOWN_HUB
// keeps events with no recorded hub, anything else keeps that hub's events.
export function matchesHub(event: Event, hubFilter: string): boolean {
  if (!hubFilter) return true;
  if (hubFilter === UNKNOWN_HUB) return !event.hub_slug;
  return event.hub_slug === hubFilter;
}
