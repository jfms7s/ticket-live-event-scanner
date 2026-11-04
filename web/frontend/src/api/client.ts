import type { Event, EventStatusFilter, RunsSummary, Watch } from '../types';

export class ApiError extends Error {}

export async function fetchEvents(status: EventStatusFilter, signal?: AbortSignal): Promise<Event[]> {
  let url = `/api/events`;
  if (status) {
    url += `?status=${encodeURIComponent(status)}`;
  }
  const response = await fetch(url, { signal });
  if (!response.ok) {
    throw new ApiError(`API error: ${response.status}`);
  }
  return (await response.json()) || [];
}

export async function fetchRuns(signal?: AbortSignal): Promise<RunsSummary> {
  const response = await fetch('/api/runs?limit=24', { signal });
  if (!response.ok) {
    throw new ApiError(`API error: ${response.status}`);
  }
  const data = (await response.json()) as Partial<RunsSummary> | null;
  return { latest: data?.latest ?? [], runs: data?.runs ?? [] };
}

// The API refuses POST, PATCH and DELETE without this header (a cross-site
// page can't add it without passing CORS); see RequireCustomHeader.
const MUTATION_HEADERS = { 'X-Requested-With': 'ticket-scanner' };

export async function retriggerEvent(eventId: number): Promise<void> {
  const response = await fetch(`/api/events/${eventId}/retrigger`, {
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
  const response = await fetch(`/api/events/${eventId}/purchased`, {
    method: 'PATCH',
    headers: { ...MUTATION_HEADERS, 'Content-Type': 'application/json' },
    body: JSON.stringify({ purchased }),
  });
  if (!response.ok) {
    throw new ApiError(`API error: ${response.status}`);
  }
}

export async function deleteEvent(eventId: number): Promise<void> {
  const response = await fetch(`/api/events/${eventId}`, {
    method: 'DELETE',
    headers: MUTATION_HEADERS,
  });
  if (!response.ok && response.status !== 404) {
    throw new ApiError(`API error: ${response.status}`);
  }
}

// The API's error text ({"error": "..."}) when it has one.
async function errorMessage(response: Response): Promise<string> {
  try {
    const body = (await response.json()) as { error?: string };
    if (body.error) return body.error;
  } catch {
    // Not JSON: fall through to the status.
  }
  return `API error: ${response.status}`;
}

export async function fetchWatches(signal?: AbortSignal): Promise<Watch[]> {
  const response = await fetch('/api/watches', { signal });
  if (!response.ok) {
    throw new ApiError(`API error: ${response.status}`);
  }
  return (await response.json()) || [];
}

export async function addWatch(url: string, label: string): Promise<Watch> {
  const response = await fetch('/api/watches', {
    method: 'POST',
    headers: { ...MUTATION_HEADERS, 'Content-Type': 'application/json' },
    body: JSON.stringify({ url, label }),
  });
  if (!response.ok) {
    throw new ApiError(await errorMessage(response));
  }
  return response.json();
}

export async function updateWatch(slug: string, patch: { enabled?: boolean; label?: string }): Promise<Watch> {
  const response = await fetch(`/api/watches/${encodeURIComponent(slug)}`, {
    method: 'PATCH',
    headers: { ...MUTATION_HEADERS, 'Content-Type': 'application/json' },
    body: JSON.stringify(patch),
  });
  if (!response.ok) {
    throw new ApiError(await errorMessage(response));
  }
  return response.json();
}
