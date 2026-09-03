import type { BoughtFilter, EventStatusFilter, NotificationStatusFilter } from '../types';

interface FilterBarProps {
  eventStatusFilter: EventStatusFilter;
  onEventStatusChange: (value: EventStatusFilter) => void;
  notificationStatusFilter: NotificationStatusFilter;
  onNotificationStatusChange: (value: NotificationStatusFilter) => void;
  venueFilter: string;
  onVenueChange: (value: string) => void;
  venues: string[];
  boughtFilter: BoughtFilter;
  onBoughtChange: (value: BoughtFilter) => void;
  onRefresh: () => void;
}

export function FilterBar({
  eventStatusFilter,
  onEventStatusChange,
  notificationStatusFilter,
  onNotificationStatusChange,
  venueFilter,
  onVenueChange,
  venues,
  boughtFilter,
  onBoughtChange,
  onRefresh,
}: FilterBarProps) {
  return (
    <div className="controls">
      <div className="filter-group">
        <label htmlFor="eventStatusFilter">Event Status:</label>
        <select
          id="eventStatusFilter"
          value={eventStatusFilter}
          onChange={(e) => onEventStatusChange(e.target.value as EventStatusFilter)}
        >
          <option value="">All</option>
          <option value="active">Active</option>
          <option value="finished">Finished</option>
        </select>
      </div>

      <div className="filter-group">
        <label htmlFor="notificationStatusFilter">Notification Status:</label>
        <select
          id="notificationStatusFilter"
          value={notificationStatusFilter}
          onChange={(e) => onNotificationStatusChange(e.target.value as NotificationStatusFilter)}
        >
          <option value="">All</option>
          <option value="pending">Pending</option>
          <option value="sent">Sent</option>
          <option value="failed">Failed</option>
          <option value="none">None yet</option>
        </select>
      </div>

      <div className="filter-group">
        <label htmlFor="venueFilter">Venue:</label>
        <select id="venueFilter" value={venueFilter} onChange={(e) => onVenueChange(e.target.value)}>
          <option value="">All</option>
          {venues.map((venue) => (
            <option key={venue} value={venue}>
              {venue}
            </option>
          ))}
        </select>
      </div>

      <div className="filter-group">
        <label htmlFor="boughtFilter">Bought:</label>
        <select
          id="boughtFilter"
          value={boughtFilter}
          onChange={(e) => onBoughtChange(e.target.value as BoughtFilter)}
        >
          <option value="">All</option>
          <option value="yes">Yes</option>
          <option value="no">No</option>
        </select>
      </div>

      <button className="btn-primary" onClick={onRefresh}>
        Refresh Now
      </button>
    </div>
  );
}
