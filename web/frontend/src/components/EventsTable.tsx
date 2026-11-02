import type { Event, SortKey, SortState } from '../types';
import { COLUMNS, TABLE_COLUMN_COUNT } from './columns';
import { EventRow } from './EventRow';

interface EventsTableProps {
  events: Event[];
  sortState: SortState;
  onSort: (key: SortKey) => void;
  onPurchasedChange: (eventId: number, purchased: boolean) => void;
  onDeleted: (eventId: number) => void;
  onError: (message: string) => void;
}

export function EventsTable({ events, sortState, onSort, onPurchasedChange, onDeleted, onError }: EventsTableProps) {
  return (
    <div className="events-container">
      <table className="events-table">
        <thead>
          <tr>
            <th></th>
            {COLUMNS.map((col) => (
              <th key={col.key} className="sortable" onClick={() => onSort(col.key)}>
                {col.label}
                <span className="sort-indicator">
                  {sortState.key === col.key ? (sortState.direction === 'asc' ? '▲' : '▼') : ''}
                </span>
              </th>
            ))}
            <th>Action</th>
          </tr>
        </thead>
        <tbody>
          {events.length === 0 ? (
            <tr className="loading-row">
              <td colSpan={TABLE_COLUMN_COUNT}>No events found</td>
            </tr>
          ) : (
            events.map((event) => (
              <EventRow
                key={event.id}
                event={event}
                onPurchasedChange={onPurchasedChange}
                onDeleted={onDeleted}
                onError={onError}
              />
            ))
          )}
        </tbody>
      </table>
    </div>
  );
}
