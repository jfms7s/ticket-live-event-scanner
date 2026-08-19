import { useCallback, useEffect, useState } from 'react';
import { fetchEvents } from '../api/client';
import type { Event, EventStatusFilter } from '../types';

const AUTO_REFRESH_MS = 30 * 1000;

export function useEvents(eventStatusFilter: EventStatusFilter) {
  const [events, setEvents] = useState<Event[]>([]);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);

  const refresh = useCallback(async () => {
    try {
      const data = await fetchEvents(eventStatusFilter);
      setEvents(data);
      setError(null);
    } catch (err) {
      setError(`Failed to fetch events: ${(err as Error).message}`);
    } finally {
      setLoading(false);
    }
  }, [eventStatusFilter]);

  useEffect(() => {
    refresh();
    const interval = setInterval(refresh, AUTO_REFRESH_MS);
    return () => clearInterval(interval);
  }, [refresh]);

  return { events, setEvents, error, setError, loading, refresh };
}
