/**
 * Hand-maintained until the Stage-10 OpenAPI generation lands. Keep
 * these in lockstep with the Go handlers; mismatches are not the
 * compiler's job to catch yet.
 */

export interface LoginRequest {
  tenant_slug: string;
  email: string;
  password: string;
  mfa_code?: string;
}

export interface LoginResponse {
  token?: string;
  expires_at?: string;
  mfa_required?: boolean;
}

export interface Me {
  user_id: string;
  tenant_id: string;
  session_id: string;
  mfa_satisfied: boolean;
  /** A second factor is active on the account. */
  mfa_enabled?: boolean;
  /** The actor's role makes MFA mandatory (admin/finance). */
  mfa_required?: boolean;
  /** Unused backup recovery codes remaining. */
  mfa_backup_codes_remaining?: number;
}

/** Returned once by enroll-confirm and backup-code regeneration. */
export interface MfaEnrollment {
  secret: string;
  otpauth_url: string;
}

export interface MfaBackupCodes {
  backup_codes: string[];
}

export interface PermissionItem {
  code: string;
  station_scoped: boolean;
}

export interface MePermissions {
  permissions: PermissionItem[];
  station_ids?: string[];
  tenant_wide: boolean;
  is_system_admin?: boolean;
}

export interface Company {
  id: string;
  tenant_id: string;
  name: string;
  legal_name?: string;
  registration_no?: string;
  tax_id?: string;
  currency: string;
  timezone: string;
  status: string;
}

/**
 * TenantBranding is the tenant-level company letterhead — the header rendered
 * on every downloadable document PDF. One per tenant. The logo is uploaded and
 * streamed separately (see uploadBrandingLogo / brandingLogoUrl); this carries
 * only a presence flag and a same-origin URL, never the bytes.
 */
export interface TenantBranding {
  display_name: string;
  legal_name: string;
  tax_id: string;
  registration_no: string;
  address_line1: string;
  address_line2: string;
  city: string;
  country: string;
  phone: string;
  email: string;
  website: string;
  footer_note: string;
  has_logo: boolean;
  logo_content_type?: string;
  logo_url?: string;
  updated_at?: string;
}

/** The writable text fields for updateBranding (logo managed separately). */
export interface TenantBrandingUpdate {
  display_name?: string;
  legal_name?: string;
  tax_id?: string;
  registration_no?: string;
  address_line1?: string;
  address_line2?: string;
  city?: string;
  country?: string;
  phone?: string;
  email?: string;
  website?: string;
  footer_note?: string;
}

/**
 * Attachment is one file attached to a business entity (an expense receipt, a
 * delivery photo, …). The bytes are fetched separately via the download_url
 * (cookie-bearing through the BFF); this carries only metadata.
 */
export interface Attachment {
  id: string;
  entity_type: string;
  entity_id: string;
  station_id?: string | null;
  filename: string;
  content_type: 'application/pdf' | 'image/png' | 'image/jpeg';
  size_bytes: number;
  checksum: string;
  uploaded_by?: string | null;
  created_at: string;
  download_url: string;
}

export interface Region {
  id: string;
  tenant_id: string;
  company_id: string;
  name: string;
  code?: string;
  status: string;
}

export interface Station {
  id: string;
  tenant_id: string;
  company_id: string;
  region_id?: string;
  name: string;
  code: string;
  address_line1?: string;
  address_line2?: string;
  city?: string;
  state?: string;
  country?: string;
  latitude?: number;
  longitude?: number;
  timezone: string;
  status: string;
}

export interface Product {
  id: string;
  tenant_id: string;
  code: string;
  name: string;
  category: string;
  unit: string;
  /** Money/rate fields are exact decimal STRINGS from the Go DTO (numeric -> text). */
  default_price: string;
  tax_rate: string;
  density_kg_m3?: string;
  loss_tolerance_percent: string;
  color: string;
  status: string;
}

export interface Supplier {
  id: string;
  tenant_id: string;
  code: string;
  name: string;
  contact_name?: string;
  contact_email?: string;
  contact_phone?: string;
  payment_terms_days: number;
  status: string;
  deactivated_at?: string;
  product_ids: string[];
}

export interface PurchaseOrderLine {
  id: string;
  tenant_id: string;
  purchase_order_id: string;
  product_id: string;
  /** Litre fields are exact decimal STRINGS (numeric(14,3) -> text). */
  ordered_litres: string;
  unit_price: string;
  received_litres: string;
}

export interface PurchaseOrder {
  id: string;
  tenant_id: string;
  station_id: string;
  supplier_id: string;
  expected_delivery_date?: string;
  status: string;
  raised_by: string;
  submitted_by?: string;
  submitted_at?: string;
  confirmed_by?: string;
  confirmed_at?: string;
  cancelled_by?: string;
  cancelled_at?: string;
  closed_by?: string;
  closed_at?: string;
  notes?: string;
  created_at: string;
  lines: PurchaseOrderLine[];
}

export interface Delivery {
  id: string;
  tenant_id: string;
  tank_id: string;
  supplier_ref?: string;
  supplier_id?: string;
  purchase_order_id?: string;
  po_line_id?: string;
  /** Ledger volume + PO variance are exact decimal STRINGS (numeric(14,3) -> text). */
  volume_litres: string;
  /** Advisory dip cross-checks are sensor floats, not ledger figures. */
  dip_before_litres?: number;
  dip_after_litres?: number;
  dip_variance_litres?: number;
  line_unit_price?: string;
  freight_amount: string;
  duty_amount: string;
  levies_amount: string;
  landed_cost_total?: string;
  landed_cost_per_litre?: string;
  match_status: string;
  quantity_variance_litres?: string;
  received_by: string;
  received_at: string;
  notes?: string;
}

export interface StockMovement {
  id: string;
  tenant_id: string;
  tank_id: string;
  movement_type: string;
  source_ref_type?: string;
  source_ref_id?: string;
  /** Ledger litres + running balance are exact decimal STRINGS (numeric(14,3) -> text). */
  litres: string;
  balance_after: string;
  supplier_id?: string;
  purchase_order_id?: string;
  landed_cost_total?: string;
  landed_cost_per_litre?: string;
  status: string;
  supersedes_id?: string;
  recorded_by: string;
  recorded_at: string;
  notes?: string;
}

export interface ProcurementDiscrepancy {
  id: string;
  tenant_id: string;
  supplier_invoice_id: string;
  purchase_order_id: string;
  delivery_id?: string;
  po_line_id?: string;
  type: string;
  severity: string;
  detail: string;
  variance_litres?: number;
  variance_amount?: string;
  status: string;
  raised_at: string;
  resolved_by?: string;
  resolved_at?: string;
}

export interface SupplierInvoiceLine {
  id: string;
  tenant_id: string;
  supplier_invoice_id: string;
  purchase_order_id: string;
  po_line_id: string;
  delivery_id?: string;
  product_id: string;
  invoiced_litres: number;
  unit_price: string;
  amount: string;
}

export interface SupplierInvoice {
  id: string;
  tenant_id: string;
  supplier_id: string;
  purchase_order_id: string;
  station_id: string;
  invoice_number: string;
  status: string;
  received_at: string;
  due_date?: string;
  total_amount: string;
  recorded_by: string;
  approved_by?: string;
  approved_at?: string;
  notes?: string;
  lines: SupplierInvoiceLine[];
  discrepancies: ProcurementDiscrepancy[];
}

export interface SupplierBalance {
  supplier_id: string;
  supplier_name: string;
  outstanding_amount: string;
  invoice_count: number;
}

export interface PriceTrendPoint {
  supplier_id: string;
  supplier_name: string;
  product_id: string;
  product_name: string;
  received_at: string;
  landed_cost_per_litre: string;
}

export interface ProcurementOverview {
  station: Station;
  open_purchase_orders: PurchaseOrder[];
  recent_receipts: Delivery[];
  supplier_balances: SupplierBalance[];
  price_trend: PriceTrendPoint[];
}

export interface Tank {
  id: string;
  tenant_id: string;
  station_id: string;
  product_id: string;
  name: string;
  code: string;
  /** Tank dimensions are exact decimal STRINGS (numeric(14,3) -> text). */
  capacity_litres: string;
  safe_min_litres: string;
  safe_max_litres: string;
  dead_stock_litres: string;
  has_water_sensor: boolean;
  has_temp_sensor: boolean;
  status: string;
  installation_date?: string;
  decommission_date?: string;
  /**
   * Latest dip-resolved volume as an exact decimal STRING (numeric(14,3) ->
   * text); present only on the station overview.
   */
  current_litres?: string;
  /** Metadata for current_litres, so the UI can flag a stale (prior-day) read. */
  current_dip_reading_type?: 'opening' | 'closing';
  current_dip_recorded_at?: string;
  current_dip_business_date?: string;
}

export interface TankBookBalance {
  tank_id: string;
  /** Exact decimal STRING (numeric(14,3) -> text). */
  book_balance: string;
}

export interface SetTankOpeningBalanceRequest {
  from_dip?: boolean;
  /** Exact decimal litres; required when from_dip is false/omitted. */
  litres?: string;
  notes?: string;
}

/** Coarse machine classification of why a stock adjustment is being made. */
export type StockAdjustmentClassification =
  | 'evaporation'
  | 'measurement_error'
  | 'theft'
  | 'spillage'
  | 'temperature'
  | 'data_entry'
  | 'other';

export type StockAdjustmentStatus = 'requested' | 'approved' | 'posted' | 'rejected';

/**
 * A stock adjustment is a controlled correction to a tank's book stock that
 * moves requested -> approved -> posted (or rejected). Posting appends a single
 * 'adjustment' movement to the tank ledger; balance_before / balance_after are
 * snapshotted at post time. Litres + balances are exact decimal STRINGS
 * (numeric(14,3) -> text); delta_litres is signed (+in / -out).
 */
export interface StockAdjustment {
  id: string;
  tank_id: string;
  delta_litres: string;
  reason: string;
  classification: StockAdjustmentClassification;
  status: StockAdjustmentStatus;
  balance_before?: string;
  balance_after?: string;
  movement_id?: string;
  requested_by: string;
  approved_by?: string;
  posted_by?: string;
  rejected_by?: string;
  decision_note?: string;
  requested_at: string;
  decided_at?: string;
  posted_at?: string;
}

export interface RequestStockAdjustmentRequest {
  tank_id: string;
  /** Signed exact-decimal litres delta (+in / -out); must be non-zero. */
  delta_litres: string;
  reason: string;
  classification: StockAdjustmentClassification;
}

export type OpeningStockRequestStatus = 'draft' | 'approved' | 'rejected';

/**
 * An opening-stock request is a controlled draft of a tank's opening balance
 * that moves draft -> approved(locked) / rejected (Feature 1.6). Approval posts
 * the genesis 'opening' movement to the tank ledger and LOCKS the request,
 * linking the movement and snapshotting the balance. litres + balance_after are
 * exact decimal STRINGS (numeric(14,3) -> text).
 */
export interface OpeningStockRequest {
  id: string;
  tank_id: string;
  litres: string;
  notes?: string;
  status: OpeningStockRequestStatus;
  movement_id?: string;
  balance_after?: string;
  requested_by: string;
  approved_by?: string;
  rejected_by?: string;
  decision_note?: string;
  requested_at: string;
  decided_at?: string;
}

export interface RequestOpeningStockRequest {
  tank_id: string;
  /** Non-negative exact-decimal litres; required when from_dip is false/omitted. */
  litres?: string;
  from_dip?: boolean;
  notes?: string;
}

export type SetupStepStatus = 'pending' | 'completed' | 'skipped';

export interface SetupBlocker {
  code: string;
  message: string;
}

export interface SetupChecklistStep {
  code: string;
  station_id?: string;
  title: string;
  description: string;
  href: string;
  cta: string;
  required: boolean;
  status: SetupStepStatus;
  ready: boolean;
  blocked: boolean;
  blocked_reason?: string;
  count: number;
  required_count: number;
  completed_at?: string;
  completed_by?: string;
  updated_at?: string;
  notes?: string;
}

export interface SetupChecklist {
  steps: SetupChecklistStep[];
  required_total: number;
  required_ready: number;
  required_completed: number;
  operationally_ready: boolean;
  blocked: SetupBlocker[];
}

export interface Pump {
  id: string;
  tenant_id: string;
  station_id: string;
  number: number;
  name?: string;
  manufacturer?: string;
  model?: string;
  serial_number?: string;
  status: string;
  installation_date?: string;
}

export interface Nozzle {
  id: string;
  tenant_id: string;
  station_id: string;
  pump_id: string;
  tank_id: string;
  product_id: string;
  number: number;
  /** Default price is an exact decimal STRING (numeric(14,2) -> text). */
  default_price: string;
  meter_decimal_places: number;
  /** Current nozzle-level baseline meter reading, exact decimal string. */
  initial_meter_reading?: string;
  initial_meter_recorded_at?: string;
  initial_meter_recorded_by?: string;
  initial_meter_note?: string;
  status: string;
}

export interface CalibrationChart {
  id: string;
  tenant_id: string;
  tank_id: string;
  name: string;
  effective_from: string;
  effective_until?: string;
  status: string;
  source: string;
  entry_count: number;
}

export interface CalibratedVolume {
  tank_id: string;
  chart_id: string;
  dip_mm: number;
  volume_litres: number;
}

export interface CalibrationPreview {
  preview: true;
  entry_count: number;
  min_dip_mm: number;
  max_dip_mm: number;
  min_volume: number;
  max_volume: number;
}

export interface PumpCalibration {
  id: string;
  tenant_id: string;
  pump_id: string;
  performed_at: string;
  performed_by: string;
  notes?: string;
  tolerance_percent?: number;
  status: string;
}

export interface Incident {
  id: string;
  tenant_id: string;
  station_id: string;
  related_entity_type?: string;
  related_entity_id?: string;
  type: string;
  severity: string;
  description: string;
  status: string;
  opened_at: string;
  opened_by: string;
  resolved_at?: string;
  resolved_by?: string;
  /** Client-supplied offline-replay key, when the create carried one. */
  dedupe_key?: string;
}

/** PRD §6.12 attendant issue types accepted by the self-service report path. */
export type IncidentReportType = 'pump' | 'nozzle' | 'meter' | 'payment' | 'safety' | 'other';

/**
 * Attendant self-service issue report (incidents.report). The incident's
 * station is ALWAYS the station of the caller's current shift, derived
 * server-side; `station_id` is an optional client assertion that must match it
 * (403 otherwise). `dedupe_key` makes the create idempotent for the offline
 * queue: a replay carrying the same key returns the existing incident (200)
 * instead of creating a duplicate (201).
 */
export interface IncidentReportRequest {
  type: IncidentReportType;
  description: string;
  severity?: string;
  station_id?: string;
  related_entity_type?: string;
  related_entity_id?: string;
  dedupe_key?: string;
}

export interface PumpWithNozzles extends Pump {
  nozzles: Nozzle[];
}

export type NotificationSeverity = 'info' | 'success' | 'warning' | 'critical';

export interface Notification {
  id: string;
  type: string;
  title: string;
  body: string;
  severity: NotificationSeverity;
  related_entity_type?: string;
  related_entity_id?: string;
  read_at?: string;
  created_at: string;
}

export interface UnreadCount {
  unread_count: number;
}

/**
 * One per-user notification delivery preference (Feature 11.1): for a
 * (category, channel) pair, whether delivery is enabled plus an optional local
 * quiet window (HH:MM 24h). Self-service — a user reads/writes only their own.
 */
export interface NotificationPreference {
  category: string;
  channel: string;
  enabled: boolean;
  quiet_hours_start?: string;
  quiet_hours_end?: string;
  updated_at: string;
}

/** Preference list plus the valid category/channel keys for rendering toggles. */
export interface NotificationPreferenceList {
  items: NotificationPreference[];
  categories: string[];
  channels: string[];
}

/** Upsert payload for a single notification preference toggle. */
export interface UpsertNotificationPreferenceRequest {
  category: string;
  channel: string;
  enabled: boolean;
  quiet_hours_start?: string | null;
  quiet_hours_end?: string | null;
}

export type JobRunStatus = 'running' | 'success' | 'failure' | 'skipped';

/** One run of a background scheduler job, from the admin job-health endpoint. */
export interface JobRun {
  id: string;
  job_name: string;
  started_at: string;
  finished_at?: string;
  status: JobRunStatus;
  detail?: string;
  /** Derived wall-clock run time in ms; absent while a run is in progress. */
  duration_ms?: number;
}

export interface JobRunList {
  items: JobRun[];
  count: number;
}

/**
 * API-exposed observability snapshot (feature 13.3) — the BFF-reachable
 * equivalent of /readyz + /metrics, surfaced under /api/v1/observability/health.
 */
export interface ObservabilityHealth {
  /** True when postgres is reachable, redis is reachable-or-unconfigured, and there are no dead-lettered events. */
  healthy: boolean;
  /** Per-dependency status, e.g. { postgres: 'ok', redis: 'ok' }. */
  checks: Record<string, string>;
  outbox: {
    /** Unpublished, not-yet-dead-lettered events awaiting dispatch. */
    backlog: number;
    /** Events that exhausted the retry budget and are parked. */
    dead_letter: number;
  };
  /** The newest scheduler run across all jobs, or null when none recorded. */
  scheduler_last_run?: {
    job_name: string;
    status: JobRunStatus;
    started_at: string;
    finished_at?: string;
    duration_ms?: number;
  } | null;
}

// --------------------------------------------------------------------------
// Data lifecycle & retention (Feature 13.2)
// --------------------------------------------------------------------------

/** The data scopes a retention policy can cover. */
export type RetentionScope = 'audit' | 'session' | 'export';

/** A retention policy: keep <scope> data for retention_days days. */
export interface RetentionPolicy {
  id: string;
  scope: RetentionScope;
  retention_days: number;
  status: 'active' | 'disabled';
  created_at: string;
  updated_at: string;
}

export interface RetentionPolicyList {
  items: RetentionPolicy[];
  count: number;
}

export interface CreateRetentionPolicyRequest {
  scope: RetentionScope;
  retention_days: number;
  /** Defaults to 'active' when omitted. */
  status?: 'active' | 'disabled';
}

export interface UpdateRetentionPolicyRequest {
  retention_days?: number;
  status?: 'active' | 'disabled';
}

/** The kind of change a closed-period change request asks for. */
export type ClosedPeriodChangeType = 'reopen' | 'relock';

export type ClosedPeriodChangeStatus = 'requested' | 'approved' | 'rejected';

/**
 * A closed-period change request (maker-checker): a request to reopen or relock
 * a CLOSED/LOCKED accounting period. A different user approves or rejects it
 * (separation of duties); approving authorizes the period transition but does
 * not itself perform it.
 */
export interface ClosedPeriodChangeRequest {
  id: string;
  period_id: string;
  change_type: ClosedPeriodChangeType;
  reason: string;
  status: ClosedPeriodChangeStatus;
  requested_by: string;
  decided_by?: string;
  decision_note?: string;
  requested_at: string;
  decided_at?: string;
}

export interface RequestClosedPeriodChangeRequest {
  change_type: ClosedPeriodChangeType;
  reason: string;
}

export interface StationOverview {
  station: Station;
  tanks: Tank[];
  pumps: PumpWithNozzles[];
  open_shifts: ShiftSummary[];
  open_incidents: Incident[];
}

export interface OperatingDay {
  id: string;
  tenant_id: string;
  station_id: string;
  business_date: string;
  status: string;
  opened_by: string;
  opened_at: string;
  closed_by?: string;
  closed_at?: string;
  locked_by?: string;
  locked_at?: string;
  notes?: string;
}

export interface Shift {
  id: string;
  tenant_id: string;
  station_id: string;
  operating_day_id: string;
  name: string;
  status: string;
  opened_by: string;
  opened_at: string;
  closed_by?: string;
  closed_at?: string;
  approved_by?: string;
  approved_at?: string;
  notes?: string;
  /** Rotation window the shift covers, set when the station uses rotation. */
  slot?: 'morning' | 'evening';
  /** Team rostered onto the shift, set when the station uses rotation. */
  team_id?: string;
}

export interface ShiftAttendant {
  shift_id: string;
  user_id: string;
  assigned_by: string;
  assigned_at: string;
}

export interface NozzleAssignment {
  id: string;
  shift_id: string;
  nozzle_id: string;
  attendant_id: string;
  assigned_at: string;
  /** Set once the assigned attendant confirms the assignment (Mobile Attendant Phase 0). */
  confirmed_at?: string;
}

/** One attendant's check-in/out record for a shift (Mobile Attendant Phase 0). */
export interface ShiftAttendance {
  id: string;
  tenant_id: string;
  station_id: string;
  shift_id: string;
  attendant_id: string;
  status: 'checked_in' | 'checked_out';
  check_in_at: string;
  check_out_at?: string;
  device_info?: Record<string, unknown>;
}

export interface ShiftAttendanceList {
  items: ShiftAttendance[];
  count: number;
}

/**
 * Dual-value supervisor verification of one closing meter reading (Mobile
 * Attendant Phase 0). All reading figures are exact decimal STRINGS
 * (numeric(14,3) -> text); the underlying meter reading is never mutated.
 */
export interface ReadingVerification {
  id: string;
  tenant_id: string;
  station_id: string;
  shift_id: string;
  nozzle_id: string;
  reading_id: string;
  attendant_submitted_reading: string;
  supervisor_verified_reading?: string;
  final_approved_reading: string;
  /**
   * approved/corrected are terminal-good; rejected/flagged are non-terminal
   * HOLDS that block shift approval (PRD §7.8/§9.5). A rejected reading is
   * sent back to the attendant to re-capture; a flagged reading is under
   * supervisor investigation.
   */
  status: 'approved' | 'corrected' | 'rejected' | 'flagged';
  reason?: string;
  verified_by: string;
  verified_at: string;
}

/**
 * A shift's verification set as read by the supervisor review surface
 * (Mobile Attendant Phase 5).
 */
export interface ReadingVerificationPage {
  items: ReadingVerification[];
  count: number;
}

export interface ReadingVerificationList extends ReadingVerificationPage {
  /** Verifications created by THIS batch call (0 on an idempotent rerun). */
  newly_verified: number;
}

/**
 * Supervisor confirmation of a shift's cash submission (Mobile Attendant
 * Phase 0, handover chain). Every money figure is an exact decimal STRING
 * (numeric(14,2) -> text); difference = received − expected.
 */
export interface CollectionReceipt {
  id: string;
  tenant_id: string;
  station_id: string;
  shift_id: string;
  cash_submission_id: string;
  expected_amount: string;
  attendant_submitted_total: string;
  supervisor_received_total: string;
  difference: string;
  /**
   * received/approved_with_difference are terminal-good; rejected and flagged
   * (PRD §9.6) are HOLDS that block shift approval.
   */
  status: 'received' | 'approved_with_difference' | 'rejected' | 'flagged';
  reason?: string;
  supervisor_comment?: string;
  received_by: string;
  received_at: string;
}

export interface ConfirmCashSubmissionRequest {
  /** Exact decimal string of the cash physically received. */
  received_total: string;
  /**
   * "received" (default), "rejected", or "flagged" (under investigation, PRD
   * §9.6); a non-zero difference upgrades an accepted handover to
   * approved_with_difference server-side. rejected and flagged both block
   * shift approval and require a reason.
   */
  status?: 'received' | 'rejected' | 'flagged';
  /** Required when the received total differs from expected or the handover is rejected or flagged. */
  reason?: string;
  supervisor_comment?: string;
}

/**
 * One assigned nozzle's expected opening meter for a shift — the previous
 * shift's final approved closing (Mobile Attendant Phase 0, handover chain).
 * expected_opening_reading is an exact decimal STRING, absent when the nozzle
 * has no prior closing at the station.
 */
export interface ExpectedOpeningReading {
  assignment_id: string;
  nozzle_id: string;
  attendant_id: string;
  expected_opening_reading?: string;
  /** "verified" when derived from a reading verification, "raw" when from the unverified closing. */
  source?: 'verified' | 'raw';
  source_shift_id?: string;
  source_reading_id?: string;
}

export interface ExpectedOpeningReadingList {
  items: ExpectedOpeningReading[];
  count: number;
}

/**
 * The attendant workflow state machine (Mobile Attendant Phase 1).
 * `open_shift` is reserved: in the current backend a supervisor opens the
 * shift, so an expected-but-unopened duty day reports `await_shift_open`.
 */
export type AttendantNextAction =
  | 'off_duty'
  | 'await_shift_open'
  | 'check_in'
  | 'confirm_assignment'
  | 'verify_opening_readings'
  | 'open_shift'
  | 'working'
  | 'submit_closing_readings'
  | 'await_reading_verification'
  | 'submit_collections'
  | 'await_collection_receipt'
  | 'complete'
  | 'blocked';

/** Machine-readable code carried when next_action is `blocked`. */
export type AttendantBlockingCode =
  | 'awaiting_nozzle_assignment'
  | 'awaiting_shift_close'
  | 'collection_rejected';

export interface AttendantStation {
  id: string;
  name: string;
}

/** Present when the actor has no shift yet but their rotation team is on duty today. */
export interface AttendantExpectedToday {
  slot: 'morning' | 'evening';
  team_id: string;
  team_name: string;
}

export interface AttendantAttendance {
  status: 'not_checked_in' | 'checked_in' | 'checked_out';
  check_in_at?: string;
  check_out_at?: string;
}

/** One of the actor's own nozzle assignments on the shift, with its confirmation state. */
export interface AttendantAssignment {
  assignment_id: string;
  nozzle_id: string;
  pump_number: number;
  nozzle_number: number;
  product_name: string;
  product_color: string;
  /** The nozzle's meter precision (0..4); capture screens validate input scale against it client-side, mirroring the server's 422. */
  meter_decimal_places: number;
  assigned_at: string;
  confirmed_at?: string;
}

/**
 * The actor's own meter progress on one nozzle. Reading figures are exact
 * decimal STRINGS (numeric 14,3). verification_status appears once a closing
 * exists: "pending" until a reading verification lands, then that row's status.
 * rejected/flagged (PRD §7.8/§9.5) are non-terminal holds — a rejected reading
 * means the attendant must re-capture; a flagged reading is under supervisor
 * investigation. Once verified, final_reading carries the final approved figure
 * (the supervisor's value when corrected) and verification_reason the
 * supervisor's reason where one was required (Phase 3 review-status screen).
 */
export interface AttendantReading {
  nozzle_id: string;
  /** The ACTIVE closing reading's id, present once a closing exists — needed to drive the /correct resubmit after a rejection. */
  closing_reading_id?: string;
  opening_reading?: string;
  closing_reading?: string;
  verification_status?: 'pending' | 'approved' | 'corrected' | 'rejected' | 'flagged';
  /** The verification's final approved reading (exact decimal string), present once verified. */
  final_reading?: string;
  /** The supervisor's reason, present when the verification required one (corrected/rejected/flagged). */
  verification_reason?: string;
}

/**
 * One frozen shift close line with its nozzle's display labels — the expected
 * collection's per-nozzle calculation basis (litres_sold × unit_price =
 * expected_value, Mobile Attendant Phase 4). Litres/price/value are exact
 * decimal STRINGS; prices are never attendant-editable.
 */
export interface AttendantCloseLine {
  nozzle_id: string;
  pump_number: number;
  nozzle_number: number;
  product_name: string;
  product_color: string;
  /** Exact decimal string (numeric 14,3). */
  opening_reading: string;
  /** Exact decimal string (numeric 14,3). */
  closing_reading: string;
  /** Exact decimal string (numeric 14,3). */
  litres_sold: string;
  /** Exact decimal string (numeric 14,2). */
  unit_price: string;
  /** Exact decimal string (numeric 14,2). */
  expected_value: string;
}

/**
 * The Mobile Attendant App workflow snapshot (self-scoped) — one payload the
 * mobile home screen renders directly, with a computed next_action and a
 * plain-English user_message.
 */
export interface AttendantCurrentShift {
  status: 'off_duty' | 'expected_today' | 'on_shift' | 'complete';
  next_action: AttendantNextAction;
  user_message: string;
  blocking_code?: AttendantBlockingCode;
  station?: AttendantStation;
  shift?: Shift;
  expected_today?: AttendantExpectedToday;
  attendance: AttendantAttendance;
  assignments: AttendantAssignment[];
  readings: AttendantReading[];
  expected_openings_available: boolean;
  /** Exact decimal string (numeric 14,2), present once the shift is closed. */
  expected_cash?: string;
  /** The expected collection's per-nozzle calculation basis, present once the shift is closed. */
  close_lines?: AttendantCloseLine[];
  cash_submission?: CashSubmission;
  collection_receipt?: CollectionReceipt;
}

export interface ShiftDetail extends Shift {
  attendants: ShiftAttendant[];
  nozzle_assignments: NozzleAssignment[];
}

export interface ShiftSummary extends Shift {
  attendants: ShiftAttendant[];
  nozzle_assignments: NozzleAssignment[];
}

export interface MeterReading {
  id: string;
  tenant_id: string;
  shift_id: string;
  nozzle_id: string;
  reading_type: 'opening' | 'closing';
  /** Meter reading is an exact decimal STRING (numeric(14,3) -> text). */
  reading: string;
  recorded_by: string;
  recorded_at: string;
  supersedes_id?: string;
  status: string;
}

export interface DipReading {
  id: string;
  tenant_id: string;
  shift_id: string;
  tank_id: string;
  reading_type: 'opening' | 'closing';
  /** dip_mm + volume_litres are exact decimal STRINGS (numeric(14,3) -> text). */
  dip_mm: string;
  volume_litres: string;
  water_mm?: number;
  temperature_c?: number;
  chart_id: string;
  recorded_by: string;
  recorded_at: string;
  supersedes_id?: string;
  status: string;
}

export interface MeterDispensed {
  nozzle_id: string;
  /** Readings + dispensed litres are exact decimal STRINGS (numeric(14,3) -> text). */
  opening: string;
  closing: string;
  litres_dispensed: string;
}

export interface MeterReadingList {
  items: MeterReading[];
  count: number;
  dispensed: MeterDispensed[];
}

export interface ShiftCloseLine {
  nozzle_id: string;
  /** Readings/litres/price/value are exact decimal STRINGS from the DB. */
  opening_reading: string;
  closing_reading: string;
  litres_sold: string;
  unit_price: string;
  expected_value: string;
}

export interface CashSubmission {
  id: string;
  shift_id: string;
  /** Every tender/variance figure is an exact decimal STRING (numeric(14,2) -> text). */
  expected_cash: string;
  cash_amount: string;
  mobile_money_amount: string;
  card_amount: string;
  credit_amount: string;
  submitted_total: string;
  variance: string;
  submitted_by: string;
  submitted_at: string;
  notes?: string;
}

export interface ShiftCloseSummary {
  shift: Shift;
  lines: ShiftCloseLine[];
  expected_cash: string;
  cash_submission: CashSubmission | null;
}

export interface ShiftException {
  id: string;
  tenant_id: string;
  shift_id: string;
  type: string;
  severity: string;
  detail?: string;
  status: string;
  raised_at: string;
  resolved_by?: string;
  resolved_at?: string;
}

export interface MyShiftNozzle {
  nozzle_id: string;
  pump_number: number;
  nozzle_number: number;
  product_name: string;
  product_color: string;
  tank_id: string;
  tank_code: string;
  default_price: number;
  meter_decimal_places: number;
  initial_meter_reading?: string;
  opening_reading?: number;
  closing_reading?: number;
}

export interface MyShiftTank {
  tank_id: string;
  tank_code: string;
  product_color: string;
  opening_dip_mm?: number;
  opening_volume_litres?: number;
  closing_dip_mm?: number;
  closing_volume_litres?: number;
}

export interface MyShift {
  shift: Shift | null;
  assigned_nozzles: MyShiftNozzle[];
  assigned_tanks: MyShiftTank[];
  /** Expected cash is an exact decimal STRING (numeric(14,2) -> text). */
  expected_cash?: string;
  cash_submission?: CashSubmission | null;
}

export interface OperationsAttendant {
  user_id: string;
  full_name: string;
  email: string;
}

export interface OperationsShift extends Shift {
  attendants: OperationsAttendant[];
  nozzle_assignments: NozzleAssignment[];
  /** expected_cash + litres_sold are exact decimal STRINGS from the DB. */
  expected_cash: string;
  litres_sold: string;
  cash_submission?: CashSubmission | null;
  exceptions: ShiftException[];
  open_exception_count: number;
}

export interface OperationsOverview {
  station: Station;
  day: OperatingDay | null;
  shifts: OperationsShift[];
}

export interface Reconciliation {
  /** Present on a persisted reconciliation; absent on a live preview. */
  id?: string;
  tank_id: string;
  operating_day_id: string;
  /** Every litre/percent figure is an exact decimal STRING from the DB. */
  opening_book: string;
  deliveries_total: string;
  sales_total: string;
  adjustments_total: string;
  closing_book: string;
  closing_physical: string;
  variance_litres: string;
  variance_percent: string;
  tolerance_percent: string;
  over_tolerance: boolean;
  /** draft | exception | sealed (preview reports the would-be draft/exception). */
  status: string;
  sealed_by?: string;
  sealed_at?: string;
}

export interface RecentVariance {
  operating_day_id: string;
  business_date: string;
  variance_litres: number;
  variance_percent: number;
  tolerance_percent: number;
  over_tolerance: boolean;
  status: string;
  sealed_at?: string;
}

export interface InventoryOverviewTank {
  tank: Tank;
  /** Book balance + latest physical are exact decimal STRINGS; fill %/days are display floats. */
  book_balance: string;
  latest_physical?: string;
  latest_physical_at?: string;
  fill_percent: number;
  days_of_stock?: number;
  last_reconciliation?: RecentVariance;
  recent_variances: RecentVariance[];
}

export interface InventoryOverview {
  station: Station;
  tanks: InventoryOverviewTank[];
}

export interface ReconciliationOverviewTank {
  tank: Tank;
  /** Book balance + latest physical are exact decimal STRINGS. */
  book_balance: string;
  latest_physical?: string;
  reconciliation?: Reconciliation;
}

export interface ReconciliationOverview {
  station: Station;
  day: OperatingDay | null;
  all_shifts_approved: boolean;
  tanks: ReconciliationOverviewTank[];
}

// ---- Phase 6: Sales, Payments & Revenue ----

export interface PriceChange {
  id: string;
  tenant_id: string;
  station_id: string;
  product_id: string;
  unit_price: string;
  effective_from: string;
  previous_price?: string;
  reason?: string;
  set_by: string;
  created_at: string;
}

export interface PriceBoardEntry {
  product_id: string;
  product_code: string;
  product_name: string;
  product_color: string;
  active_price?: string;
  next_price?: string;
  next_effective_from?: string;
}

export interface Sale {
  id: string;
  shift_id: string;
  station_id: string;
  operating_day_id: string;
  nozzle_id: string;
  product_id: string;
  tank_id: string;
  litres: number;
  unit_price: string;
  gross_amount: string;
  tax_rate: string;
  tax_amount: string;
  net_amount: string;
  unit_cost?: string;
  cogs_amount?: string;
  margin_amount?: string;
  recorded_at: string;
  /**
   * The sale's current (non-rejected) void status (Feature 4.3): 'requested'
   * while a void awaits approval, 'approved' once the sale is reversed, or
   * undefined when the sale has no active void. Lets the UI distinguish a
   * reversed sale.
   */
  void_status?: SaleVoidStatus;
}

/** The lifecycle states of a sale void (Feature 4.3). */
export type SaleVoidStatus = 'requested' | 'approved' | 'rejected';

/**
 * A sale-void lifecycle row. On approve it becomes the reversal record: the
 * reversal_* fields hold the recognized sale's amounts NEGATED (exact decimal
 * STRINGS), present only in the 'approved' state. The original sale is
 * append-only and is never mutated.
 */
export interface SaleVoid {
  id: string;
  sale_id: string;
  status: SaleVoidStatus;
  reason: string;
  reversal_litres?: string;
  reversal_gross?: string;
  reversal_tax?: string;
  reversal_net?: string;
  reversal_cogs?: string;
  reversal_margin?: string;
  requested_by: string;
  decided_by?: string;
  decision_note?: string;
  requested_at: string;
  decided_at?: string;
}

export interface RequestSaleVoidRequest {
  /** Why the sale is being voided (required). */
  reason: string;
}

export interface TankValuation {
  tank_id: string;
  code: string;
  name: string;
  product_id: string;
  book_litres: number;
  avg_cost?: string;
  stock_value?: string;
}

export interface Payment {
  id: string;
  station_id: string;
  shift_id?: string;
  customer_id?: string;
  tender_type: string;
  amount: string;
  reference?: string;
  received_by: string;
  received_at: string;
  status: string;
  notes?: string;
}

export interface ShiftPaymentReconciliation {
  shift_id: string;
  tendered: string;
  recognized: string;
  variance: string;
  over_threshold: boolean;
}

/** One M-Pesa (Safaricom Daraja) STK Push collection and its lifecycle. */
export interface MpesaTransaction {
  id: string;
  station_id: string;
  checkout_request_id: string;
  merchant_request_id?: string;
  /** Decimal string (e.g. "150.00"). */
  amount: string;
  phone: string;
  /** pending | paid | failed | cancelled */
  status: string;
  result_code?: number;
  mpesa_receipt?: string;
  account_reference?: string;
  description?: string;
  reconciled_revenue_day_id?: string;
  reconciled_at?: string;
  created_at: string;
  updated_at: string;
}

/** Response of initiating an STK push: the pending txn + Daraja's prompt msg. */
export interface MpesaStkPushResult {
  transaction: MpesaTransaction;
  customer_message: string;
}

export interface Customer {
  id: string;
  tenant_id: string;
  code: string;
  name: string;
  contact_name?: string;
  contact_phone?: string;
  contact_email?: string;
  credit_limit: string;
  status: string;
  legal_name?: string;
  trading_name?: string;
  tax_id?: string;
  billing_address?: string;
  account_type?: string;
  default_terms_days?: number;
  notes?: string;
}

export interface CustomerContact {
  id: string;
  name: string;
  role?: string;
  email?: string;
  phone?: string;
  statement_preference: string;
  notification_preference: string;
}

export interface CreditProfile {
  customer_id: string;
  payment_terms_days: number;
  grace_days: number;
  statement_cycle_days: number;
  risk_category: string;
  warning_threshold_pct: string;
  hold: boolean;
  hold_reason?: string;
}

export interface CreditPosition {
  customer_id: string;
  credit_limit: string;
  exposure: string;
  available_credit: string;
  overdue_amount: string;
  status: string;
  hold: boolean;
  hold_reason?: string;
  over_limit: boolean;
  warning: boolean;
}

export interface CustomerPriceAgreement {
  id: string;
  customer_id: string;
  product_id: string;
  station_id?: string;
  price_type: string;
  fixed_price?: string;
  discount?: string;
  markup?: string;
  effective_from: string;
  effective_to?: string;
  status: string;
  version: number;
}

export interface Vehicle {
  id: string;
  customer_id: string;
  registration: string;
  fleet_number?: string;
  vin?: string;
  vehicle_type?: string;
  default_product_id?: string;
  tank_capacity?: string;
  odometer_required: boolean;
  status: string;
}

export interface Driver {
  id: string;
  customer_id: string;
  name: string;
  phone?: string;
  license_number?: string;
  has_pin: boolean;
  status: string;
  allowed_product_ids: string[];
  assignment_rule: string;
}

export interface FuelCredential {
  id: string;
  customer_id: string;
  vehicle_id?: string;
  driver_id?: string;
  credential_type: string;
  masked_label: string;
  status: string;
  issued_at: string;
  expiry_date?: string;
}

export interface CredentialValidation extends FuelCredential {
  customer_name: string;
  expired: boolean;
  usable: boolean;
}

export interface FuelAuthorization {
  id: string;
  customer_id: string;
  vehicle_id?: string;
  driver_id?: string;
  credential_id?: string;
  station_id: string;
  product_id?: string;
  requested_amount: string;
  approved_amount: string;
  odometer?: string;
  status: string;
  source: string;
  consumed_by?: string;
}

export interface AuthorizationDenied {
  error: string;
  rule_code: string;
  detail: string;
}

export interface OdometerReading {
  id: string;
  reading: string;
  distance_since?: string;
  validation_status: string;
  note?: string;
  captured_at: string;
}

export interface VehicleConsumption {
  vehicle_id: string;
  registration: string;
  fuelings: number;
  amount_total: string;
  odometer_start?: string;
  odometer_end?: string;
  distance?: string;
}

export interface CreditStatement {
  id: string;
  customer_id: string;
  period_start: string;
  period_end: string;
  opening_balance: string;
  charges: string;
  payments: string;
  closing_balance: string;
  status: string;
}

export interface CreditAlert {
  id: string;
  customer_id: string;
  alert_type: string;
  severity: string;
  status: string;
  detail?: string;
}

export interface StationGroup {
  id: string;
  name: string;
  kind?: string;
  status: string;
}

export interface ApprovalRequest {
  id: string;
  workflow_type: string;
  reference_type?: string;
  reference_id?: string;
  amount: string;
  required_approvals: number;
  approvals_count: number;
  status: string;
  requested_by: string;
}

export interface ApprovalPolicy {
  id: string;
  workflow_type: string;
  /** Decimal string; the minimum amount at or above which the policy applies. */
  min_amount: string;
  required_approvals: number;
  required_role?: string | null;
  status: string;
}

/**
 * Outcome of POST /api/v1/approval-policies/simulate — whether a workflow +
 * amount would require approval under the current active policies, resolved by
 * the same engine that raises real requests (feature 9.2).
 */
export interface ApprovalSimulation {
  workflow_type: string;
  /** True when an active policy applies and an approval would be required. */
  approval_required: boolean;
  /** Alias of approval_required: whether any active policy matched. */
  matched: boolean;
  /** Strictest matching policy's required-approvals count (1 when none matched). */
  required_approvals: number;
  /** Required approver role from the matching policy, if any. */
  required_role?: string | null;
  /** Id of the matching policy, when one matched. */
  policy_id?: string | null;
}

/** One switchable enterprise scope from GET /api/v1/enterprise/scopes (feature 13.1). */
export interface EnterpriseScope {
  scope_type: 'company' | 'region' | 'group' | 'station';
  scope_id: string | null;
  /** Human label resolved from the company / region / group / station name. */
  label: string;
  /** Number of stations the scope covers. */
  station_count: number;
}

/** The enterprise scopes a user may switch the active chain view between. */
export interface EnterpriseScopes {
  /** True when the user holds a tenant-level grant (all stations). */
  tenant_wide: boolean;
  scopes: EnterpriseScope[];
}

export interface EnterpriseOverview {
  from: string;
  to: string;
  gross_revenue: string;
  net_revenue: string;
  margin_total: string;
  ap_outstanding: string;
  ar_outstanding: string;
  open_incidents: number;
  approvals_waiting: number;
  projection_rebuilt_at?: string;
}

export interface StationRank {
  station_id: string;
  name: string;
  gross_revenue: string;
  margin_total: string;
}

export interface CentralPriceRollout {
  id: string;
  product_id: string;
  scope_type: string;
  scope_id?: string;
  unit_price: string;
  effective_from: string;
  status: string;
  stations_applied: number;
}

export interface StockTransfer {
  id: string;
  from_tank_id: string;
  to_tank_id: string;
  product_id: string;
  litres: string;
  status: string;
}

export interface RiskAlert {
  id: string;
  rule_code?: string;
  rule_id?: string;
  alert_type: string;
  severity: string;
  status: string;
  station_id?: string;
  subject_type?: string;
  subject_id?: string;
  detail?: string;
  amount?: string;
  recommended_action?: string;
  score: number;
}

/**
 * The resolved source-record link for an insight (Feature 11.3): the aggregate
 * kind + id the insight was derived from, and an in-app route when one exists.
 */
export interface InsightSource {
  kind: string;
  id: string;
  href?: string;
}

/**
 * One persisted, deterministic insight (Feature 11.3), projected from a risk
 * alert and linked to the source record it was derived from. Read-only; gated
 * by risk.read.
 */
export interface Insight {
  id: string;
  rule_code?: string;
  type: string;
  severity: string;
  status: string;
  detail?: string;
  amount?: string;
  recommended_action?: string;
  station_id?: string;
  source?: InsightSource;
  created_at: string;
}

// RiskRuleInput is the create/update payload for a rule. All fields besides
// code/name are optional on update; threshold stays a decimal string.
export interface RiskRuleInput {
  code: string;
  name: string;
  rule_type?: string;
  category?: string;
  condition?: string;
  severity?: string;
  description?: string;
  message_template?: string;
  recommended_action?: string;
  threshold?: string;
  lookback_days?: number;
  comparison_period_days?: number;
  status?: string;
  enabled?: boolean;
}

// RiskRule is one configurable rule in the Rules & Insights Engine. `condition`
// names a code-backed evaluator (not an expression); `threshold` is a decimal
// string (or null) and stays off the float path.
export interface RiskRule {
  id: string;
  code: string;
  name: string;
  rule_type: string;
  status: string;
  category: string;
  condition?: string;
  threshold?: string;
  lookback_days: number;
  comparison_period_days?: number;
  severity: string;
  description?: string;
  message_template?: string;
  recommended_action?: string;
  enabled: boolean;
}

export interface ARentry {
  id: string;
  customer_id: string;
  entry_type: string;
  amount: string;
  balance_after: string;
  source_ref_type?: string;
  source_ref_id?: string;
  recorded_at: string;
  notes?: string;
}

export interface CustomerStatement {
  customer: Customer;
  balance: string;
  entries: ARentry[];
}

export interface RevenueDay {
  id: string;
  station_id: string;
  operating_day_id: string;
  business_date: string;
  gross_revenue: string;
  net_revenue: string;
  tax_total: string;
  cogs_total: string;
  margin_total: string;
  cash_total: string;
  mobile_money_total: string;
  card_total: string;
  credit_total: string;
  voucher_total: string;
  tender_total: string;
  cash_variance: string;
  status: string;
  locked_by?: string;
  locked_at?: string;
}

export interface RevenueSummary {
  gross_revenue: string;
  net_revenue: string;
  tax_total: string;
  cogs_total: string;
  margin_total: string;
  litres_sold: number;
  sale_count: number;
}

export interface RevenueTenderBreakdown {
  cash: string;
  mobile_money: string;
  card: string;
  credit: string;
  voucher: string;
  total: string;
}

export interface RevenueOverview {
  station: Station;
  day?: OperatingDay;
  summary?: RevenueSummary;
  tenders?: RevenueTenderBreakdown;
  recent_days: RevenueDay[];
}

export interface CustomerBalance {
  customer_id: string;
  code: string;
  name: string;
  balance: string;
}

// ---- Standard report exports (CSV + PDF) ----

/** Reporting window for the financials and general-ledger exports. */
export type ReportPeriod = 'this-month' | 'last-month' | 'ytd' | 'last-30';

/** Accountant-importable format for the general-ledger export. */
export type GeneralLedgerFormat = 'csv' | 'iif' | 'xero';

/**
 * Discriminated spec for a standard report. Passed to client.reportUrl /
 * client.fetchReportBlob to build the same-origin download URL.
 *
 * CSV specs stream spreadsheet-ready data; the two `*-pdf` specs render the
 * formal printable documents (a daily shift/close report per station and the
 * tenant financial statement). Every export is permission-gated and audited.
 */
export type ReportSpec =
  | { kind: 'revenue'; stationID: string }
  | { kind: 'inventory'; stationID: string }
  | { kind: 'reconciliation'; stationID: string; operatingDayID?: string }
  | { kind: 'financials'; period?: ReportPeriod }
  | { kind: 'ar-aging' }
  | { kind: 'daily-close-pdf'; stationID: string; operatingDayID?: string }
  | { kind: 'financials-pdf'; period?: ReportPeriod }
  | { kind: 'gl-export'; period?: ReportPeriod; format?: GeneralLedgerFormat }
  // Excel (XLSX) exports mirroring the revenue/reconciliation/financials CSVs.
  | { kind: 'revenue-xlsx'; stationID: string }
  | { kind: 'reconciliation-xlsx'; stationID: string; operatingDayID?: string }
  | { kind: 'financials-xlsx'; period?: ReportPeriod };

// ---- Deterministic report insights + data-quality (reporting hub) ----

/** The signature reports that expose a deterministic insights endpoint. */
export type ReportKey =
  | 'daily-close'
  | 'stock-reconciliation'
  | 'sales-summary'
  | 'cash-reconciliation'
  | 'customer-aging';

export type InsightSeverity = 'info' | 'warning' | 'critical';

/** A single deterministic observation about a report's already-computed data. */
export interface ReportInsight {
  severity: InsightSeverity;
  message: string;
  recommended_action?: string;
}

/** A reason a report's figures may be incomplete or subject to change. */
export interface DataQualityWarning {
  message: string;
}

/** The insights + data-quality annotations for one report view. */
export interface ReportInsights {
  insights: ReportInsight[];
  data_quality: DataQualityWarning[];
}

// ---- Structured, permission-aware report API (drillable envelope) ----

/** The structured report keys returned by the envelope endpoints. */
export type StructuredReportKey =
  | 'inventory-reconciliation'
  | 'station-close'
  | 'cash-reconciliation'
  | 'fuel-loss'
  | 'sales'
  | 'delivery'
  | 'profitability'
  | 'station-comparison';

/** Identifies a structured report instance. */
export interface ReportEnvelopeMetadata {
  report_key: string;
  title: string;
  /** RFC3339 generation timestamp. */
  generated_at: string;
  station_id?: string;
  period: string;
}

/** One data-quality banner: a severity level and a message. */
export interface ReportDataQualityItem {
  level: string;
  message: string;
}

/** One headline figure. `value` is always a string (decimal money/litre or count). */
export interface ReportSummaryMetric {
  label: string;
  value: string;
  unit?: string;
  delta?: string;
  direction?: string;
}

/** The generic, drillable grid. Every cell is a string (decimal-safe). */
export interface ReportTable {
  columns: string[];
  rows: string[][];
}

/** A link to a deeper view. */
export interface ReportDrilldownLink {
  label: string;
  href: string;
}

/** A downloadable rendering of the same report. */
export interface ReportExportOption {
  format: string;
  url: string;
}

/**
 * An additive, report-specific breakdown of a station-day's recorded tenders by
 * type and their total. Every figure is an exact decimal STRING (numeric ->
 * text), read straight from the revenue_days rollup — never recomputed. Present
 * on reports that surface a tender split (e.g. Daily Station Close); the Sales
 * report reuses the same shape for its payment-method donut.
 */
export interface ReportTenderMix {
  cash: string;
  mobile_money: string;
  card: string;
  credit: string;
  voucher: string;
  total: string;
}

/**
 * The shared, drillable structured-report payload. `chart_data` is
 * report-specific (always decimal strings, never float money) so it is typed as
 * `unknown` here — narrow it per `metadata.report_key` at the call site.
 * `tender_mix` is an optional additive breakdown present on reports that
 * surface a tender split.
 */
export interface ReportEnvelope {
  metadata: ReportEnvelopeMetadata;
  filters_used: Record<string, string>;
  data_quality: ReportDataQualityItem[];
  summary: ReportSummaryMetric[];
  chart_data: unknown;
  tender_mix?: ReportTenderMix;
  table: ReportTable;
  insights: ReportInsight[];
  recommended_actions: string[];
  drilldown: ReportDrilldownLink[];
  export_options: ReportExportOption[];
}

/**
 * One open invoice in a Customer Credit drilldown (§5.9), aged into a bucket by
 * its due date. Money figures are exact decimal strings.
 */
export interface CustomerCreditDrilldownInvoice {
  invoice_id: string;
  invoice_number: string | null;
  invoice_date: string;
  due_date: string | null;
  amount: string;
  outstanding: string;
  days_overdue: number;
  bucket: string;
  status: string;
}

/** One posted/recent payment in a Customer Credit drilldown. */
export interface CustomerCreditDrilldownPayment {
  payment_id: string;
  payment_date: string;
  method: string;
  reference: string | null;
  amount: string;
  allocated: string;
  status: string;
}

/**
 * A customer's balance -> invoices -> payments drilldown for the Customer Credit
 * report (§5.9): the open invoices aged into buckets and the recent payments.
 */
export interface CustomerCreditDrilldown {
  customer_id: string;
  invoices: CustomerCreditDrilldownInvoice[];
  payments: CustomerCreditDrilldownPayment[];
}

/** One category card on the reports landing page. */
export interface ReportCategory {
  key: string;
  title: string;
  description: string;
  headline: string;
  headline_unit: string;
  alert_count: number;
  href: string;
}

/** The reports landing payload: categories with live headline metrics. */
export interface ReportsOverview {
  generated_at: string;
  categories: ReportCategory[];
}

/** Availability of a catalog category or report. */
export type ReportAvailability = 'live' | 'partial' | 'placeholder';

/**
 * A category's live key metric on the Reports Home. `value` is a decimal
 * string for money/litres or a count, and is `null` when no figure is genuinely
 * computable (a partial/placeholder category) or a sensitive figure is gated
 * away — `reason` then explains why, honestly. Never a fabricated number.
 */
export interface ReportCatalogMetric {
  label: string;
  value: string | null;
  unit?: string;
  reason?: string;
}

/** One report under a category that the actor may see. */
export interface ReportCatalogReport {
  key: string;
  name: string;
  description: string;
  endpoint: string;
  required_permission: string;
  availability: ReportAvailability;
}

/** One category card on the Reports & Intelligence Center home. */
export interface ReportCatalogCategory {
  key: string;
  name: string;
  description: string;
  icon: string;
  sort_order: number;
  required_permission: string;
  availability: ReportAvailability;
  target_route: string;
  metric: ReportCatalogMetric;
  alert_count: number;
  reports: ReportCatalogReport[];
}

/** One hub-level data-quality warning aggregated across categories. */
export interface ReportCatalogDataQuality {
  category_key: string;
  level: 'info' | 'warning';
  message: string;
}

/**
 * The Reports & Intelligence Center catalog: the permission-filtered blueprint
 * categories (each with availability, a live key metric, an alert count and the
 * reports under it) plus a hub-level data-quality band.
 */
export interface ReportCatalog {
  generated_at: string;
  categories: ReportCatalogCategory[];
  data_quality: ReportCatalogDataQuality[];
}

/** The keys + formats accepted by the unified export endpoint. */
export interface ReportExportRequest {
  report_key: string;
  format: 'csv' | 'pdf' | 'xlsx';
  filters?: Record<string, string>;
}

/** The unified export response: the same-origin URL to fetch the file from. */
export interface ReportExportResult {
  report_key: string;
  format: string;
  url: string;
}

/** The body to record a report export job (Feature 10.7). */
export interface ExportJobRequest {
  report_key: string;
  format: 'csv' | 'pdf' | 'xlsx';
  filters?: Record<string, string>;
}

/**
 * A recorded report export job: a durable receipt of an export request and the
 * resulting file's metadata, powering the reporting hub's export history.
 */
export interface ExportJob {
  id: string;
  report_key: string;
  format: string;
  filters: Record<string, string>;
  status: 'queued' | 'running' | 'completed' | 'failed';
  file_url: string | null;
  file_name: string | null;
  file_size: number | null;
  error: string | null;
  requested_by: string;
  /** RFC3339 creation timestamp. */
  created_at: string;
}

// ---- Phase 7: Finance & Accounting Control ----

export interface Account {
  id: string;
  code: string;
  name: string;
  type: string;
  normal_balance: string;
  parent_id?: string;
  system_key?: string;
  status: string;
}

export interface AccountingPeriod {
  id: string;
  start_date: string;
  end_date: string;
  status: string;
  closed_by?: string;
  closed_at?: string;
  locked_by?: string;
  locked_at?: string;
}

export interface JournalLine {
  id: string;
  account_id: string;
  debit: string;
  credit: string;
  station_id?: string;
  memo?: string;
}

export interface JournalEntry {
  id: string;
  entry_number: number;
  period_id: string;
  entry_date: string;
  source_type: string;
  source_id?: string;
  station_id?: string;
  status: string;
  memo?: string;
  reverses_entry_id?: string;
  reversed_by_entry_id?: string;
  total?: string;
  lines?: JournalLine[];
}

export interface Payable {
  id: string;
  supplier_id: string;
  source_invoice_id: string;
  invoice_number?: string;
  invoice_date?: string;
  due_date?: string;
  amount: string;
  outstanding_amount: string;
  station_id?: string;
  status: string;
  journal_entry_id?: string;
}

export interface SupplierAging {
  supplier_id: string;
  /** Total outstanding across all buckets (decimal string). */
  outstanding: string;
  open_count: number;
  /** Not yet due (due_date >= today) or no due date. Decimal string. */
  current: string;
  /** 1-30 days past due. Decimal string. */
  d1_30: string;
  /** 31-60 days past due. Decimal string. */
  d31_60: string;
  /** 61-90 days past due. Decimal string. */
  d61_90: string;
  /** More than 90 days past due. Decimal string. */
  d90_plus: string;
}

/**
 * AP aging response: per-supplier day-aged buckets plus a tenant-wide grand
 * total. `totals.supplier_id` is the zero UUID and is not a real supplier.
 */
export interface ApAgingResponse extends Paginated<SupplierAging> {
  totals: SupplierAging;
}

export interface TrialBalanceRow {
  account_id: string;
  code: string;
  name: string;
  type: string;
  normal_balance: string;
  debit: string;
  credit: string;
  balance: string;
}

export interface TrialBalance {
  as_of: string;
  rows: TrialBalanceRow[];
  balanced: boolean;
}

export interface IncomeStatement {
  from: string;
  to: string;
  revenue: string;
  expenses: string;
  net_profit: string;
}

export interface BalanceSheet {
  as_of: string;
  assets: string;
  liabilities: string;
  equity: string;
}

export interface FinanceOverview {
  balance_sheet: { assets: string; liabilities: string; equity: string };
  income_statement: { revenue: string; expenses: string; net_profit: string };
  ap_supplier_count: number;
  open_periods: number;
  recent_entries: JournalEntry[];
}

export interface GeneralLedgerRow {
  entry_id: string;
  entry_number: string;
  entry_date: string;
  source_type: string;
  memo: string;
  debit: string;
  credit: string;
}

export interface CashReconciliation {
  id: string;
  station_id: string;
  operating_day_id: string;
  expected_cash: string;
  counted_cash: string;
  variance: string;
  status: string;
  notes?: string;
  journal_entry_id?: string;
  reviewed_by?: string;
}

export interface BankAccount {
  id: string;
  name: string;
  account_number?: string;
  currency: string;
  status: string;
}

export interface BankDeposit {
  id: string;
  station_id: string;
  bank_account_id: string;
  slip_number?: string;
  amount: string;
  reference?: string;
  expected_bank_date?: string;
  actual_bank_date?: string;
  status: string;
  prepared_entry_id?: string;
  confirmed_entry_id?: string;
}

export interface BankStatementLine {
  id: string;
  import_id: string;
  bank_account_id: string;
  txn_date: string;
  value_date?: string;
  amount: string;
  reference?: string;
  description?: string;
  status: string;
  matched_doc_type?: string;
  matched_doc_id?: string;
  journal_entry_id?: string;
}

export interface CustomerInvoice {
  id: string;
  customer_id: string;
  invoice_number?: string;
  invoice_date: string;
  due_date?: string;
  amount: string;
  outstanding_amount: string;
  source_type: string;
  station_id?: string;
  status: string;
  journal_entry_id?: string;
}

export interface CustomerPayment {
  id: string;
  customer_id: string;
  payment_date: string;
  method: string;
  reference?: string;
  amount: string;
  source_account_key: string;
  status: string;
  journal_entry_id?: string;
}

export interface ExpenseCategory {
  id: string;
  name: string;
  /** GL account this category maps spend to (accounting mapping). */
  account_key: string;
  /** Optional money amount at/above which an expense warrants approval; null when unset. */
  approval_threshold?: string | null;
  status: string;
}

export interface Expense {
  id: string;
  station_id?: string;
  category_id?: string;
  payee?: string;
  expense_date: string;
  amount: string;
  account_key: string;
  payment_mode: string;
  reference?: string;
  status: string;
  journal_entry_id?: string;
  approved_by?: string;
}

export interface PettyCashFloat {
  id: string;
  station_id: string;
  name: string;
  balance: string;
  status: string;
}

export interface PettyCashTransaction {
  id: string;
  txn_type: string;
  amount: string;
  balance_after: string;
  description?: string;
  account_key?: string;
  overdraw: boolean;
  journal_entry_id?: string;
  created_at?: string;
}

export interface AccountingExport {
  id: string;
  export_type: string;
  format: string;
  row_count: number;
  checksum: string;
  provisional: boolean;
  generated_at?: string;
}

export interface AccountingExportResult extends AccountingExport {
  export_id: string;
  csv: string;
}

export interface CloseChecklist {
  checks: Record<string, number>;
  blockers: number;
  can_close: boolean;
  periods: Array<{ id: string; start_date: string; end_date: string; status: string }>;
}

export interface UserSummary {
  id: string;
  email: string;
  full_name: string;
  status: string;
  mfa_enabled: boolean;
  last_login_at?: string;
  created_at: string;
  roles: string[];
  station_ids: string[];
  tenant_wide: boolean;
}

export interface Role {
  id: string;
  code: string;
  name: string;
  description?: string;
  is_system: boolean;
  permissions: string[];
}

export interface Session {
  id: string;
  issued_at: string;
  expires_at: string;
  user_agent?: string;
  is_current: boolean;
}

export interface AuditLogEntry {
  id: string;
  tenant_id?: string;
  actor_id?: string;
  action: string;
  entity_type: string;
  entity_id?: string;
  previous_value?: unknown;
  new_value?: unknown;
  reason?: string;
  ip?: string;
  user_agent?: string;
  request_id?: string;
  occurred_at: string;
}

/**
 * Result of POST /audit-logs/export. The server returns the generated CSV
 * inline as text alongside the run metadata (the export is itself audited as
 * `export.generated`).
 */
export interface AuditExportResult {
  export_id: string;
  export_type: string;
  format: string;
  from: string;
  to: string;
  row_count: number;
  checksum: string;
  csv: string;
}

export interface Paginated<T> {
  items: T[];
  count: number;
  limit?: number;
  offset?: number;
  /**
   * True when the returned window may not be the tail of the result set, i.e.
   * another page can be fetched (the Go writePaged/writePagedMore envelope).
   */
  has_more: boolean;
}

// ----------- Workforce (Phase 11) -----------

export type EmployeeRole = string;

export interface EmployeeRoleOption {
  id: string;
  tenant_id: string;
  code: EmployeeRole;
  name: string;
  is_default: boolean;
  status: 'active' | 'inactive';
  sort_order: number;
  created_at: string;
  updated_at: string;
}

export interface Employee {
  id: string;
  tenant_id: string;
  station_id: string;
  user_id?: string;
  full_name: string;
  role: EmployeeRole;
  employee_code?: string;
  phone?: string;
  email?: string;
  status: 'active' | 'inactive';
  team_id?: string;
  created_at: string;
  updated_at: string;
}

export interface ShiftTeam {
  id: string;
  tenant_id: string;
  station_id: string;
  name: string;
  rotation_order: number;
  member_count: number;
}

export interface RotationAnchor {
  station_id: string;
  rotation_anchor_date: string | null;
}

export interface ScheduledTeam {
  date: string;
  slot: 'morning' | 'evening';
  team: ShiftTeam | null;
  members: Employee[];
}

export interface DayRoster {
  date: string;
  morning_team: ShiftTeam | null;
  evening_team: ShiftTeam | null;
  resting_team: ShiftTeam | null;
}

/** The unpaginated `{ items, count }` envelope the workforce list endpoints return. */
export interface WorkforceList<T> {
  items: T[];
  count: number;
}

export interface ApiError {
  error: string;
  status: number;
}
