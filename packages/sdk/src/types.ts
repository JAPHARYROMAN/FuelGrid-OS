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
  tank_code: string;
  default_price: number;
  meter_decimal_places: number;
  opening_reading?: number;
  closing_reading?: number;
}

export interface MyShift {
  shift: Shift | null;
  assigned_nozzles: MyShiftNozzle[];
  expected_cash?: number;
  cash_submission?: CashSubmission | null;
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
