import { useCallback, useEffect, useRef, useState } from 'react';
import { fetchEvents } from '../api/client';
import type { Event, EventStatusFilter } from '../types';
import { createLatestOnly, isAbortError } from '../utils/latestOnly';
import { startVisibilityAwarePolling } from './visibilityPolling';

const AUTO_REFRESH_MS = 30 * 1000;

export function useEvents(eventStatusFilter: EventStatusFilter) {
  const [events, setEvents] = useState<Event[]>([]);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);

  // Only the newest request may update state: a slower, older response (an
  // earlier filter, or a poll racing a filter change) is aborted and ignored.
  const latest = useRef(createLatestOnly());

  const refresh = useCallback(async () => {
    const request = latest.current.begin();
    try {
      const data = await fetchEvents(eventStatusFilter, request.signal);
      if (!request.isCurrent()) return;
      setEvents(data);
      setError(null);
    } catch (err) {
      if (!request.isCurrent() || isAbortError(err)) return;
      setError(`Failed to fetch events: ${(err as Error).message}`);
    } finally {
      if (request.isCurrent()) setLoading(false);
    }
  }, [eventStatusFilter]);

  useEffect(() => {
    refresh();
    return startVisibilityAwarePolling(refresh, AUTO_REFRESH_MS);
  }, [refresh]);

  return { events, setEvents, error, setError, loading, refresh };
}
