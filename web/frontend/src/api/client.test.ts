import { afterEach, describe, expect, it, vi } from 'vitest';
import { deleteEvent, fetchEvents, fetchRuns } from './client';

function stubFetch(response: Partial<Response>) {
  const fetchMock = vi.fn().mockResolvedValue(response);
  vi.stubGlobal('fetch', fetchMock);
  return fetchMock;
}

afterEach(() => {
  vi.unstubAllGlobals();
});

describe('api client', () => {
  it('adds the status filter', async () => {
    const fetchMock = stubFetch({ ok: true, status: 200, json: async () => [] });

    await expect(fetchEvents('active')).resolves.toEqual([]);
    expect(fetchMock).toHaveBeenCalledWith('/api/events?status=active', { signal: undefined });
  });

  it('forwards the abort signal to fetch', async () => {
    const fetchMock = stubFetch({ ok: true, status: 200, json: async () => [] });
    const controller = new AbortController();

    await fetchEvents('', controller.signal);
    expect(fetchMock).toHaveBeenCalledWith('/api/events', { signal: controller.signal });
  });

  it('rejects with the status on a server error', async () => {
    stubFetch({ ok: false, status: 500 });

    await expect(fetchEvents('')).rejects.toThrow('API error: 500');
  });

  it('treats a 404 on delete as success', async () => {
    const fetchMock = stubFetch({ ok: false, status: 404 });

    await expect(deleteEvent(42)).resolves.toBeUndefined();
    expect(fetchMock).toHaveBeenCalledWith(
      '/api/events/42',
      expect.objectContaining({ method: 'DELETE' }),
    );
  });

  it('fetches the run log and fills missing lists', async () => {
    const fetchMock = stubFetch({ ok: true, status: 200, json: async () => ({ latest: null }) });

    await expect(fetchRuns()).resolves.toEqual({ latest: [], runs: [] });
    expect(fetchMock.mock.calls[0][0]).toBe('/api/runs?limit=24');
  });

  it('rejects when the run log is unavailable', async () => {
    stubFetch({ ok: false, status: 503 });

    await expect(fetchRuns()).rejects.toThrow(/API error: 503/);
  });
});
