// Global state
let allEvents = [];
let refreshInterval;

// Utility: Create a badge element (helper for DOM creation)
function createBadge(status, type) {
  const badge = document.createElement('span');
  badge.className = `badge badge-${type}`;
  badge.textContent = status.charAt(0).toUpperCase() + status.slice(1);
  return badge;
}

// Show error banner
function showError(message) {
  const banner = document.getElementById('errorBanner');
  banner.innerHTML = '';

  const messageSpan = document.createElement('span');
  messageSpan.textContent = message;

  const closeBtn = document.createElement('button');
  closeBtn.textContent = '×';
  closeBtn.onclick = () => banner.style.display = 'none';

  banner.appendChild(messageSpan);
  banner.appendChild(closeBtn);
  banner.style.display = 'flex';
}

// Hide error banner
function hideError() {
  document.getElementById('errorBanner').style.display = 'none';
}

// Fetch events from API
async function fetchEvents() {
  try {
    // Build URL with event status query parameter if filter is set
    const eventStatusFilter = document.getElementById('eventStatusFilter').value;
    let url = `${window.API_BASE_URL}/api/events`;
    if (eventStatusFilter) {
      url += `?status=${encodeURIComponent(eventStatusFilter)}`;
    }

    const response = await fetch(url);
    if (!response.ok) {
      throw new Error(`API error: ${response.status}`);
    }
    allEvents = await response.json() || [];
    hideError();
    renderEvents();
  } catch (error) {
    showError(`Failed to fetch events: ${error.message}`);
    console.error('Fetch error:', error);
  }
}

// Render events table
function renderEvents() {
  const tbody = document.getElementById('eventsTableBody');
  const eventStatusFilter = document.getElementById('eventStatusFilter').value;
  const notificationStatusFilter = document.getElementById('notificationStatusFilter').value;

  // Filter events
  const filteredEvents = allEvents.filter((event) => {
    if (eventStatusFilter && event.status !== eventStatusFilter) {
      return false;
    }
    if (notificationStatusFilter) {
      const { status } = getLatestNotificationStatus(event.notifications);
      if (status !== notificationStatusFilter && notificationStatusFilter !== 'none') {
        return false;
      }
      if (notificationStatusFilter === 'none' && status !== 'none') {
        return false;
      }
    }
    return true;
  });

  // Render rows
  tbody.innerHTML = ''; // Clear existing rows

  if (filteredEvents.length === 0) {
    const row = document.createElement('tr');
    row.className = 'loading-row';
    const cell = document.createElement('td');
    cell.colSpan = 7;
    cell.textContent = 'No events found';
    row.appendChild(cell);
    tbody.appendChild(row);
    return;
  }

  // Build and append all rows at once
  const fragment = document.createDocumentFragment();

  filteredEvents.forEach((event) => {
    const { status: notifStatus, notif } = getLatestNotificationStatus(event.notifications);
    const row = document.createElement('tr');
    row.dataset.eventId = event.id;

    // Title
    const titleCell = document.createElement('td');
    titleCell.className = 'event-title';
    titleCell.textContent = event.title || 'N/A';

    // Venue
    const venueCell = document.createElement('td');
    venueCell.className = 'event-venue';
    venueCell.textContent = event.venue || 'N/A';

    // Category
    const categoryCell = document.createElement('td');
    categoryCell.textContent = event.category || 'N/A';

    // Date
    const dateCell = document.createElement('td');
    dateCell.textContent = formatDate(event.event_date);

    // Status badge
    const statusCell = document.createElement('td');
    statusCell.appendChild(createBadge(event.status || 'unknown', event.status || 'unknown'));

    // Notification status badge + details
    const notifCell = document.createElement('td');
    if (notifStatus === 'none') {
      notifCell.appendChild(createBadge('None yet', 'none'));
    } else {
      const badgeContainer = document.createElement('div');
      badgeContainer.appendChild(createBadge(notifStatus, notifStatus));

      // Show error details for failed notifications
      if (notifStatus === 'failed' && notif && notif.error) {
        const errorDetail = document.createElement('div');
        errorDetail.className = 'notification-error-detail';
        errorDetail.title = notif.error; // Tooltip with full error
        errorDetail.textContent = `Error: ${notif.error.substring(0, 50)}${notif.error.length > 50 ? '...' : ''}`;
        badgeContainer.appendChild(errorDetail);
      }

      notifCell.appendChild(badgeContainer);
    }

    // Action cell (retrigger button + status message)
    const actionCell = document.createElement('td');
    const retriggerBtn = document.createElement('button');
    retriggerBtn.className = 'btn-retrigger';
    retriggerBtn.textContent = 'Retrigger';
    retriggerBtn.dataset.eventId = event.id;
    retriggerBtn.onclick = (e) => handleRetrigger(e, event.id);

    const statusMsg = document.createElement('div');
    statusMsg.className = 'retrigger-status';
    statusMsg.dataset.eventId = event.id;

    actionCell.appendChild(retriggerBtn);
    actionCell.appendChild(statusMsg);

    row.appendChild(titleCell);
    row.appendChild(venueCell);
    row.appendChild(categoryCell);
    row.appendChild(dateCell);
    row.appendChild(statusCell);
    row.appendChild(notifCell);
    row.appendChild(actionCell);

    fragment.appendChild(row);
  });

  tbody.appendChild(fragment);
}

// Handle retrigger action
async function handleRetrigger(e, eventId) {
  const btn = e.target;
  const statusMsg = document.querySelector(`.retrigger-status[data-event-id="${eventId}"]`);

  btn.disabled = true;
  statusMsg.className = 'retrigger-status loading';
  statusMsg.textContent = 'Retriggering...';

  try {
    const response = await fetch(`${window.API_BASE_URL}/api/events/${eventId}/retrigger`, {
      method: 'POST',
    });

    if (response.status === 202 || response.ok) {
      statusMsg.className = 'retrigger-status success';
      statusMsg.textContent = 'Retrigger sent ✓';

      // Refresh event data after a short delay
      setTimeout(() => {
        fetchEvents();
      }, 500);
    } else if (response.status === 404) {
      statusMsg.className = 'retrigger-status error';
      statusMsg.textContent = 'Event not found';
    } else {
      statusMsg.className = 'retrigger-status error';
      statusMsg.textContent = `Error: ${response.status}`;
    }
  } catch (error) {
    statusMsg.className = 'retrigger-status error';
    statusMsg.textContent = `Error: ${error.message}`;
    console.error('Retrigger error:', error);
  }

  // Clear status message and re-enable button after 3 seconds
  setTimeout(() => {
    statusMsg.textContent = '';
    btn.disabled = false;
  }, 3000);
}

// Set up event listeners for filters
function setupFilterListeners() {
  // Event status filter requires re-fetch (server-side filtering)
  document.getElementById('eventStatusFilter').addEventListener('change', fetchEvents);
  // Notification status filter only requires re-render (client-side filtering)
  document.getElementById('notificationStatusFilter').addEventListener('change', renderEvents);
  document.getElementById('refreshButton').addEventListener('click', fetchEvents);
}

// Initialize auto-refresh (every 30 seconds)
function startAutoRefresh() {
  if (refreshInterval) {
    clearInterval(refreshInterval);
  }
  refreshInterval = setInterval(fetchEvents, 30 * 1000);
}

// Initialize on page load
document.addEventListener('DOMContentLoaded', () => {
  if (!window.API_BASE_URL) {
    showError('Configuration error: API_BASE_URL is not defined. Please ensure config.js is loaded correctly.');
    return;
  }
  console.log('App initialized. API Base URL:', window.API_BASE_URL);
  setupFilterListeners();
  fetchEvents();
  startAutoRefresh();
});

// Cleanup on page unload
window.addEventListener('beforeunload', () => {
  if (refreshInterval) {
    clearInterval(refreshInterval);
  }
});
