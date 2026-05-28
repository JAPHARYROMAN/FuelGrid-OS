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
  default_price: number;
  tax_rate: number;
  density_kg_m3?: number;
  loss_tolerance_percent: number;
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
  ordered_litres: number;
  unit_price: string;
  received_litres: number;
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
  volume_litres: number;
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
  quantity_variance_litres?: number;
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
  litres: number;
  balance_after: number;
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
  capacity_litres: number;
  safe_min_litres: number;
  safe_max_litres: number;
  dead_stock_litres: number;
  has_water_sensor: boolean;
  has_temp_sensor: boolean;
  status: string;
  installation_date?: string;
  decommission_date?: string;
  /** Latest dip-resolved volume; present only on the station overview. */
  current_litres?: number;
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
  default_price: number;
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
  reading: number;
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
  dip_mm: number;
  volume_litres: number;
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
  opening: number;
  closing: number;
  litres_dispensed: number;
}

export interface MeterReadingList {
  items: MeterReading[];
  count: number;
  dispensed: MeterDispensed[];
}

export interface ShiftCloseLine {
  nozzle_id: string;
  opening_reading: number;
  closing_reading: number;
  litres_sold: number;
  unit_price: number;
  expected_value: number;
}

export interface CashSubmission {
  id: string;
  shift_id: string;
  expected_cash: number;
  cash_amount: number;
  mobile_money_amount: number;
  card_amount: number;
  credit_amount: number;
  submitted_total: number;
  variance: number;
  submitted_by: string;
  submitted_at: string;
  notes?: string;
}

export interface ShiftCloseSummary {
  shift: Shift;
  lines: ShiftCloseLine[];
  expected_cash: number;
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
  expected_cash?: number;
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
  expected_cash: number;
  litres_sold: number;
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
  opening_book: number;
  deliveries_total: number;
  sales_total: number;
  adjustments_total: number;
  closing_book: number;
  closing_physical: number;
  variance_litres: number;
  variance_percent: number;
  tolerance_percent: number;
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
  book_balance: number;
  latest_physical?: number;
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
  book_balance: number;
  latest_physical?: number;
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
}

export interface ApiError {
  error: string;
  status: number;
}
