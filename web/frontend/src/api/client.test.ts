import { afterEach, describe, expect, it, vi } from 'vitest';
import { ApiError, deleteEvent, fetchEvents } from './client';

function stubWindow(apiBaseUrl?: string) {
  vi.stubGlobal('window', apiBaseUrl === undefined ? {} : { API_BASE_URL: apiBaseUrl });
}

function stubFetch(response: Partial<Response>) {
  const fetchMock = vi.fn().mockResolvedValue(response);
  vi.stubGlobal('fetch', fetchMock);
  return fetchMock;
}

afterEach(() => {
  vi.unstubAllGlobals();
});

describe('api client', () => {
  it('rejects with a configuration error and does not fetch when API_BASE_URL is missing', async () => {
    stubWindow();
    const fetchMock = stubFetch({ ok: true, status: 200, json: async () => [] });

    const result = fetchEvents('');
    await expect(result).rejects.toBeInstanceOf(ApiError);
    await expect(result).rejects.toThrow(/Configuration error: API_BASE_URL is not set/);
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it('rejects with a configuration error when API_BASE_URL is empty', async () => {
    stubWindow('');
    const fetchMock = stubFetch({ ok: true, status: 200, json: async () => [] });

    await expect(fetchEvents('')).rejects.toThrow(/Configuration error/);
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it('strips one trailing slash and adds the status filter', async () => {
    stubWindow('https://x.example/');
    const fetchMock = stubFetch({ ok: true, status: 200, json: async () => [] });

    await expect(fetchEvents('active')).resolves.toEqual([]);
    expect(fetchMock).toHaveBeenCalledWith('https://x.example/api/events?status=active');
  });

  it('rejects with the status on a server error', async () => {
    stubWindow('https://x.example');
    stubFetch({ ok: false, status: 500 });

    await expect(fetchEvents('')).rejects.toThrow('API error: 500');
  });

  it('treats a 404 on delete as success', async () => {
    stubWindow('https://x.example');
    const fetchMock = stubFetch({ ok: false, status: 404 });

    await expect(deleteEvent(42)).resolves.toBeUndefined();
    expect(fetchMock).toHaveBeenCalledWith(
      'https://x.example/api/events/42',
      expect.objectContaining({ method: 'DELETE' }),
    );
  });
});
