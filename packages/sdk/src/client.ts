import type {
  AuditLogEntry,
  CalibratedVolume,
  CalibrationChart,
  CalibrationPreview,
  Company,
  Incident,
  LoginRequest,
  LoginResponse,
  Me,
  MePermissions,
  Nozzle,
  Paginated,
  Product,
  Pump,
  PumpCalibration,
  Region,
  Role,
  Session,
  Station,
  Tank,
  UserSummary,
} from './types';

/**
 * SdkError carries the HTTP status alongside the parsed API error body so
 * callers can branch on it without re-reading the response.
 */
export class SdkError extends Error {
  readonly status: number;
  readonly body: unknown;

  constructor(message: string, status: number, body: unknown) {
    super(message);
    this.name = 'SdkError';
    this.status = status;
    this.body = body;
  }
}

export interface ClientConfig {
  baseURL: string;
  /** Returns the bearer token to attach, or null when unauthenticated. */
  getToken?: () => string | null;
  /** Optional fetch override (for tests, instrumentation, retries). */
  fetch?: typeof fetch;
}

interface RequestOptions {
  method?: 'GET' | 'POST' | 'PUT' | 'PATCH' | 'DELETE';
  body?: unknown;
  /** Force the request to skip the session Authorization header (e.g. login). */
  unauthenticated?: boolean;
  /** Extra headers merged last — used to pass a non-session bearer. */
  headers?: Record<string, string>;
  signal?: AbortSignal;
}

export class Client {
  private readonly baseURL: string;
  private readonly getToken: () => string | null;
  private readonly fetchImpl: typeof fetch;

  constructor(cfg: ClientConfig) {
    this.baseURL = cfg.baseURL.replace(/\/$/, '');
    this.getToken = cfg.getToken ?? (() => null);
    this.fetchImpl = cfg.fetch ?? fetch;
  }

  /**
   * Low-level request helper. Most callers should use the typed
   * endpoint methods below.
   */
  async request<T>(path: string, opts: RequestOptions = {}): Promise<T> {
    const url = `${this.baseURL}${path}`;
    const headers: Record<string, string> = {
      Accept: 'application/json',
    };

    if (!opts.unauthenticated) {
      const token = this.getToken();
      if (token) headers.Authorization = `Bearer ${token}`;
    }

    let body: BodyInit | undefined;
    if (opts.body !== undefined) {
      if (opts.body instanceof FormData) {
        // Let the browser set multipart/form-data with its boundary.
        body = opts.body;
      } else {
        headers['Content-Type'] = 'application/json';
        body = JSON.stringify(opts.body);
      }
    }

    if (opts.headers) {
      Object.assign(headers, opts.headers);
    }

    const res = await this.fetchImpl(url, {
      method: opts.method ?? 'GET',
      headers,
      body,
      signal: opts.signal,
      credentials: 'omit',
    });

    if (res.status === 204) {
      return undefined as T;
    }

    const text = await res.text();
    const parsed = text ? safeParse(text) : null;

    if (!res.ok) {
      const message =
        (parsed && typeof parsed === 'object' && 'error' in parsed
          ? String((parsed as { error: unknown }).error)
          : `HTTP ${res.status}`) ?? `HTTP ${res.status}`;
      throw new SdkError(message, res.status, parsed);
    }

    return parsed as T;
  }

  // ----------- Platform (operator/IaC, not user sessions) -----------

  /**
   * Provision a new tenant + its first admin user. Requires the static
   * PLATFORM_ADMIN_TOKEN bearer, passed explicitly here rather than via
   * the client's session token. Returns a one-time password-reset token
   * the new admin uses to set their password.
   */
  createTenant(
    platformToken: string,
    req: { name: string; slug: string; admin_email: string; admin_full_name: string },
    signal?: AbortSignal,
  ): Promise<{
    tenant_id: string;
    tenant_slug: string;
    admin_user_id: string;
    admin_email: string;
    password_reset_token: string;
  }> {
    return this.request('/api/v1/platform/tenants', {
      method: 'POST',
      body: req,
      unauthenticated: true,
      headers: { Authorization: `Bearer ${platformToken}` },
      signal,
    });
  }

  // ----------- Auth -----------

  login(req: LoginRequest, signal?: AbortSignal): Promise<LoginResponse> {
    return this.request<LoginResponse>('/api/v1/auth/login', {
      method: 'POST',
      body: req,
      unauthenticated: true,
      signal,
    });
  }

  logout(signal?: AbortSignal): Promise<void> {
    return this.request<void>('/api/v1/auth/logout', { method: 'POST', signal });
  }

  refresh(signal?: AbortSignal): Promise<{ expires_at: string }> {
    return this.request('/api/v1/auth/refresh', { method: 'POST', signal });
  }

  requestPasswordReset(
    req: { tenant_slug: string; email: string },
    signal?: AbortSignal,
  ): Promise<void> {
    return this.request<void>('/api/v1/auth/password-reset/request', {
      method: 'POST',
      body: req,
      unauthenticated: true,
      signal,
    });
  }

  confirmPasswordReset(
    req: { token: string; new_password: string },
    signal?: AbortSignal,
  ): Promise<void> {
    return this.request<void>('/api/v1/auth/password-reset/confirm', {
      method: 'POST',
      body: req,
      unauthenticated: true,
      signal,
    });
  }

  // ----------- Me -----------

  me(signal?: AbortSignal): Promise<Me> {
    return this.request<Me>('/api/v1/me', { signal });
  }

  mePermissions(signal?: AbortSignal): Promise<MePermissions> {
    return this.request<MePermissions>('/api/v1/me/permissions', { signal });
  }

  // ----------- Me (session management + password) -----------

  listMySessions(signal?: AbortSignal): Promise<Paginated<Session>> {
    return this.request<Paginated<Session>>('/api/v1/me/sessions', { signal });
  }

  revokeMySession(sessionID: string, signal?: AbortSignal): Promise<void> {
    return this.request<void>(`/api/v1/me/sessions/${encodeURIComponent(sessionID)}`, {
      method: 'DELETE',
      signal,
    });
  }

  changeMyPassword(
    req: { old_password: string; new_password: string },
    signal?: AbortSignal,
  ): Promise<void> {
    return this.request<void>('/api/v1/me/password', {
      method: 'POST',
      body: req,
      signal,
    });
  }

  // ----------- Companies -----------

  listCompanies(signal?: AbortSignal): Promise<Paginated<Company>> {
    return this.request<Paginated<Company>>('/api/v1/companies', { signal });
  }

  createCompany(req: Partial<Company> & { name: string }, signal?: AbortSignal): Promise<Company> {
    return this.request<Company>('/api/v1/companies', { method: 'POST', body: req, signal });
  }

  updateCompany(id: string, req: Partial<Company>, signal?: AbortSignal): Promise<Company> {
    return this.request<Company>(`/api/v1/companies/${encodeURIComponent(id)}`, {
      method: 'PATCH',
      body: req,
      signal,
    });
  }

  deleteCompany(id: string, signal?: AbortSignal): Promise<void> {
    return this.request<void>(`/api/v1/companies/${encodeURIComponent(id)}`, {
      method: 'DELETE',
      signal,
    });
  }

  // ----------- Regions -----------

  listRegions(opts: { companyID?: string } = {}, signal?: AbortSignal): Promise<Paginated<Region>> {
    const qs = opts.companyID ? `?company_id=${encodeURIComponent(opts.companyID)}` : '';
    return this.request<Paginated<Region>>(`/api/v1/regions${qs}`, { signal });
  }

  createRegion(
    req: { company_id: string; name: string; code?: string },
    signal?: AbortSignal,
  ): Promise<Region> {
    return this.request<Region>('/api/v1/regions', { method: 'POST', body: req, signal });
  }

  updateRegion(id: string, req: Partial<Region>, signal?: AbortSignal): Promise<Region> {
    return this.request<Region>(`/api/v1/regions/${encodeURIComponent(id)}`, {
      method: 'PATCH',
      body: req,
      signal,
    });
  }

  deleteRegion(id: string, signal?: AbortSignal): Promise<void> {
    return this.request<void>(`/api/v1/regions/${encodeURIComponent(id)}`, {
      method: 'DELETE',
      signal,
    });
  }

  // ----------- Stations -----------

  listStations(
    opts: { regionID?: string } = {},
    signal?: AbortSignal,
  ): Promise<Paginated<Station>> {
    const qs = opts.regionID ? `?region_id=${encodeURIComponent(opts.regionID)}` : '';
    return this.request<Paginated<Station>>(`/api/v1/stations${qs}`, { signal });
  }

  getStation(stationID: string, signal?: AbortSignal): Promise<Station> {
    return this.request<Station>(`/api/v1/stations/${encodeURIComponent(stationID)}`, {
      signal,
    });
  }

  createStation(
    req: Partial<Station> & { company_id: string; name: string; code: string },
    signal?: AbortSignal,
  ): Promise<Station> {
    return this.request<Station>('/api/v1/stations', { method: 'POST', body: req, signal });
  }

  updateStation(id: string, req: Partial<Station>, signal?: AbortSignal): Promise<Station> {
    return this.request<Station>(`/api/v1/stations/${encodeURIComponent(id)}`, {
      method: 'PATCH',
      body: req,
      signal,
    });
  }

  deleteStation(id: string, signal?: AbortSignal): Promise<void> {
    return this.request<void>(`/api/v1/stations/${encodeURIComponent(id)}`, {
      method: 'DELETE',
      signal,
    });
  }

  // ----------- Products -----------

  listProducts(signal?: AbortSignal): Promise<Paginated<Product>> {
    return this.request<Paginated<Product>>('/api/v1/products', { signal });
  }

  getProduct(id: string, signal?: AbortSignal): Promise<Product> {
    return this.request<Product>(`/api/v1/products/${encodeURIComponent(id)}`, { signal });
  }

  createProduct(
    req: Partial<Product> & { code: string; name: string },
    signal?: AbortSignal,
  ): Promise<Product> {
    return this.request<Product>('/api/v1/products', { method: 'POST', body: req, signal });
  }

  updateProduct(id: string, req: Partial<Product>, signal?: AbortSignal): Promise<Product> {
    return this.request<Product>(`/api/v1/products/${encodeURIComponent(id)}`, {
      method: 'PATCH',
      body: req,
      signal,
    });
  }

  deleteProduct(id: string, signal?: AbortSignal): Promise<void> {
    return this.request<void>(`/api/v1/products/${encodeURIComponent(id)}`, {
      method: 'DELETE',
      signal,
    });
  }

  // ----------- Tanks -----------

  listTanks(opts: { stationID?: string } = {}, signal?: AbortSignal): Promise<Paginated<Tank>> {
    const qs = opts.stationID ? `?station_id=${encodeURIComponent(opts.stationID)}` : '';
    return this.request<Paginated<Tank>>(`/api/v1/tanks${qs}`, { signal });
  }

  getTank(id: string, signal?: AbortSignal): Promise<Tank> {
    return this.request<Tank>(`/api/v1/tanks/${encodeURIComponent(id)}`, { signal });
  }

  createTank(
    req: Partial<Tank> & {
      station_id: string;
      product_id: string;
      name: string;
      code: string;
      capacity_litres: number;
    },
    signal?: AbortSignal,
  ): Promise<Tank> {
    return this.request<Tank>('/api/v1/tanks', { method: 'POST', body: req, signal });
  }

  updateTank(id: string, req: Partial<Tank>, signal?: AbortSignal): Promise<Tank> {
    return this.request<Tank>(`/api/v1/tanks/${encodeURIComponent(id)}`, {
      method: 'PATCH',
      body: req,
      signal,
    });
  }

  deleteTank(id: string, signal?: AbortSignal): Promise<void> {
    return this.request<void>(`/api/v1/tanks/${encodeURIComponent(id)}`, {
      method: 'DELETE',
      signal,
    });
  }

  updateTankStatus(
    id: string,
    status: string,
    reason?: string,
    signal?: AbortSignal,
  ): Promise<Tank> {
    return this.request<Tank>(`/api/v1/tanks/${encodeURIComponent(id)}/status`, {
      method: 'PATCH',
      body: { status, reason },
      signal,
    });
  }

  // ----------- Tank calibration -----------

  listCalibrationCharts(
    tankID: string,
    signal?: AbortSignal,
  ): Promise<Paginated<CalibrationChart>> {
    return this.request<Paginated<CalibrationChart>>(
      `/api/v1/tanks/${encodeURIComponent(tankID)}/calibration-charts`,
      { signal },
    );
  }

  activeCalibrationChart(tankID: string, signal?: AbortSignal): Promise<CalibrationChart> {
    return this.request<CalibrationChart>(
      `/api/v1/tanks/${encodeURIComponent(tankID)}/calibration-charts/active`,
      { signal },
    );
  }

  calibratedVolume(tankID: string, dipMM: number, signal?: AbortSignal): Promise<CalibratedVolume> {
    return this.request<CalibratedVolume>(
      `/api/v1/tanks/${encodeURIComponent(tankID)}/calibrated-volume?dip_mm=${encodeURIComponent(dipMM)}`,
      { signal },
    );
  }

  /**
   * Upload a strapping-chart CSV (header: dip_mm,volume_litres). With
   * dryRun, the server validates and returns a preview without persisting.
   */
  uploadCalibrationChart(
    tankID: string,
    opts: {
      file: File | Blob;
      name: string;
      source?: string;
      effectiveFrom?: string;
      dryRun?: boolean;
    },
    signal?: AbortSignal,
  ): Promise<CalibrationChart | CalibrationPreview> {
    const form = new FormData();
    form.set('file', opts.file);
    form.set('name', opts.name);
    if (opts.source) form.set('source', opts.source);
    if (opts.effectiveFrom) form.set('effective_from', opts.effectiveFrom);
    const qs = opts.dryRun ? '?dry_run=true' : '';
    return this.request<CalibrationChart | CalibrationPreview>(
      `/api/v1/tanks/${encodeURIComponent(tankID)}/calibration-charts${qs}`,
      { method: 'POST', body: form, signal },
    );
  }

  // ----------- Pumps -----------

  listPumps(opts: { stationID?: string } = {}, signal?: AbortSignal): Promise<Paginated<Pump>> {
    const qs = opts.stationID ? `?station_id=${encodeURIComponent(opts.stationID)}` : '';
    return this.request<Paginated<Pump>>(`/api/v1/pumps${qs}`, { signal });
  }

  getPump(id: string, signal?: AbortSignal): Promise<Pump> {
    return this.request<Pump>(`/api/v1/pumps/${encodeURIComponent(id)}`, { signal });
  }

  createPump(
    req: Partial<Pump> & { station_id: string; number: number },
    signal?: AbortSignal,
  ): Promise<Pump> {
    return this.request<Pump>('/api/v1/pumps', { method: 'POST', body: req, signal });
  }

  updatePump(id: string, req: Partial<Pump>, signal?: AbortSignal): Promise<Pump> {
    return this.request<Pump>(`/api/v1/pumps/${encodeURIComponent(id)}`, {
      method: 'PATCH',
      body: req,
      signal,
    });
  }

  deletePump(id: string, signal?: AbortSignal): Promise<void> {
    return this.request<void>(`/api/v1/pumps/${encodeURIComponent(id)}`, {
      method: 'DELETE',
      signal,
    });
  }

  updatePumpStatus(
    id: string,
    status: string,
    reason?: string,
    signal?: AbortSignal,
  ): Promise<Pump> {
    return this.request<Pump>(`/api/v1/pumps/${encodeURIComponent(id)}/status`, {
      method: 'PATCH',
      body: { status, reason },
      signal,
    });
  }

  listPumpCalibrations(pumpID: string, signal?: AbortSignal): Promise<Paginated<PumpCalibration>> {
    return this.request<Paginated<PumpCalibration>>(
      `/api/v1/pumps/${encodeURIComponent(pumpID)}/calibrations`,
      { signal },
    );
  }

  recordPumpCalibration(
    pumpID: string,
    req: { performed_at?: string; notes?: string; tolerance_percent?: number; status?: string },
    signal?: AbortSignal,
  ): Promise<PumpCalibration> {
    return this.request<PumpCalibration>(
      `/api/v1/pumps/${encodeURIComponent(pumpID)}/calibrations`,
      { method: 'POST', body: req, signal },
    );
  }

  // ----------- Nozzles -----------

  listNozzles(
    opts: { stationID?: string; pumpID?: string } = {},
    signal?: AbortSignal,
  ): Promise<Paginated<Nozzle>> {
    const qs = new URLSearchParams();
    if (opts.stationID) qs.set('station_id', opts.stationID);
    if (opts.pumpID) qs.set('pump_id', opts.pumpID);
    const q = qs.toString();
    return this.request<Paginated<Nozzle>>(`/api/v1/nozzles${q ? `?${q}` : ''}`, { signal });
  }

  createNozzle(
    req: {
      pump_id: string;
      tank_id: string;
      number: number;
      default_price?: number;
      meter_decimal_places?: number;
    },
    signal?: AbortSignal,
  ): Promise<Nozzle> {
    return this.request<Nozzle>('/api/v1/nozzles', { method: 'POST', body: req, signal });
  }

  updateNozzle(
    id: string,
    req: Partial<Pick<Nozzle, 'number' | 'default_price' | 'meter_decimal_places' | 'status'>> & {
      tank_id?: string;
    },
    signal?: AbortSignal,
  ): Promise<Nozzle> {
    return this.request<Nozzle>(`/api/v1/nozzles/${encodeURIComponent(id)}`, {
      method: 'PATCH',
      body: req,
      signal,
    });
  }

  deleteNozzle(id: string, signal?: AbortSignal): Promise<void> {
    return this.request<void>(`/api/v1/nozzles/${encodeURIComponent(id)}`, {
      method: 'DELETE',
      signal,
    });
  }

  // ----------- Incidents -----------

  listIncidents(
    opts: { stationID?: string; status?: string; severity?: string } = {},
    signal?: AbortSignal,
  ): Promise<Paginated<Incident>> {
    const qs = new URLSearchParams();
    if (opts.stationID) qs.set('station_id', opts.stationID);
    if (opts.status) qs.set('status', opts.status);
    if (opts.severity) qs.set('severity', opts.severity);
    const q = qs.toString();
    return this.request<Paginated<Incident>>(`/api/v1/incidents${q ? `?${q}` : ''}`, { signal });
  }

  createIncident(
    req: {
      station_id: string;
      description: string;
      type?: string;
      severity?: string;
      related_entity_type?: string;
      related_entity_id?: string;
    },
    signal?: AbortSignal,
  ): Promise<Incident> {
    return this.request<Incident>('/api/v1/incidents', { method: 'POST', body: req, signal });
  }

  updateIncidentStatus(id: string, status: string, signal?: AbortSignal): Promise<Incident> {
    return this.request<Incident>(`/api/v1/incidents/${encodeURIComponent(id)}/status`, {
      method: 'PATCH',
      body: { status },
      signal,
    });
  }

  // ----------- Users -----------

  listUsers(signal?: AbortSignal): Promise<Paginated<UserSummary>> {
    return this.request<Paginated<UserSummary>>('/api/v1/users', { signal });
  }

  inviteUser(
    req: { email: string; full_name: string },
    signal?: AbortSignal,
  ): Promise<{ id: string; email: string; full_name: string }> {
    return this.request('/api/v1/admin/users', { method: 'POST', body: req, signal });
  }

  updateUserStatus(
    userID: string,
    status: 'active' | 'suspended',
    signal?: AbortSignal,
  ): Promise<{ id: string; status: string }> {
    return this.request(`/api/v1/admin/users/${encodeURIComponent(userID)}/status`, {
      method: 'PATCH',
      body: { status },
      signal,
    });
  }

  grantUserRole(
    userID: string,
    roleCode: string,
    signal?: AbortSignal,
  ): Promise<{ user_id: string; role_code: string }> {
    return this.request(`/api/v1/admin/users/${encodeURIComponent(userID)}/roles`, {
      method: 'POST',
      body: { role_code: roleCode },
      signal,
    });
  }

  revokeUserRole(userID: string, roleCode: string, signal?: AbortSignal): Promise<void> {
    return this.request<void>(
      `/api/v1/admin/users/${encodeURIComponent(userID)}/roles/${encodeURIComponent(roleCode)}`,
      { method: 'DELETE', signal },
    );
  }

  grantStationAccess(
    userID: string,
    stationID: string,
    signal?: AbortSignal,
  ): Promise<{ user_id: string; station_id: string }> {
    return this.request(`/api/v1/admin/users/${encodeURIComponent(userID)}/station-access`, {
      method: 'POST',
      body: { station_id: stationID },
      signal,
    });
  }

  revokeStationAccess(userID: string, stationID: string, signal?: AbortSignal): Promise<void> {
    return this.request<void>(
      `/api/v1/admin/users/${encodeURIComponent(userID)}/station-access/${encodeURIComponent(stationID)}`,
      { method: 'DELETE', signal },
    );
  }

  // ----------- Roles -----------

  listRoles(signal?: AbortSignal): Promise<Paginated<Role>> {
    return this.request<Paginated<Role>>('/api/v1/roles', { signal });
  }

  // ----------- Audit logs -----------

  listAuditLogs(
    opts: {
      action?: string;
      entityType?: string;
      entityID?: string;
      actorID?: string;
      since?: string;
      until?: string;
      limit?: number;
    } = {},
    signal?: AbortSignal,
  ): Promise<Paginated<AuditLogEntry>> {
    const qs = new URLSearchParams();
    if (opts.action) qs.set('action', opts.action);
    if (opts.entityType) qs.set('entity_type', opts.entityType);
    if (opts.entityID) qs.set('entity_id', opts.entityID);
    if (opts.actorID) qs.set('actor_id', opts.actorID);
    if (opts.since) qs.set('since', opts.since);
    if (opts.until) qs.set('until', opts.until);
    if (opts.limit) qs.set('limit', String(opts.limit));
    const q = qs.toString();
    return this.request<Paginated<AuditLogEntry>>(`/api/v1/audit-logs${q ? `?${q}` : ''}`, {
      signal,
    });
  }
}

function safeParse(text: string): unknown {
  try {
    return JSON.parse(text);
  } catch {
    return text;
  }
}
