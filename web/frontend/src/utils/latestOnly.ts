export interface LatestOnlyRequest {
  signal: AbortSignal;
  isCurrent(): boolean;
}

/**
 * Lets only the most recent of overlapping async requests apply its result.
 * Each begin() aborts the previous request's signal; isCurrent() is true only
 * for the request started last.
 */
export function createLatestOnly(): { begin(): LatestOnlyRequest } {
  let current: AbortController | null = null;
  return {
    begin() {
      current?.abort();
      const controller = new AbortController();
      current = controller;
      return {
        signal: controller.signal,
        isCurrent: () => current === controller,
      };
    },
  };
}

export function isAbortError(err: unknown): boolean {
  return err instanceof Error && err.name === 'AbortError';
}
