import type { Event, EventStatusFilter } from '../types';

class ApiError extends Error {}

function baseUrl(): string {
  return window.API_BASE_URL;
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

export async function retriggerEvent(eventId: number): Promise<void> {
  const response = await fetch(`${baseUrl()}/api/events/${eventId}/retrigger`, {
    method: 'POST',
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
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ purchased }),
  });
  if (!response.ok) {
    throw new ApiError(`API error: ${response.status}`);
  }
}

export async function deleteEvent(eventId: number): Promise<void> {
  const response = await fetch(`${baseUrl()}/api/events/${eventId}`, {
    method: 'DELETE',
  });
  if (!response.ok && response.status !== 404) {
    throw new ApiError(`API error: ${response.status}`);
  }
}
