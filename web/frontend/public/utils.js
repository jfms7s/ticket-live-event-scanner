// Utility functions for event and notification processing

function getLatestNotificationStatus(notifications) {
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

function formatDate(dateStr) {
  if (!dateStr) return 'N/A';

  // Pull the date and, if present, an "HH:MM" time straight out of the
  // string instead of going through `new Date(dateStr)` for the time part:
  // the scraper stores a timezone-less "YYYY-MM-DDTHH:MM" (event start
  // time, as published on ticketline.pt with no UTC offset attached), and
  // some DB drivers round-trip that into RFC3339 with a trailing "Z" that
  // does NOT represent an actual UTC conversion — just re-serialization of
  // the same digits. Parsing that through Date() would silently shift the
  // displayed hour by the browser's UTC offset for no reason.
  const match = /^(\d{4}-\d{2}-\d{2})(?:T(\d{2}):(\d{2}))?/.exec(dateStr);
  if (!match) return 'N/A';
  const [, datePart, hh, mm] = match;

  const date = new Date(`${datePart}T00:00:00Z`);
  if (isNaN(date.getTime())) return 'N/A';
  const formattedDate = date.toLocaleDateString('en-US', {
    year: 'numeric',
    month: 'short',
    day: 'numeric',
    timeZone: 'UTC',
  });

  // No time component, or a "00:00" that's typically just the DB's
  // midnight placeholder for a date that was never given a time — either
  // way, there's no real time of day to show.
  if (!hh || (hh === '00' && mm === '00')) {
    return formattedDate;
  }

  return `${formattedDate}, ${hh}:${mm}`;
}

function escapeHtml(text) {
  const map = {
    '&': '&amp;',
    '<': '&lt;',
    '>': '&gt;',
    '"': '&quot;',
    "'": '&#039;',
  };
  return String(text).replace(/[&<>"']/g, (ch) => map[ch]);
}

// Export for browser global scope (if not in Node.js)
if (typeof window !== 'undefined') {
  window.getLatestNotificationStatus = getLatestNotificationStatus;
  window.formatDate = formatDate;
  window.escapeHtml = escapeHtml;
}

// Export for Node.js/CommonJS
if (typeof module !== 'undefined' && module.exports) {
  module.exports = {
    getLatestNotificationStatus,
    formatDate,
    escapeHtml,
  };
}
