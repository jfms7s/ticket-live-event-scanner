import { useEffect, useState } from 'react';
import { useRuns } from '../hooks/useRuns';
import { runHealth } from '../utils/runHealth';

/** "Last run 12 min ago · 2 hubs · 0 new · 0 errors", coloured by health. */
export function RunStatus() {
  const { latest, error } = useRuns();
  // Re-render each minute so "12 min ago" moves between polls.
  const [now, setNow] = useState(() => new Date());
  useEffect(() => {
    const id = setInterval(() => setNow(new Date()), 60 * 1000);
    return () => clearInterval(id);
  }, []);

  if (error && latest === null) {
    return <p className="run-status run-status-unknown">Run status unavailable</p>;
  }
  if (latest === null) {
    return null;
  }
  const health = runHealth(latest, now);
  return (
    <p className={`run-status run-status-${health.level}`} title={health.detail || undefined} role="status">
      <span className="run-status-dot" aria-hidden="true" />
      {health.text}
    </p>
  );
}
