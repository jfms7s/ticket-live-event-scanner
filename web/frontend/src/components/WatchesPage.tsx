import { useCallback, useEffect, useRef, useState } from 'react';
import type { FormEvent } from 'react';
import { addWatch, fetchRuns, fetchWatches, updateWatch } from '../api/client';
import type { HubStatus, Watch } from '../types';
import { createLatestOnly, isAbortError } from '../utils/latestOnly';
import { watchName, watchRunSummary, watchUrl } from '../utils/watches';
import { ErrorBanner } from './ErrorBanner';

/** The hub pages the scraper tracks: add one, disable one, see its last run. */
export function WatchesPage() {
  const [watches, setWatches] = useState<Watch[] | null>(null);
  const [latest, setLatest] = useState<HubStatus[]>([]);
  const [error, setError] = useState<string | null>(null);
  const [url, setUrl] = useState('');
  const [label, setLabel] = useState('');
  const [busy, setBusy] = useState(false);
  const [toggling, setToggling] = useState<ReadonlySet<string>>(new Set());
  const latestOnly = useRef(createLatestOnly());

  const refresh = useCallback(async () => {
    const request = latestOnly.current.begin();
    try {
      const [list, runs] = await Promise.all([
        fetchWatches(request.signal),
        // The run log is a nicety: the list still works without it.
        fetchRuns(request.signal).catch((err) => {
          if (isAbortError(err)) throw err;
          return null;
        }),
      ]);
      if (!request.isCurrent()) return;
      setWatches(list);
      if (runs) setLatest(runs.latest);
      setError(null);
    } catch (err) {
      if (!request.isCurrent() || isAbortError(err)) return;
      setError(`Failed to fetch watches: ${(err as Error).message}`);
    }
  }, []);

  useEffect(() => {
    refresh();
  }, [refresh]);

  async function handleAdd(e: FormEvent) {
    e.preventDefault();
    if (!url.trim()) return;
    setBusy(true);
    try {
      await addWatch(url.trim(), label.trim());
      setUrl('');
      setLabel('');
      setError(null);
      await refresh();
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setBusy(false);
    }
  }

  async function handleToggle(watch: Watch) {
    if (toggling.has(watch.slug)) return;
    // An in-flight refresh began before this change; its list must not
    // overwrite it.
    latestOnly.current.begin();
    setToggling((prev) => new Set(prev).add(watch.slug));
    try {
      const updated = await updateWatch(watch.slug, { enabled: !watch.enabled });
      setWatches((prev) => prev?.map((w) => (w.slug === updated.slug ? updated : w)) ?? prev);
      setError(null);
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setToggling((prev) => {
        const next = new Set(prev);
        next.delete(watch.slug);
        return next;
      });
    }
  }

  return (
    <section aria-label="Watches">
      <ErrorBanner message={error} onDismiss={() => setError(null)} />

      <form className="controls watch-form" onSubmit={handleAdd}>
        <div className="filter-group watch-url">
          <label htmlFor="watchUrl">Hub page URL:</label>
          <input
            id="watchUrl"
            type="text"
            placeholder="https://www.ticketline.pt/evento/…-98164"
            value={url}
            onChange={(e) => setUrl(e.target.value)}
          />
        </div>
        <div className="filter-group">
          <label htmlFor="watchLabel">Label (optional):</label>
          <input id="watchLabel" type="text" maxLength={80} value={label} onChange={(e) => setLabel(e.target.value)} />
        </div>
        <button className="btn-primary" type="submit" disabled={busy || !url.trim()}>
          Add watch
        </button>
      </form>

      <div className="events-container">
        <table className="events-table">
          <thead>
            <tr>
              <th>Hub</th>
              <th>Last run</th>
              <th>Enabled</th>
            </tr>
          </thead>
          <tbody>
            {watches === null && (
              <tr className="loading-row">
                <td colSpan={3}>Loading watches...</td>
              </tr>
            )}
            {watches?.length === 0 && (
              <tr className="loading-row">
                <td colSpan={3}>No watches yet. Add a hub page above.</td>
              </tr>
            )}
            {watches?.map((watch) => (
              <tr key={watch.slug}>
                <td>
                  <a href={watchUrl(watch)} target="_blank" rel="noopener noreferrer">
                    {watchName(watch)}
                  </a>
                </td>
                <td>{watchRunSummary(watch, latest)}</td>
                <td>
                  <input
                    type="checkbox"
                    checked={watch.enabled}
                    disabled={toggling.has(watch.slug)}
                    aria-label={`Scrape ${watchName(watch)}`}
                    onChange={() => handleToggle(watch)}
                  />
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      <footer>
        <p>Changes apply from the scraper&apos;s next run. A disabled hub keeps its events.</p>
      </footer>
    </section>
  );
}
