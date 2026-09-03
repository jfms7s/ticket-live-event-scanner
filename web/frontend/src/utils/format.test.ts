import { describe, expect, it } from 'vitest';
import { formatDate } from './format';

describe('formatDate', () => {
  it('returns "N/A" for empty string', () => {
    expect(formatDate('')).toBe('N/A');
  });

  it('returns "N/A" for null', () => {
    expect(formatDate(null)).toBe('N/A');
  });

  it('formats a valid date string', () => {
    expect(formatDate('2026-08-22')).toMatch(/Aug 22, 2026/);
  });

  it('formats an ISO datetime', () => {
    expect(formatDate('2026-08-17T10:00:00Z')).toMatch(/Aug 17, 2026/);
  });

  it('shows time for the scraper\'s "YYYY-MM-DDTHH:MM" format', () => {
    expect(formatDate('2026-08-22T11:30')).toBe('Aug 22, 2026, 11:30');
  });

  it('shows time for RFC3339 with seconds and offset', () => {
    expect(formatDate('2026-08-22T11:30:00Z')).toBe('Aug 22, 2026, 11:30');
  });

  it('does not shift the hour based on the browser timezone', () => {
    // A naive "no offset" datetime must display its literal digits, not the
    // browser-local interpretation `new Date()` would otherwise apply.
    expect(formatDate('2026-08-22T23:45')).toBe('Aug 22, 2026, 23:45');
  });

  it('hides a bare "00:00" time (date-only DB round-trip artifact)', () => {
    expect(formatDate('2026-08-29T00:00:00Z')).toBe('Aug 29, 2026');
  });

  it('date-only string has no time suffix', () => {
    expect(formatDate('2026-08-22')).toBe('Aug 22, 2026');
  });
});
