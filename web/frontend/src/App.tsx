import { useEffect, useMemo, useState } from 'react';
import { ErrorBanner } from './components/ErrorBanner';
import { TABLE_COLUMN_COUNT } from './components/columns';
import { EventsTable } from './components/EventsTable';
import { FilterBar } from './components/FilterBar';
import { Header } from './components/Header';
import { useEvents } from './hooks/useEvents';
import type { BoughtFilter, EventStatusFilter, NotificationStatusFilter, SortKey, SortState } from './types';
import { UNKNOWN_HUB, hubOptions, matchesHub } from './utils/hub';
import { getLatestNotificationStatus } from './utils/notifications';
import { getSortValue } from './utils/sort';

export function App() {
  const [eventStatusFilter, setEventStatusFilter] = useState<EventStatusFilter>('');
  const [notificationStatusFilter, setNotificationStatusFilter] = useState<NotificationStatusFilter>('');
  const [venueFilter, setVenueFilter] = useState('');
  const [hubFilter, setHubFilter] = useState('');
  const [boughtFilter, setBoughtFilter] = useState<BoughtFilter>('');
  const [sortState, setSortState] = useState<SortState>({ key: null, direction: 'asc' });
  const [actionError, setActionError] = useState<string | null>(null);

  const {
    events,
    setEvents,
    error: fetchError,
    setError: setFetchError,
    loading,
    refresh,
  } = useEvents(eventStatusFilter);

  const venues = useMemo(
    () => Array.from(new Set(events.map((event) => event.venue).filter((v): v is string => Boolean(v)))).sort((a, b) => a.localeCompare(b)),
    [events]
  );

  const hubs = useMemo(() => hubOptions(events), [events]);
  const hasUnknownHub = useMemo(() => events.some((event) => !event.hub_slug), [events]);

  useEffect(() => {
    if (hubFilter && hubFilter !== UNKNOWN_HUB && !hubs.some((hub) => hub.value === hubFilter)) {
      setHubFilter('');
    }
  }, [hubs, hubFilter]);

  useEffect(() => {
    if (venueFilter && !venues.includes(venueFilter)) {
      setVenueFilter('');
    }
  }, [venues, venueFilter]);

  const visibleEvents = useMemo(() => {
    const filtered = events.filter((event) => {
      if (notificationStatusFilter) {
        const { status } = getLatestNotificationStatus(event.notifications);
        if (status !== notificationStatusFilter) return false;
      }
      if (venueFilter && event.venue !== venueFilter) return false;
      if (!matchesHub(event, hubFilter)) return false;
      if (boughtFilter === 'yes' && !event.purchased) return false;
      if (boughtFilter === 'no' && event.purchased) return false;
      return true;
    });

    if (sortState.key) {
      const { key, direction } = sortState;
      filtered.sort((a, b) => {
        const valueA = getSortValue(a, key);
        const valueB = getSortValue(b, key);
        if (valueA < valueB) return direction === 'asc' ? -1 : 1;
        if (valueA > valueB) return direction === 'asc' ? 1 : -1;
        return 0;
      });
    }

    return filtered;
  }, [events, notificationStatusFilter, venueFilter, hubFilter, boughtFilter, sortState]);

  function handleSort(key: SortKey) {
    setSortState((prev) =>
      prev.key === key ? { key, direction: prev.direction === 'asc' ? 'desc' : 'asc' } : { key, direction: 'asc' }
    );
  }

  function handlePurchasedChange(eventId: number, purchased: boolean) {
    // Ticking records a new invite as pending server-side; mirror that until
    // the next poll brings the result. Unticking leaves the invite as it is.
    setEvents((prev) =>
      prev.map((event) =>
        event.id !== eventId
          ? event
          : purchased
            ? { ...event, purchased, invite_status: 'pending', invite_error: null }
            : { ...event, purchased }
      )
    );
  }

  function handleDeleted(eventId: number) {
    setEvents((prev) => prev.filter((event) => event.id !== eventId));
  }

  const bannerMessage = actionError || fetchError;

  return (
    <div className="container">
      <Header />

      <ErrorBanner
        message={bannerMessage}
        onDismiss={() => {
          setActionError(null);
          setFetchError(null);
        }}
      />

      <FilterBar
        eventStatusFilter={eventStatusFilter}
        onEventStatusChange={setEventStatusFilter}
        notificationStatusFilter={notificationStatusFilter}
        onNotificationStatusChange={setNotificationStatusFilter}
        venueFilter={venueFilter}
        onVenueChange={setVenueFilter}
        venues={venues}
        hubFilter={hubFilter}
        onHubChange={setHubFilter}
        hubs={hubs}
        hasUnknownHub={hasUnknownHub}
        boughtFilter={boughtFilter}
        onBoughtChange={setBoughtFilter}
        onRefresh={refresh}
      />

      {loading ? (
        <div className="events-container">
          <table className="events-table">
            <tbody>
              <tr className="loading-row">
                <td colSpan={TABLE_COLUMN_COUNT}>
                  <span className="loading-state">
                    <span className="spinner" />
                    Loading events...
                  </span>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      ) : (
        <EventsTable
          events={visibleEvents}
          sortState={sortState}
          onSort={handleSort}
          onPurchasedChange={handlePurchasedChange}
          onDeleted={handleDeleted}
          onError={setActionError}
        />
      )}

      <footer>
        <p>Auto-refreshing every 30 seconds</p>
      </footer>
    </div>
  );
}
