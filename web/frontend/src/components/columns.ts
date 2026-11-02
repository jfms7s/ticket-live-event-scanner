import type { SortKey } from '../types';

export interface Column {
  key: SortKey;
  label: string;
}

export const COLUMNS: Column[] = [
  { key: 'title', label: 'Title' },
  { key: 'venue', label: 'Venue' },
  { key: 'category', label: 'Category' },
  { key: 'event_date', label: 'Date' },
  { key: 'status', label: 'Status' },
  { key: 'notification', label: 'Notification' },
  { key: 'purchased', label: 'Bought' },
];

// The sortable columns plus the thumbnail column before them and the action
// column after them. Full-width rows (loading, empty) span this many cells.
export const TABLE_COLUMN_COUNT = COLUMNS.length + 2;
