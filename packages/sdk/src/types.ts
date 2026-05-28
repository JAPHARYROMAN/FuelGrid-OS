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
