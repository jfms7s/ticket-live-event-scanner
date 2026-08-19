import type { Event, SortKey } from '../types';
import { getLatestNotificationStatus } from './notifications';

export function getSortValue(event: Event, key: SortKey): string | number {
  switch (key) {
    case 'title':
      return (event.title || '').toLowerCase();
    case 'venue':
      return (event.venue || '').toLowerCase();
    case 'category':
      return (event.category || '').toLowerCase();
    case 'event_date':
      return event.event_date || '';
    case 'status':
      return (event.status || '').toLowerCase();
    case 'notification':
      return getLatestNotificationStatus(event.notifications).status;
    case 'purchased':
      return event.purchased ? 1 : 0;
    default:
      return '';
  }
}
