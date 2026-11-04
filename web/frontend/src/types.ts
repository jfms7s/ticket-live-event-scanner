export interface Notification {
  id: number;
  status: string;
  telegram_message_id: string | null;
  attempted_at: string;
  confirmed_at: string | null;
  error: string | null;
  triggered_by: string;
}

export interface Event {
  id: number;
  slug: string;
  title: string;
  venue: string | null;
  category: string | null;
  event_date: string | null;
  url: string;
  image_url: string | null;
  // The hub page that found the event; null for events stored before hubs were recorded.
  hub_slug: string | null;
  discovered_at: string;
  purchased: boolean;
  status: string;
  notifications: Notification[];
  // Calendar-invite email state; null when the event was never purchased,
  // or was purchased before invites were tracked.
  invite_status: 'pending' | 'sent' | 'failed' | null;
  invite_error: string | null;
}

export type SortKey =
  | 'title'
  | 'venue'
  | 'category'
  | 'event_date'
  | 'status'
  | 'notification'
  | 'purchased';

export type SortDirection = 'asc' | 'desc';

export interface SortState {
  key: SortKey | null;
  direction: SortDirection;
}

export type EventStatusFilter = '' | 'active' | 'finished';
export type NotificationStatusFilter = '' | 'pending' | 'sent' | 'failed' | 'none';
export type BoughtFilter = '' | 'yes' | 'no';

// One hub's outcome in one scraper run (GET /api/runs).
export interface Run {
  run_id: string;
  hub_id: number;
  hub_slug: string;
  started_at: string;
  finished_at: string;
  sessions_seen: number;
  future_sessions: number;
  new_events: number;
  // Absent when the hub did not fail.
  error?: string;
}

// A hub of the newest run, with what its older runs say.
export interface HubStatus extends Run {
  last_success_at: string | null;
  upcoming_sessions: number;
  consecutive_empty_runs: number;
}

export interface RunsSummary {
  latest: HubStatus[];
  runs: Run[];
}

// A tracked ticketline.pt hub page (GET /api/watches).
export interface Watch {
  slug: string;
  label: string;
  enabled: boolean;
  added_at: string;
  hub_id: number;
}
