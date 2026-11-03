import type { HubStatus } from '../types';

// The scraper runs hourly; two hours without a finished run means it
// missed at least one.
export const STALE_AFTER_MS = 2 * 60 * 60 * 1000;
// Successful runs in a row with no future session before a hub counts as
// empty (the API's runs.EmptyRunsThreshold).
export const EMPTY_RUNS_THRESHOLD = 3;

export type RunHealthLevel = 'ok' | 'warn' | 'bad' | 'unknown';

export interface RunHealth {
  level: RunHealthLevel;
  text: string;
  // Longer explanation for the tooltip.
  detail: string;
}

export function formatAgo(ms: number): string {
  const minutes = Math.max(0, Math.floor(ms / 60000));
  if (minutes < 1) return 'just now';
  if (minutes < 60) return `${minutes} min ago`;
  const hours = Math.floor(minutes / 60);
  if (hours < 48) return `${hours} h ago`;
  return `${Math.floor(hours / 24)} days ago`;
}

function plural(n: number, word: string): string {
  return `${n} ${word}${n === 1 ? '' : 's'}`;
}

/**
 * The header's run status from the newest run's hubs: red ("bad") when the
 * newest run finished over two hours ago or any hub failed in it, amber
 * ("warn") when a hub has listed no future session for three runs, green
 * otherwise. Pure, so it is tested without a clock.
 */
export function runHealth(latest: HubStatus[], now: Date): RunHealth {
  if (latest.length === 0) {
    return { level: 'unknown', text: 'No scraper runs recorded yet', detail: '' };
  }

  const finished = Math.max(...latest.map((h) => Date.parse(h.finished_at)).filter((t) => !Number.isNaN(t)));
  if (!Number.isFinite(finished)) {
    return { level: 'unknown', text: 'Last run time unknown', detail: '' };
  }
  const age = now.getTime() - finished;
  const newEvents = latest.reduce((sum, h) => sum + h.new_events, 0);
  const failed = latest.filter((h) => h.error);
  const empty = latest.filter((h) => !h.error && h.consecutive_empty_runs >= EMPTY_RUNS_THRESHOLD);

  const text = [
    `Last run ${formatAgo(age)}`,
    plural(latest.length, 'hub'),
    `${newEvents} new`,
    plural(failed.length, 'error'),
  ].join(' · ');

  const details: string[] = [];
  for (const h of failed) details.push(`${h.hub_slug}: ${h.error}`);
  for (const h of empty) details.push(`${h.hub_slug}: no upcoming sessions for ${h.consecutive_empty_runs} runs`);

  if (age > STALE_AFTER_MS) {
    return { level: 'bad', text, detail: ['No run in the last 2 hours', ...details].join('\n') };
  }
  if (failed.length > 0) {
    return { level: 'bad', text, detail: details.join('\n') };
  }
  if (empty.length > 0) {
    return { level: 'warn', text: `${text} · ${plural(empty.length, 'empty hub')}`, detail: details.join('\n') };
  }
  return { level: 'ok', text, detail: '' };
}
