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
  | 'fuel-loss';

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
 * The shared, drillable structured-report payload. `chart_data` is
 * report-specific (always decimal strings, never float money) so it is typed as
 * `unknown` here — narrow it per `metadata.report_key` at the call site.
 */
export interface ReportEnvelope {
  metadata: ReportEnvelopeMetadata;
  filters_used: Record<string, string>;
  data_quality: ReportDataQualityItem[];
  summary: ReportSummaryMetric[];
  chart_data: unknown;
  table: ReportTable;
  insights: ReportInsight[];
  recommended_actions: string[];
  drilldown: ReportDrilldownLink[];
  export_options: ReportExportOption[];
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
  outstanding: string;
  open_count: number;
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
  account_key: string;
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

export type EmployeeRole = 'pump_attendant' | 'cashier' | 'supervisor' | 'manager' | 'other';

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
