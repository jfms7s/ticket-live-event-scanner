import { useCallback, useEffect, useRef, useState } from 'react';
import { fetchRuns } from '../api/client';
import type { HubStatus } from '../types';
import { createLatestOnly, isAbortError } from '../utils/latestOnly';
import { startVisibilityAwarePolling } from './visibilityPolling';

const AUTO_REFRESH_MS = 30 * 1000;

/** Polls GET /api/runs with the events, for the header's run status. */
export function useRuns() {
  const [latest, setLatest] = useState<HubStatus[] | null>(null);
  const [error, setError] = useState(false);
  const latestOnly = useRef(createLatestOnly());

  const refresh = useCallback(async () => {
    const request = latestOnly.current.begin();
    try {
      const data = await fetchRuns(request.signal);
      if (!request.isCurrent()) return;
      setLatest(data.latest);
      setError(false);
    } catch (err) {
      if (!request.isCurrent() || isAbortError(err)) return;
      setError(true);
    }
  }, []);

  useEffect(() => {
    refresh();
    return startVisibilityAwarePolling(refresh, AUTO_REFRESH_MS);
  }, [refresh]);

  return { latest, error };
}
