import { describe, expect, it } from 'vitest';
import { createLatestOnly, isAbortError } from './latestOnly';

describe('createLatestOnly', () => {
  it('aborts the earlier of two overlapping requests and keeps only the later one current', () => {
    const latest = createLatestOnly();
    const first = latest.begin();
    const second = latest.begin();

    expect(first.signal.aborted).toBe(true);
    expect(first.isCurrent()).toBe(false);
    expect(second.signal.aborted).toBe(false);
    expect(second.isCurrent()).toBe(true);
  });

  it('keeps a lone request current and unaborted', () => {
    const only = createLatestOnly().begin();
    expect(only.signal.aborted).toBe(false);
    expect(only.isCurrent()).toBe(true);
  });

  it('keeps separate instances independent', () => {
    const a = createLatestOnly().begin();
    createLatestOnly().begin();
    expect(a.isCurrent()).toBe(true);
  });
});

describe('isAbortError', () => {
  it('recognises an AbortError and nothing else', () => {
    expect(isAbortError(new DOMException('aborted', 'AbortError'))).toBe(true);
    expect(isAbortError(new Error('boom'))).toBe(false);
    expect(isAbortError('AbortError')).toBe(false);
  });
});
