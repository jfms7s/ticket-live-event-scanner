import type { HubStatus, Watch } from '../types';
import { hubLabel } from './hub';

// What the Watches page shows for a watch: its label, else the slug without
// its ticketline id.
export function watchName(watch: Watch): string {
  return watch.label || hubLabel(watch.slug);
}

// The newest run's outcome for a watch, from GET /api/runs. A disabled
// watch has none (the API leaves it out), and a new one has none until the
// next scraper run.
export function watchRunSummary(watch: Watch, latest: HubStatus[]): string {
  if (!watch.enabled) return 'Disabled';
  const hub = latest.find((h) => h.hub_slug === watch.slug);
  if (!hub) return 'Not scraped yet';
  if (hub.error) return 'Last run failed';
  const n = hub.upcoming_sessions;
  return `${n} upcoming session${n === 1 ? '' : 's'}`;
}

// The hub page on ticketline.pt for a watch.
export function watchUrl(watch: Watch): string {
  return `https://www.ticketline.pt/evento/${encodeURIComponent(watch.slug)}`;
}
