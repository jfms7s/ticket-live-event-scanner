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
  discovered_at: string;
  purchased: boolean;
  status: string;
  notifications: Notification[];
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
