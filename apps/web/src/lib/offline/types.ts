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
  | 'collection'
  | 'report_issue';

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

/**
 * Machine codes for CLIENT-generated queue messages (Phase 6b i18n): the
 * queue stores codes + params, never display prose, so the sync sheet can
 * render them in the attendant's language at display time. Raw SERVER error
 * prose has no code and is kept verbatim in `error_message` as the fallback.
 */
export type QueueMessageCode =
  | 'opening_below_expected'
  | 'assignment_changed'
  | 'reading_conflict'
  | 'collection_conflict'
  | 'verify_unavailable'
  | 'no_active_shift'
  | 'issue_invalid';

/** Parameters for coded queue messages (JSON-serializable, stored verbatim). */
export interface QueueMessageParams {
  reading_type?: 'opening' | 'closing';
  /** The server's differing figure for conflict messages (decimal string). */
  server_value?: string;
}

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

/**
 * An attendant self-service issue report (PRD §6.12). The station is derived
 * SERVER-SIDE from the actor's current shift, so it is never carried here.
 * `dedupe_key` is the per-submission UUID generated once at capture time and
 * reused on every replay so the server returns the original incident (200)
 * rather than creating a duplicate (201) — the offline idempotency contract.
 */
export interface IssuePayload {
  type: 'pump' | 'nozzle' | 'meter' | 'payment' | 'safety' | 'other';
  description: string;
  severity?: string;
  /** Per-submission idempotency key (uuid), reused across retries. */
  dedupe_key: string;
}

interface QueuedActionBase {
  /** Client-generated idempotent identity for this capture (uuid). */
  local_action_id: string;
  /** Monotonic enqueue order — replay is strictly in seq order per shift. */
  seq: number;
  shift_id: string;
  /**
   * Legacy (Phase 6a) stored display prose. New records do NOT set it — the
   * sync sheet derives a translated label from action_type + payload at
   * display time. Kept optional so records persisted before Phase 6b still
   * render.
   */
  label?: string;
  /** ISO timestamp from the device clock at capture time. */
  created_at_local: string;
  retry_count: number;
  sync_status: OfflineSyncStatus;
  /**
   * Raw error prose for failed/conflict rows. For client-classified outcomes
   * `error_code` is ALSO set and the sheet renders the translated message;
   * for raw server errors this prose is shown verbatim (untranslated).
   */
  error_message?: string;
  /** Machine code for client-generated messages — translated at display time. */
  error_code?: QueueMessageCode;
  /** Parameters for the coded message (reading type, server figure, …). */
  error_params?: QueueMessageParams;
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
    | { action_type: 'report_issue'; payload: IssuePayload }
  );

/** What the caller provides at capture time; identity/order/status are filled in. */
export type EnqueueInput = Pick<QueuedAction, 'action_type' | 'shift_id' | 'payload'> & {
  action_type: OfflineActionType;
  /** Legacy display prose — new callers omit it (labels derive from payload). */
  label?: string;
};
