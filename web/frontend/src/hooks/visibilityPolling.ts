/** The parts of `document` the poller needs, so tests can pass a fake. */
export interface VisibilityDocument {
  visibilityState: DocumentVisibilityState;
  addEventListener(type: 'visibilitychange', listener: () => void): void;
  removeEventListener(type: 'visibilitychange', listener: () => void): void;
}

/**
 * Calls `refresh` every `intervalMs` while the tab is visible, skips ticks
 * while it is hidden, and refreshes once as soon as it becomes visible again.
 * Returns a function that stops both the interval and the listener.
 */
export function startVisibilityAwarePolling(
  refresh: () => void,
  intervalMs: number,
  doc: VisibilityDocument = document,
): () => void {
  const interval = setInterval(() => {
    if (doc.visibilityState === 'hidden') return;
    refresh();
  }, intervalMs);
  const onVisibilityChange = () => {
    if (doc.visibilityState === 'visible') refresh();
  };
  doc.addEventListener('visibilitychange', onVisibilityChange);
  return () => {
    clearInterval(interval);
    doc.removeEventListener('visibilitychange', onVisibilityChange);
  };
}
