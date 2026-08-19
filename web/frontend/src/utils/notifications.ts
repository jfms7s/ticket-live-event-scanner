import type { Notification } from '../types';

export interface LatestNotification {
  status: string;
  notif: Notification | null;
}

export function getLatestNotificationStatus(
  notifications: Notification[] | null | undefined
): LatestNotification {
  if (!notifications || !Array.isArray(notifications) || notifications.length === 0) {
    return { status: 'none', notif: null };
  }
  // Return the most recent notification (API returns them in order)
  const notif = notifications[0];
  if (!notif || typeof notif.status !== 'string') {
    return { status: 'none', notif: null };
  }
  return { status: notif.status, notif };
}
