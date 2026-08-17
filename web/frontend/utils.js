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
  const date = new Date(dateStr);
  // Check if date is valid
  if (isNaN(date.getTime())) return 'N/A';
  return date.toLocaleDateString('en-US', { year: 'numeric', month: 'short', day: 'numeric' });
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
