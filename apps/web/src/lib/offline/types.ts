/**
 * Offline action queue — shared types (Mobile Attendant Phase 6a, PRD §9.7 /
 * §14). Every payload figure that is money or litres stays an exact decimal
 * STRING end-to-end: the queue stores the SDK request shapes verbatim and
 * never reserializes them through floats.
 */

/** The PRD's offline-capable attendant actions (§14.1) that have endpoints today. */
export type OfflineActionType =
  | 'check_in'
  | 'check_out'
  | 'confirm_assignment'
  | 'opening_reading'
  | 'closing_reading'
  | 'collection';

/**
 * Per-action lifecycle (PRD §9.7):
 *  - pending  — captured on this phone, waiting for connectivity;
 *  - syncing  — replay in flight right now;
 *  - synced   — applied on the server (possibly "already applied" via the
 *               idempotent / duplicate-verified paths);
 *  - failed   — the server rejected it for a reason the attendant must fix
 *               (e.g. 422 opening_below_expected) — kept, re-editable,
 *               retryable, discardable only by explicit user action;
 *  - conflict — the server holds a DIFFERENT record than this submission —
 *               kept with the local payload visible, needs supervisor
 *               attention, never silently dropped (PRD §14.3).
 */
export type OfflineSyncStatus = 'pending' | 'syncing' | 'synced' | 'failed' | 'conflict';

export interface CheckInPayload {
  device_info?: Record<string, unknown>;
}

export interface ConfirmAssignmentPayload {
  assignment_id: string;
  /** Display context for the sync sheet (what was confirmed). */
  pump_number?: number;
  nozzle_number?: number;
}

export interface ReadingPayload {
  nozzle_id: string;
  /** Exact decimal string — preserved verbatim through the queue. */
  reading: string;
  /** Display context for the sync sheet. */
  pump_number?: number;
  nozzle_number?: number;
}

export interface CollectionPayload {
  /** Exact decimal strings — preserved verbatim through the queue. */
  cash_amount: string;
  mobile_money_amount: string;
  card_amount: string;
  credit_amount: string;
  notes?: string;
}

interface QueuedActionBase {
  /** Client-generated idempotent identity for this capture (uuid). */
  local_action_id: string;
  /** Monotonic enqueue order — replay is strictly in seq order per shift. */
  seq: number;
  shift_id: string;
  /** Short human description for the sync details sheet. */
  label: string;
  /** ISO timestamp from the device clock at capture time. */
  created_at_local: string;
  retry_count: number;
  sync_status: OfflineSyncStatus;
  /** Plain-language error for failed/conflict rows in the sync sheet. */
  error_message?: string;
  /** On conflict: the server's value, kept alongside the local payload. */
  server_value?: string;
}

export type QueuedAction = QueuedActionBase &
  (
    | { action_type: 'check_in'; payload: CheckInPayload }
    | { action_type: 'check_out'; payload: Record<string, never> }
    | { action_type: 'confirm_assignment'; payload: ConfirmAssignmentPayload }
    | { action_type: 'opening_reading'; payload: ReadingPayload }
    | { action_type: 'closing_reading'; payload: ReadingPayload }
    | { action_type: 'collection'; payload: CollectionPayload }
  );

/** What the caller provides at capture time; identity/order/status are filled in. */
export type EnqueueInput = Pick<QueuedAction, 'action_type' | 'shift_id' | 'payload' | 'label'> & {
  action_type: OfflineActionType;
};
