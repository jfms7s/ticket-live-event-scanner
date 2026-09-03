import { useState } from 'react';
import { deleteEvent as apiDeleteEvent, retriggerEvent, setPurchased } from '../api/client';
import type { Event } from '../types';
import { formatDate } from '../utils/format';
import { getLatestNotificationStatus } from '../utils/notifications';
import { StatusBadge } from './StatusBadge';

const STATUS_MESSAGE_MS = 3000;

interface EventRowProps {
  event: Event;
  onPurchasedChange: (eventId: number, purchased: boolean) => void;
  onDeleted: (eventId: number) => void;
  onError: (message: string) => void;
}

export function EventRow({ event, onPurchasedChange, onDeleted, onError }: EventRowProps) {
  const [imageFailed, setImageFailed] = useState(false);
  const [purchasedPending, setPurchasedPending] = useState(false);
  const [deletePending, setDeletePending] = useState(false);
  const [retrigger, setRetrigger] = useState<{ busy: boolean; message: string; kind: 'loading' | 'success' | 'error' | '' }>({
    busy: false,
    message: '',
    kind: '',
  });

  const { status: notifStatus, notif } = getLatestNotificationStatus(event.notifications);

  async function handleRetrigger() {
    setRetrigger({ busy: true, message: 'Retriggering...', kind: 'loading' });

    try {
      await retriggerEvent(event.id);
      setRetrigger({ busy: true, message: 'Retrigger sent ✓', kind: 'success' });
    } catch (err) {
      setRetrigger({ busy: true, message: (err as Error).message, kind: 'error' });
    }

    setTimeout(() => {
      setRetrigger({ busy: false, message: '', kind: '' });
    }, STATUS_MESSAGE_MS);
  }

  async function handleTogglePurchased(e: React.ChangeEvent<HTMLInputElement>) {
    const purchased = e.target.checked;
    setPurchasedPending(true);

    try {
      await setPurchased(event.id, purchased);
      onPurchasedChange(event.id, purchased);
    } catch (err) {
      onError(`Failed to update purchased status: ${(err as Error).message}`);
    }

    setPurchasedPending(false);
  }

  async function handleDelete() {
    if (!window.confirm(`Remove "${event.title || 'this event'}" from the database? This cannot be undone.`)) {
      return;
    }

    setDeletePending(true);
    try {
      await apiDeleteEvent(event.id);
      onDeleted(event.id);
    } catch (err) {
      onError(`Failed to remove event: ${(err as Error).message}`);
      setDeletePending(false);
    }
  }

  return (
    <tr>
      <td className="event-thumb">
        {event.image_url && !imageFailed && (
          <img src={event.image_url} alt="" loading="lazy" onError={() => setImageFailed(true)} />
        )}
      </td>

      <td className="event-title">
        {event.url ? (
          <a href={event.url} target="_blank" rel="noopener noreferrer">
            {event.title || 'N/A'}
          </a>
        ) : (
          event.title || 'N/A'
        )}
      </td>

      <td className="event-venue">{event.venue || 'N/A'}</td>
      <td>{event.category || 'N/A'}</td>
      <td>{formatDate(event.event_date)}</td>

      <td>
        <StatusBadge status={event.status || 'unknown'} type={event.status || 'unknown'} />
      </td>

      <td>
        {notifStatus === 'none' ? (
          <StatusBadge status="None yet" type="none" />
        ) : (
          <div>
            <StatusBadge status={notifStatus} type={notifStatus} />
            {notifStatus === 'failed' && notif?.error && (
              <div className="notification-error-detail" title={notif.error}>
                Error: {notif.error.substring(0, 50)}
                {notif.error.length > 50 ? '...' : ''}
              </div>
            )}
          </div>
        )}
      </td>

      <td className="event-purchased">
        <input
          type="checkbox"
          checked={event.purchased}
          disabled={purchasedPending}
          onChange={handleTogglePurchased}
        />
      </td>

      <td>
        <button className="btn-retrigger" disabled={retrigger.busy} onClick={handleRetrigger}>
          Retrigger
        </button>
        <button className="btn-delete" disabled={deletePending} onClick={handleDelete}>
          Remove
        </button>
        <div className={`retrigger-status ${retrigger.kind}`}>{retrigger.message}</div>
      </td>
    </tr>
  );
}
