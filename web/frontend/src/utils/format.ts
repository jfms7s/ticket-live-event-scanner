// The scraper stores a timezone-less "YYYY-MM-DDTHH:MM" (event start time,
// as published on ticketline.pt with no UTC offset attached), and some DB
// drivers round-trip that into RFC3339 with a trailing "Z" that does NOT
// represent an actual UTC conversion — just re-serialization of the same
// digits. Parsing that through `new Date(dateStr)` would silently shift the
// displayed hour by the browser's UTC offset for no reason, so the date and
// optional "HH:MM" time are pulled straight out of the string instead.
export function formatDate(dateStr: string | null | undefined): string {
  if (!dateStr) return 'N/A';

  const match = /^(\d{4}-\d{2}-\d{2})(?:T(\d{2}):(\d{2}))?/.exec(dateStr);
  if (!match) return 'N/A';
  const [, datePart, hh, mm] = match;

  const date = new Date(`${datePart}T00:00:00Z`);
  if (isNaN(date.getTime())) return 'N/A';
  const formattedDate = date.toLocaleDateString('en-US', {
    year: 'numeric',
    month: 'short',
    day: 'numeric',
    timeZone: 'UTC',
  });

  // No time component, or a "00:00" that's typically just the DB's midnight
  // placeholder for a date that was never given a time — either way, there's
  // no real time of day to show.
  if (!hh || (hh === '00' && mm === '00')) {
    return formattedDate;
  }

  return `${formattedDate}, ${hh}:${mm}`;
}
