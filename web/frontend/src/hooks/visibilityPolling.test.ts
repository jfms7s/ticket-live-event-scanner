import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { startVisibilityAwarePolling, type VisibilityDocument } from './visibilityPolling';

function fakeDocument(state: DocumentVisibilityState) {
  const listeners = new Set<() => void>();
  const doc: VisibilityDocument & { set(s: DocumentVisibilityState): void; listeners: Set<() => void> } = {
    visibilityState: state,
    addEventListener: (_type, l) => listeners.add(l),
    removeEventListener: (_type, l) => listeners.delete(l),
    set(s) {
      doc.visibilityState = s;
      listeners.forEach((l) => l());
    },
    listeners,
  };
  return doc;
}

describe('startVisibilityAwarePolling', () => {
  beforeEach(() => vi.useFakeTimers());
  afterEach(() => vi.useRealTimers());

  it('polls while visible', () => {
    const refresh = vi.fn();
    startVisibilityAwarePolling(refresh, 1000, fakeDocument('visible'));
    vi.advanceTimersByTime(3000);
    expect(refresh).toHaveBeenCalledTimes(3);
  });

  it('skips ticks while hidden and refreshes once on becoming visible', () => {
    const refresh = vi.fn();
    const doc = fakeDocument('hidden');
    startVisibilityAwarePolling(refresh, 1000, doc);
    vi.advanceTimersByTime(5000);
    expect(refresh).not.toHaveBeenCalled();
    doc.set('visible');
    expect(refresh).toHaveBeenCalledTimes(1);
  });

  it('stops the interval and the listener', () => {
    const refresh = vi.fn();
    const doc = fakeDocument('visible');
    const stop = startVisibilityAwarePolling(refresh, 1000, doc);
    stop();
    vi.advanceTimersByTime(3000);
    expect(refresh).not.toHaveBeenCalled();
    expect(doc.listeners.size).toBe(0);
  });
});
