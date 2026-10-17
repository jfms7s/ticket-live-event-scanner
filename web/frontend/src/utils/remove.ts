// removeConfirmText is the confirm dialog for the Remove button. Remove
// hides the event (the API keeps its row so the scraper doesn't announce it
// again), so the text says that rather than promising a deletion.
export function removeConfirmText(title: string): string {
  const name = title ? `"${title}"` : 'this event';
  return `Remove ${name} from the dashboard? It will not be notified again, even while it is still listed. This cannot be undone.`;
}
