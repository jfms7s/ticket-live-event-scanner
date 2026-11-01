import type { Event, EventStatusFilter } from '../types';

export class ApiError extends Error {}

// config.js (served by server.js) sets API_BASE_URL before the app loads. If
// it didn't load, fail with a clear message instead of fetching
// "undefined/api/…" and showing a generic network error.
function baseUrl(): string {
  const url = window.API_BASE_URL;
  if (!url) {
    throw new ApiError('Configuration error: API_BASE_URL is not set (config.js missing?)');
  }
  return url.endsWith('/') ? url.slice(0, -1) : url;
}

export async function fetchEvents(status: EventStatusFilter): Promise<Event[]> {
  let url = `${baseUrl()}/api/events`;
  if (status) {
    url += `?status=${encodeURIComponent(status)}`;
  }
  const response = await fetch(url);
  if (!response.ok) {
    throw new ApiError(`API error: ${response.status}`);
  }
  return (await response.json()) || [];
}

// The API refuses POST, PATCH and DELETE without this header (a cross-site
// page can't add it without passing CORS); see RequireCustomHeader.
const MUTATION_HEADERS = { 'X-Requested-With': 'ticket-scanner' };

export async function retriggerEvent(eventId: number): Promise<void> {
  const response = await fetch(`${baseUrl()}/api/events/${eventId}/retrigger`, {
    method: 'POST',
    headers: MUTATION_HEADERS,
  });
  if (response.status === 202 || response.ok) {
    return;
  }
  if (response.status === 404) {
    throw new ApiError('Event not found');
  }
  throw new ApiError(`Error: ${response.status}`);
}

export async function setPurchased(eventId: number, purchased: boolean): Promise<void> {
  const response = await fetch(`${baseUrl()}/api/events/${eventId}/purchased`, {
    method: 'PATCH',
    headers: { ...MUTATION_HEADERS, 'Content-Type': 'application/json' },
    body: JSON.stringify({ purchased }),
  });
  if (!response.ok) {
    throw new ApiError(`API error: ${response.status}`);
  }
}

export async function deleteEvent(eventId: number): Promise<void> {
  const response = await fetch(`${baseUrl()}/api/events/${eventId}`, {
    method: 'DELETE',
    headers: MUTATION_HEADERS,
  });
  if (!response.ok && response.status !== 404) {
    throw new ApiError(`API error: ${response.status}`);
  }
}
