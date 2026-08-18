// Global state
let allEvents = [];
let refreshInterval;
let sortState = { key: null, direction: 'asc' };

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
    populateVenueFilter();
    hideError();
    renderEvents();
  } catch (error) {
    showError(`Failed to fetch events: ${error.message}`);
    console.error('Fetch error:', error);
  }
}

// Populate the venue filter dropdown with the unique venues present in allEvents,
// preserving the current selection if it's still a valid option.
function populateVenueFilter() {
  const select = document.getElementById('venueFilter');
  const previousValue = select.value;

  const venues = Array.from(
    new Set(allEvents.map((event) => event.venue).filter(Boolean))
  ).sort((a, b) => a.localeCompare(b));

  select.innerHTML = '';
  const allOption = document.createElement('option');
  allOption.value = '';
  allOption.textContent = 'All';
  select.appendChild(allOption);

  venues.forEach((venue) => {
    const option = document.createElement('option');
    option.value = venue;
    option.textContent = venue;
    select.appendChild(option);
  });

  if (venues.includes(previousValue)) {
    select.value = previousValue;
  }
}

// Get a comparable value for a given event and sort key
function getSortValue(event, key) {
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

// Update the sort indicator arrows in the table headers
function updateSortIndicators() {
  document.querySelectorAll('.events-table th.sortable').forEach((th) => {
    const indicator = th.querySelector('.sort-indicator');
    if (th.dataset.sortKey === sortState.key) {
      indicator.textContent = sortState.direction === 'asc' ? '▲' : '▼';
    } else {
      indicator.textContent = '';
    }
  });
}

// Handle clicking a sortable column header
function handleSortClick(key) {
  if (sortState.key === key) {
    sortState.direction = sortState.direction === 'asc' ? 'desc' : 'asc';
  } else {
    sortState.key = key;
    sortState.direction = 'asc';
  }
  renderEvents();
}

// Set up click listeners on sortable table headers
function setupSortListeners() {
  document.querySelectorAll('.events-table th.sortable').forEach((th) => {
    th.addEventListener('click', () => handleSortClick(th.dataset.sortKey));
  });
}

// Render events table
function renderEvents() {
  const tbody = document.getElementById('eventsTableBody');
  const eventStatusFilter = document.getElementById('eventStatusFilter').value;
  const notificationStatusFilter = document.getElementById('notificationStatusFilter').value;
  const venueFilter = document.getElementById('venueFilter').value;
  const boughtFilter = document.getElementById('boughtFilter').value;

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
    if (venueFilter && event.venue !== venueFilter) {
      return false;
    }
    if (boughtFilter === 'yes' && !event.purchased) {
      return false;
    }
    if (boughtFilter === 'no' && event.purchased) {
      return false;
    }
    return true;
  });

  // Sort events
  if (sortState.key) {
    const { key, direction } = sortState;
    filteredEvents.sort((a, b) => {
      const valueA = getSortValue(a, key);
      const valueB = getSortValue(b, key);
      if (valueA < valueB) return direction === 'asc' ? -1 : 1;
      if (valueA > valueB) return direction === 'asc' ? 1 : -1;
      return 0;
    });
  }
  updateSortIndicators();

  // Render rows
  tbody.innerHTML = ''; // Clear existing rows

  if (filteredEvents.length === 0) {
    const row = document.createElement('tr');
    row.className = 'loading-row';
    const cell = document.createElement('td');
    cell.colSpan = 9;
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

    // Thumbnail
    const thumbCell = document.createElement('td');
    thumbCell.className = 'event-thumb';
    if (event.image_url) {
      const img = document.createElement('img');
      img.src = event.image_url;
      img.alt = '';
      img.loading = 'lazy';
      // A dead poster link shouldn't leave a broken-image icon in the table
      img.onerror = () => thumbCell.replaceChildren();
      thumbCell.appendChild(img);
    }

    // Title (links out to the original ticketline.pt event page, if known)
    const titleCell = document.createElement('td');
    titleCell.className = 'event-title';
    if (event.url) {
      const titleLink = document.createElement('a');
      titleLink.href = event.url;
      titleLink.target = '_blank';
      titleLink.rel = 'noopener noreferrer';
      titleLink.textContent = event.title || 'N/A';
      titleCell.appendChild(titleLink);
    } else {
      titleCell.textContent = event.title || 'N/A';
    }

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

    // Purchased checkbox
    const purchasedCell = document.createElement('td');
    purchasedCell.className = 'event-purchased';
    const purchasedCheckbox = document.createElement('input');
    purchasedCheckbox.type = 'checkbox';
    purchasedCheckbox.checked = !!event.purchased;
    purchasedCheckbox.dataset.eventId = event.id;
    purchasedCheckbox.onchange = (e) => handleTogglePurchased(e, event.id);
    purchasedCell.appendChild(purchasedCheckbox);

    // Action cell (retrigger + delete buttons + status message)
    const actionCell = document.createElement('td');
    const retriggerBtn = document.createElement('button');
    retriggerBtn.className = 'btn-retrigger';
    retriggerBtn.textContent = 'Retrigger';
    retriggerBtn.dataset.eventId = event.id;
    retriggerBtn.onclick = (e) => handleRetrigger(e, event.id);

    const deleteBtn = document.createElement('button');
    deleteBtn.className = 'btn-delete';
    deleteBtn.textContent = 'Remove';
    deleteBtn.dataset.eventId = event.id;
    deleteBtn.onclick = (e) => handleDelete(e, event.id, event.title);

    const statusMsg = document.createElement('div');
    statusMsg.className = 'retrigger-status';
    statusMsg.dataset.eventId = event.id;

    actionCell.appendChild(retriggerBtn);
    actionCell.appendChild(deleteBtn);
    actionCell.appendChild(statusMsg);

    row.appendChild(thumbCell);
    row.appendChild(titleCell);
    row.appendChild(venueCell);
    row.appendChild(categoryCell);
    row.appendChild(dateCell);
    row.appendChild(statusCell);
    row.appendChild(notifCell);
    row.appendChild(purchasedCell);
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

// Handle toggling the "purchased" checkbox for an event
async function handleTogglePurchased(e, eventId) {
  const checkbox = e.target;
  const purchased = checkbox.checked;
  checkbox.disabled = true;

  try {
    const response = await fetch(`${window.API_BASE_URL}/api/events/${eventId}/purchased`, {
      method: 'PATCH',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ purchased }),
    });

    if (!response.ok) {
      throw new Error(`API error: ${response.status}`);
    }

    // Keep local state in sync so a later re-render (e.g. filter change)
    // doesn't revert the checkbox before the next fetch.
    const evt = allEvents.find((ev) => ev.id === eventId);
    if (evt) {
      evt.purchased = purchased;
    }
  } catch (error) {
    checkbox.checked = !purchased; // Revert on failure
    showError(`Failed to update purchased status: ${error.message}`);
    console.error('Toggle purchased error:', error);
  }

  checkbox.disabled = false;
}

// Handle removing an event from the database
async function handleDelete(e, eventId, eventTitle) {
  if (!window.confirm(`Remove "${eventTitle || 'this event'}" from the database? This cannot be undone.`)) {
    return;
  }

  const btn = e.target;
  btn.disabled = true;

  try {
    const response = await fetch(`${window.API_BASE_URL}/api/events/${eventId}`, {
      method: 'DELETE',
    });

    if (!response.ok && response.status !== 404) {
      throw new Error(`API error: ${response.status}`);
    }

    allEvents = allEvents.filter((ev) => ev.id !== eventId);
    renderEvents();
  } catch (error) {
    showError(`Failed to remove event: ${error.message}`);
    console.error('Delete error:', error);
    btn.disabled = false;
  }
}

// Set up event listeners for filters
function setupFilterListeners() {
  // Event status filter requires re-fetch (server-side filtering)
  document.getElementById('eventStatusFilter').addEventListener('change', fetchEvents);
  // Notification status filter only requires re-render (client-side filtering)
  document.getElementById('notificationStatusFilter').addEventListener('change', renderEvents);
  document.getElementById('venueFilter').addEventListener('change', renderEvents);
  document.getElementById('boughtFilter').addEventListener('change', renderEvents);
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
  setupSortListeners();
  fetchEvents();
  startAutoRefresh();
});

// Cleanup on page unload
window.addEventListener('beforeunload', () => {
  if (refreshInterval) {
    clearInterval(refreshInterval);
  }
});
