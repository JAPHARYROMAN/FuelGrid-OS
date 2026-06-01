import type {
  Account,
  AccountingExport,
  AccountingExportResult,
  AccountingPeriod,
  ApprovalRequest,
  CentralPriceRollout,
  EnterpriseOverview,
  RiskAlert,
  StationGroup,
  StationRank,
  StockTransfer,
  BalanceSheet,
  BankAccount,
  BankDeposit,
  BankStatementLine,
  CashReconciliation,
  CloseChecklist,
  FinanceOverview,
  GeneralLedgerRow,
  IncomeStatement,
  TrialBalance,
  JournalEntry,
  Payable,
  SupplierAging,
  AuditLogEntry,
  CalibratedVolume,
  CalibrationChart,
  CalibrationPreview,
  Company,
  DipReading,
  Expense,
  ExpenseCategory,
  Incident,
  InventoryOverview,
  JobRunList,
  LoginRequest,
  LoginResponse,
  Me,
  MePermissions,
  MfaBackupCodes,
  MfaEnrollment,
  MeterReading,
  MeterReadingList,
  MyShift,
  Notification,
  Nozzle,
  NozzleAssignment,
  OperatingDay,
  OperationsOverview,
  Paginated,
  PettyCashFloat,
  PettyCashTransaction,
  PriceBoardEntry,
  PriceChange,
  Product,
  ProcurementDiscrepancy,
  ProcurementOverview,
  Pump,
  PumpCalibration,
  PurchaseOrder,
  Reconciliation,
  ReconciliationOverview,
  Region,
  ReportSpec,
  Role,
  Sale,
  TankValuation,
  ARentry,
  CreditAlert,
  CreditPosition,
  CreditProfile,
  CreditStatement,
  Customer,
  CustomerBalance,
  CustomerContact,
  CustomerInvoice,
  CustomerPayment,
  CustomerPriceAgreement,
  CustomerStatement,
  CredentialValidation,
  Driver,
  FuelAuthorization,
  FuelCredential,
  OdometerReading,
  Vehicle,
  VehicleConsumption,
  Payment,
  RevenueDay,
  RevenueOverview,
  ShiftPaymentReconciliation,
  CashSubmission,
  Session,
  Shift,
  ShiftCloseSummary,
  ShiftDetail,
  ShiftException,
  Station,
  StationOverview,
  StockMovement,
  Supplier,
  SupplierInvoice,
  Tank,
  UnreadCount,
  UserSummary,
  Delivery,
  Employee,
  EmployeeRole,
  ShiftTeam,
  RotationAnchor,
  ScheduledTeam,
  DayRoster,
  WorkforceList,
} from './types';
import { loginResponseSchema, meSchema, mePermissionsSchema } from './schemas';

/**
 * SdkError carries the HTTP status alongside the parsed API error body so
 * callers can branch on it without re-reading the response. It also carries
 * the server's X-Request-Id (when present) so a failed call can be correlated
 * with server logs/traces — surfaced to observability as a tag (OBS-1).
 */
export class SdkError extends Error {
  readonly status: number;
  readonly body: unknown;
  /** The response's X-Request-Id header, when the server returned one. */
  readonly requestId: string | null;

  constructor(message: string, status: number, body: unknown, requestId: string | null = null) {
    super(message);
    this.name = 'SdkError';
    this.status = status;
    this.body = body;
    this.requestId = requestId;
  }
}

/**
 * A minimal structural schema interface, satisfied by a Zod schema's
 * `.safeParse`. Declared here so the SDK transport can validate critical
 * payloads without importing zod into the transport file's type surface.
 */
export interface ResponseSchema<T> {
  safeParse(
    data: unknown,
  ): { success: true; data: T } | { success: false; error: { message: string } };
}

export interface ClientConfig {
  baseURL: string;
  /** Returns the bearer token to attach, or null when unauthenticated. */
  getToken?: () => string | null;
  /** Optional fetch override (for tests, instrumentation, retries). */
  fetch?: typeof fetch;
  /**
   * Invoked once whenever any request receives a 401, before the SdkError is
   * thrown. The web app injects this to clear the session and redirect to
   * login — a transport-level logout backstop so an expired token anywhere in
   * the app lands the user on /login rather than a broken screen (SEC-3).
   */
  onUnauthorized?: () => void;
}

interface RequestOptions {
  method?: 'GET' | 'POST' | 'PUT' | 'PATCH' | 'DELETE';
  body?: unknown;
  /** Force the request to skip the session Authorization header (e.g. login). */
  unauthenticated?: boolean;
  /** Extra headers merged last — used to pass a non-session bearer. */
  headers?: Record<string, string>;
  signal?: AbortSignal;
  /**
   * Optional runtime schema. When set, a 2xx body is parsed with it and a
   * mismatch throws an SdkError (status 0). Used ONLY for a curated set of
   * critical/auth responses (see requestValidated); most calls keep the plain
   * typed cast to avoid an over-strict schema breaking the running app.
   */
  schema?: ResponseSchema<unknown>;
}

export class Client {
  private readonly baseURL: string;
  private readonly getToken: () => string | null;
  private readonly fetchImpl: typeof fetch;
  private readonly onUnauthorized?: () => void;

  constructor(cfg: ClientConfig) {
    this.baseURL = cfg.baseURL.replace(/\/$/, '');
    this.getToken = cfg.getToken ?? (() => null);
    this.onUnauthorized = cfg.onUnauthorized;
    // The global `fetch` must be invoked with `this` bound to the global
    // object. Storing it on an instance field and calling it as
    // `this.fetchImpl(...)` would set `this` to the Client, which Chrome
    // rejects with "TypeError: Illegal invocation". Bind the default so
    // every call dispatches correctly. A caller-supplied fetch (tests,
    // instrumentation) is used as-is.
    this.fetchImpl = cfg.fetch ?? fetch.bind(globalThis);
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

    let res: Response;
    try {
      res = await this.fetchImpl(url, {
        method: opts.method ?? 'GET',
        headers,
        body,
        signal: opts.signal,
        // same-origin so the httpOnly session cookie travels to a same-origin
        // BFF proxy (WEB-001): the web app points the base URL at /api/bff and
        // the proxy reads the cookie server-side. For a cross-origin base URL
        // this behaves like 'omit' (no credentials sent), preserving the prior
        // header-only contract for any direct-to-API consumer.
        credentials: 'same-origin',
      });
    } catch (err) {
      // A deliberate cancellation propagates unchanged so callers (and React
      // Query) can detect it. Any other transport failure (offline, DNS, TLS,
      // CORS) is surfaced as an SdkError with status 0 — not a raw TypeError —
      // so the same error handling applies everywhere (SDK-03).
      if (err instanceof DOMException && err.name === 'AbortError') {
        throw err;
      }
      const detail = err instanceof Error ? err.message : String(err);
      throw new SdkError(`network request failed: ${detail}`, 0, null);
    }

    // The server stamps every response with X-Request-Id; carry it on any
    // SdkError so a failure can be correlated to server logs/traces (OBS-1).
    const requestId = res.headers.get('X-Request-Id');

    if (res.status === 204) {
      return undefined as T;
    }

    let text: string;
    try {
      text = await res.text();
    } catch (err) {
      if (err instanceof DOMException && err.name === 'AbortError') {
        throw err;
      }
      throw new SdkError('failed to read the response body', 0, null, requestId);
    }
    const parsed = text ? safeParse(text) : null;

    if (!res.ok) {
      // A 401 means the session is gone or invalid. Notify the app (logout +
      // redirect) before surfacing the error, so an expired token anywhere
      // lands the user on /login rather than a broken screen (SEC-3).
      if (res.status === 401) {
        this.onUnauthorized?.();
      }
      const message =
        (parsed && typeof parsed === 'object' && 'error' in parsed
          ? String((parsed as { error: unknown }).error)
          : `HTTP ${res.status}`) ?? `HTTP ${res.status}`;
      throw new SdkError(message, res.status, parsed, requestId);
    }

    // Scoped runtime validation: only when an explicit schema was passed
    // (the curated critical/auth calls). A mismatch is a contract break —
    // throw an SdkError so it surfaces through the same path as a 5xx.
    if (opts.schema) {
      const result = opts.schema.safeParse(parsed);
      if (!result.success) {
        throw new SdkError(
          `response failed schema validation: ${result.error.message}`,
          0,
          parsed,
          requestId,
        );
      }
      return result.data as T;
    }

    return parsed as T;
  }

  /**
   * Like request, but validates the 2xx body against a runtime schema. Use
   * ONLY for the curated critical/auth responses (see schemas.ts) — there is
   * no e2e contract test yet, so blanket validation would risk breaking the
   * running app on any drift the hand-maintained types missed.
   */
  requestValidated<T>(
    schema: ResponseSchema<T>,
    path: string,
    opts: RequestOptions = {},
  ): Promise<T> {
    return this.request<T>(path, { ...opts, schema });
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
    // Critical/auth response — runtime-validated (SDK-01).
    return this.requestValidated<LoginResponse>(loginResponseSchema, '/api/v1/auth/login', {
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
    // Critical/auth response — runtime-validated (SDK-01).
    return this.requestValidated<Me>(meSchema, '/api/v1/me', { signal });
  }

  mePermissions(signal?: AbortSignal): Promise<MePermissions> {
    // Critical/auth response — runtime-validated (SDK-01).
    return this.requestValidated<MePermissions>(mePermissionsSchema, '/api/v1/me/permissions', {
      signal,
    });
  }

  /** The actor's own current shift + assigned nozzles (attendant console). */
  myActiveShift(signal?: AbortSignal): Promise<MyShift> {
    return this.request<MyShift>('/api/v1/me/active-shift', { signal });
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

  // ----------- Me (MFA) -----------

  /** Begin TOTP enrollment; returns the secret + otpauth URI to display. */
  mfaEnroll(signal?: AbortSignal): Promise<MfaEnrollment> {
    return this.request<MfaEnrollment>('/api/v1/me/mfa/enroll', { method: 'POST', signal });
  }

  /** Confirm enrollment with a TOTP code; enables MFA and returns backup codes. */
  mfaConfirm(code: string, signal?: AbortSignal): Promise<MfaBackupCodes> {
    return this.request<MfaBackupCodes>('/api/v1/me/mfa/confirm', {
      method: 'POST',
      body: { code },
      signal,
    });
  }

  /** Disable MFA. Requires a current TOTP or backup code. */
  mfaDisable(code: string, signal?: AbortSignal): Promise<void> {
    return this.request<void>('/api/v1/me/mfa/disable', {
      method: 'POST',
      body: { code },
      signal,
    });
  }

  /** Regenerate one-time backup recovery codes (returns the fresh set once). */
  regenerateBackupCodes(signal?: AbortSignal): Promise<MfaBackupCodes> {
    return this.request<MfaBackupCodes>('/api/v1/me/mfa/backup-codes', { method: 'POST', signal });
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

  getStationOverview(stationID: string, signal?: AbortSignal): Promise<StationOverview> {
    return this.request<StationOverview>(
      `/api/v1/stations/${encodeURIComponent(stationID)}/overview`,
      { signal },
    );
  }

  /** The station's active operating day + its shifts (supervisor dashboard). */
  getOperationsOverview(stationID: string, signal?: AbortSignal): Promise<OperationsOverview> {
    return this.request<OperationsOverview>(
      `/api/v1/stations/${encodeURIComponent(stationID)}/operations-overview`,
      { signal },
    );
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

  // ----------- Procurement: suppliers, orders, receipts, invoices -----------

  listSuppliers(signal?: AbortSignal): Promise<Paginated<Supplier>> {
    return this.request<Paginated<Supplier>>('/api/v1/suppliers', { signal });
  }

  getSupplier(id: string, signal?: AbortSignal): Promise<Supplier> {
    return this.request<Supplier>(`/api/v1/suppliers/${encodeURIComponent(id)}`, { signal });
  }

  createSupplier(
    req: {
      code: string;
      name: string;
      contact_name?: string;
      contact_email?: string;
      contact_phone?: string;
      payment_terms_days?: number;
      product_ids?: string[];
    },
    signal?: AbortSignal,
  ): Promise<Supplier> {
    return this.request<Supplier>('/api/v1/suppliers', { method: 'POST', body: req, signal });
  }

  updateSupplier(id: string, req: Partial<Supplier>, signal?: AbortSignal): Promise<Supplier> {
    return this.request<Supplier>(`/api/v1/suppliers/${encodeURIComponent(id)}`, {
      method: 'PATCH',
      body: req,
      signal,
    });
  }

  deactivateSupplier(id: string, signal?: AbortSignal): Promise<Supplier> {
    return this.request<Supplier>(`/api/v1/suppliers/${encodeURIComponent(id)}`, {
      method: 'DELETE',
      signal,
    });
  }

  listPurchaseOrders(
    opts: { stationID?: string; supplierID?: string; status?: string } = {},
    signal?: AbortSignal,
  ): Promise<Paginated<PurchaseOrder>> {
    const qs = new URLSearchParams();
    if (opts.stationID) qs.set('station_id', opts.stationID);
    if (opts.supplierID) qs.set('supplier_id', opts.supplierID);
    if (opts.status) qs.set('status', opts.status);
    const q = qs.toString();
    return this.request<Paginated<PurchaseOrder>>(`/api/v1/purchase-orders${q ? `?${q}` : ''}`, {
      signal,
    });
  }

  getPurchaseOrder(id: string, signal?: AbortSignal): Promise<PurchaseOrder> {
    return this.request<PurchaseOrder>(`/api/v1/purchase-orders/${encodeURIComponent(id)}`, {
      signal,
    });
  }

  createPurchaseOrder(
    req: {
      station_id: string;
      supplier_id: string;
      expected_delivery_date?: string;
      notes?: string;
      lines: Array<{ product_id: string; ordered_litres: string; unit_price: string }>;
    },
    signal?: AbortSignal,
  ): Promise<PurchaseOrder> {
    return this.request<PurchaseOrder>('/api/v1/purchase-orders', {
      method: 'POST',
      body: req,
      signal,
    });
  }

  updatePurchaseOrder(
    id: string,
    req: {
      expected_delivery_date?: string;
      notes?: string;
      lines?: Array<{ product_id: string; ordered_litres: string; unit_price: string }>;
    },
    signal?: AbortSignal,
  ): Promise<PurchaseOrder> {
    return this.request<PurchaseOrder>(`/api/v1/purchase-orders/${encodeURIComponent(id)}`, {
      method: 'PATCH',
      body: req,
      signal,
    });
  }

  transitionPurchaseOrder(
    id: string,
    status: string,
    signal?: AbortSignal,
  ): Promise<PurchaseOrder> {
    return this.request<PurchaseOrder>(`/api/v1/purchase-orders/${encodeURIComponent(id)}/status`, {
      method: 'POST',
      body: { status },
      signal,
    });
  }

  receivePurchaseOrderReceipt(
    purchaseOrderID: string,
    req: {
      tank_id: string;
      po_line_id: string;
      volume_litres: number;
      dip_before_litres?: number;
      dip_after_litres?: number;
      line_unit_price?: string;
      freight_amount?: string;
      duty_amount?: string;
      levies_amount?: string;
      notes?: string;
    },
    signal?: AbortSignal,
  ): Promise<{
    delivery: Delivery;
    movement: StockMovement;
    dip_mismatch: boolean;
    quantity_discrepancy: boolean;
    quantity_variance_litres: number;
    purchase_order_status: string;
  }> {
    return this.request(`/api/v1/purchase-orders/${encodeURIComponent(purchaseOrderID)}/receipts`, {
      method: 'POST',
      body: req,
      signal,
    });
  }

  listStationDeliveries(stationID: string, signal?: AbortSignal): Promise<Paginated<Delivery>> {
    return this.request<Paginated<Delivery>>(
      `/api/v1/stations/${encodeURIComponent(stationID)}/deliveries`,
      { signal },
    );
  }

  getDeliveryReceipt(id: string, signal?: AbortSignal): Promise<Delivery> {
    return this.request<Delivery>(`/api/v1/deliveries/${encodeURIComponent(id)}`, { signal });
  }

  recordSupplierInvoice(
    req: {
      purchase_order_id: string;
      invoice_number: string;
      received_at?: string;
      due_date?: string;
      notes?: string;
      lines: Array<{
        po_line_id: string;
        delivery_id?: string;
        invoiced_litres: number;
        unit_price: string;
        amount?: string;
      }>;
    },
    signal?: AbortSignal,
  ): Promise<SupplierInvoice> {
    return this.request<SupplierInvoice>('/api/v1/supplier-invoices', {
      method: 'POST',
      body: req,
      signal,
    });
  }

  getSupplierInvoice(id: string, signal?: AbortSignal): Promise<SupplierInvoice> {
    return this.request<SupplierInvoice>(`/api/v1/supplier-invoices/${encodeURIComponent(id)}`, {
      signal,
    });
  }

  approveSupplierInvoice(id: string, signal?: AbortSignal): Promise<SupplierInvoice> {
    return this.request<SupplierInvoice>(
      `/api/v1/supplier-invoices/${encodeURIComponent(id)}/approve`,
      { method: 'POST', signal },
    );
  }

  resolveProcurementDiscrepancy(id: string, signal?: AbortSignal): Promise<ProcurementDiscrepancy> {
    return this.request<ProcurementDiscrepancy>(
      `/api/v1/procurement-discrepancies/${encodeURIComponent(id)}/status`,
      { method: 'PATCH', body: { status: 'resolved' }, signal },
    );
  }

  getProcurementOverview(stationID: string, signal?: AbortSignal): Promise<ProcurementOverview> {
    return this.request<ProcurementOverview>(
      `/api/v1/stations/${encodeURIComponent(stationID)}/procurement-overview`,
      { signal },
    );
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
      capacity_litres: string;
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
      default_price?: string;
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

  // ----------- Operating days -----------

  listOperatingDays(stationID: string, signal?: AbortSignal): Promise<Paginated<OperatingDay>> {
    return this.request<Paginated<OperatingDay>>(
      `/api/v1/stations/${encodeURIComponent(stationID)}/operating-days`,
      { signal },
    );
  }

  getOperatingDay(id: string, signal?: AbortSignal): Promise<OperatingDay> {
    return this.request<OperatingDay>(`/api/v1/operating-days/${encodeURIComponent(id)}`, {
      signal,
    });
  }

  openOperatingDay(
    stationID: string,
    req: { business_date?: string; notes?: string } = {},
    signal?: AbortSignal,
  ): Promise<OperatingDay> {
    return this.request<OperatingDay>(
      `/api/v1/stations/${encodeURIComponent(stationID)}/operating-days`,
      { method: 'POST', body: req, signal },
    );
  }

  updateOperatingDayStatus(
    id: string,
    status: string,
    reason?: string,
    signal?: AbortSignal,
  ): Promise<OperatingDay> {
    return this.request<OperatingDay>(`/api/v1/operating-days/${encodeURIComponent(id)}/status`, {
      method: 'PATCH',
      body: { status, reason },
      signal,
    });
  }

  lockOperatingDay(id: string, reason?: string, signal?: AbortSignal): Promise<OperatingDay> {
    return this.request<OperatingDay>(`/api/v1/operating-days/${encodeURIComponent(id)}/lock`, {
      method: 'PATCH',
      body: { reason },
      signal,
    });
  }

  // ----------- Shifts -----------

  listShifts(
    stationID: string,
    opts: { operatingDayID?: string } = {},
    signal?: AbortSignal,
  ): Promise<Paginated<Shift>> {
    const qs = opts.operatingDayID
      ? `?operating_day_id=${encodeURIComponent(opts.operatingDayID)}`
      : '';
    return this.request<Paginated<Shift>>(
      `/api/v1/stations/${encodeURIComponent(stationID)}/shifts${qs}`,
      { signal },
    );
  }

  getShift(id: string, signal?: AbortSignal): Promise<ShiftDetail> {
    return this.request<ShiftDetail>(`/api/v1/shifts/${encodeURIComponent(id)}`, { signal });
  }

  openShift(
    stationID: string,
    req: { operating_day_id: string; name: string; slot: 'morning' | 'evening'; notes?: string },
    signal?: AbortSignal,
  ): Promise<Shift> {
    return this.request<Shift>(`/api/v1/stations/${encodeURIComponent(stationID)}/shifts`, {
      method: 'POST',
      body: req,
      signal,
    });
  }

  assignAttendant(shiftID: string, userID: string, signal?: AbortSignal): Promise<void> {
    return this.request<void>(`/api/v1/shifts/${encodeURIComponent(shiftID)}/attendants`, {
      method: 'POST',
      body: { user_id: userID },
      signal,
    });
  }

  unassignAttendant(shiftID: string, userID: string, signal?: AbortSignal): Promise<void> {
    return this.request<void>(
      `/api/v1/shifts/${encodeURIComponent(shiftID)}/attendants/${encodeURIComponent(userID)}`,
      { method: 'DELETE', signal },
    );
  }

  assignNozzle(
    shiftID: string,
    req: { nozzle_id: string; attendant_id: string },
    signal?: AbortSignal,
  ): Promise<NozzleAssignment> {
    return this.request<NozzleAssignment>(
      `/api/v1/shifts/${encodeURIComponent(shiftID)}/nozzle-assignments`,
      { method: 'POST', body: req, signal },
    );
  }

  unassignNozzle(shiftID: string, assignmentID: string, signal?: AbortSignal): Promise<void> {
    return this.request<void>(
      `/api/v1/shifts/${encodeURIComponent(shiftID)}/nozzle-assignments/${encodeURIComponent(assignmentID)}`,
      { method: 'DELETE', signal },
    );
  }

  // ----------- Workforce (Phase 11) -----------

  /** A station's employees (station.read). */
  listEmployees(stationID: string, signal?: AbortSignal): Promise<WorkforceList<Employee>> {
    return this.request<WorkforceList<Employee>>(
      `/api/v1/stations/${encodeURIComponent(stationID)}/employees`,
      { signal },
    );
  }

  /** Add an employee to a station (station.manage). */
  createEmployee(
    stationID: string,
    req: {
      full_name: string;
      role?: EmployeeRole;
      user_id?: string;
      employee_code?: string;
      phone?: string;
      email?: string;
    },
    signal?: AbortSignal,
  ): Promise<Employee> {
    return this.request<Employee>(`/api/v1/stations/${encodeURIComponent(stationID)}/employees`, {
      method: 'POST',
      body: req,
      signal,
    });
  }

  /** Update an employee (station.manage). */
  updateEmployee(
    employeeID: string,
    req: {
      full_name?: string;
      role?: EmployeeRole;
      status?: 'active' | 'inactive';
      user_id?: string;
      employee_code?: string;
      phone?: string;
      email?: string;
    },
    signal?: AbortSignal,
  ): Promise<Employee> {
    return this.request<Employee>(`/api/v1/employees/${encodeURIComponent(employeeID)}`, {
      method: 'PATCH',
      body: req,
      signal,
    });
  }

  /** A station's three shift teams (station.read). */
  listTeams(stationID: string, signal?: AbortSignal): Promise<WorkforceList<ShiftTeam>> {
    return this.request<WorkforceList<ShiftTeam>>(
      `/api/v1/stations/${encodeURIComponent(stationID)}/teams`,
      { signal },
    );
  }

  /** Ensure the station's three rotation teams exist (station.manage). */
  ensureTeams(
    stationID: string,
    req: { names?: string[] } = {},
    signal?: AbortSignal,
  ): Promise<WorkforceList<ShiftTeam>> {
    return this.request<WorkforceList<ShiftTeam>>(
      `/api/v1/stations/${encodeURIComponent(stationID)}/teams`,
      { method: 'POST', body: req, signal },
    );
  }

  /** Replace a team's membership (station.manage). */
  setTeamMembers(
    teamID: string,
    employeeIDs: string[],
    signal?: AbortSignal,
  ): Promise<WorkforceList<Employee>> {
    return this.request<WorkforceList<Employee>>(
      `/api/v1/teams/${encodeURIComponent(teamID)}/members`,
      { method: 'PUT', body: { employee_ids: employeeIDs }, signal },
    );
  }

  /** The station's rotation anchor date (station.read). */
  getRotationAnchor(stationID: string, signal?: AbortSignal): Promise<RotationAnchor> {
    return this.request<RotationAnchor>(
      `/api/v1/stations/${encodeURIComponent(stationID)}/rotation-anchor`,
      { signal },
    );
  }

  /** Set or clear the station's rotation anchor date (station.manage). */
  setRotationAnchor(
    stationID: string,
    rotationAnchorDate: string | null,
    signal?: AbortSignal,
  ): Promise<RotationAnchor> {
    return this.request<RotationAnchor>(
      `/api/v1/stations/${encodeURIComponent(stationID)}/rotation-anchor`,
      { method: 'PUT', body: { rotation_anchor_date: rotationAnchorDate }, signal },
    );
  }

  /** Forward-looking rotation roster (station.read). */
  getRoster(
    stationID: string,
    opts: { from?: string; days?: number } = {},
    signal?: AbortSignal,
  ): Promise<WorkforceList<DayRoster>> {
    const qs = new URLSearchParams();
    if (opts.from) qs.set('from', opts.from);
    if (opts.days != null) qs.set('days', String(opts.days));
    const suffix = qs.toString() ? `?${qs.toString()}` : '';
    return this.request<WorkforceList<DayRoster>>(
      `/api/v1/stations/${encodeURIComponent(stationID)}/roster${suffix}`,
      { signal },
    );
  }

  /** The team on duty for a date + slot (station.read). */
  getScheduledTeam(
    stationID: string,
    opts: { slot: 'morning' | 'evening'; date?: string },
    signal?: AbortSignal,
  ): Promise<ScheduledTeam> {
    const qs = new URLSearchParams({ slot: opts.slot });
    if (opts.date) qs.set('date', opts.date);
    return this.request<ScheduledTeam>(
      `/api/v1/stations/${encodeURIComponent(stationID)}/scheduled-team?${qs.toString()}`,
      { signal },
    );
  }

  // ----------- Meter readings -----------

  listMeterReadings(shiftID: string, signal?: AbortSignal): Promise<MeterReadingList> {
    return this.request<MeterReadingList>(
      `/api/v1/shifts/${encodeURIComponent(shiftID)}/meter-readings`,
      { signal },
    );
  }

  captureMeterReading(
    shiftID: string,
    req: { nozzle_id: string; reading_type: 'opening' | 'closing'; reading: string },
    signal?: AbortSignal,
  ): Promise<MeterReading> {
    return this.request<MeterReading>(
      `/api/v1/shifts/${encodeURIComponent(shiftID)}/meter-readings`,
      { method: 'POST', body: req, signal },
    );
  }

  correctMeterReading(
    shiftID: string,
    readingID: string,
    reading: string,
    signal?: AbortSignal,
  ): Promise<MeterReading> {
    return this.request<MeterReading>(
      `/api/v1/shifts/${encodeURIComponent(shiftID)}/meter-readings/${encodeURIComponent(readingID)}/correct`,
      { method: 'POST', body: { reading }, signal },
    );
  }

  // ----------- Tank dip readings -----------

  listDipReadings(shiftID: string, signal?: AbortSignal): Promise<Paginated<DipReading>> {
    return this.request<Paginated<DipReading>>(
      `/api/v1/shifts/${encodeURIComponent(shiftID)}/dip-readings`,
      { signal },
    );
  }

  captureDipReading(
    shiftID: string,
    req: {
      tank_id: string;
      reading_type: 'opening' | 'closing';
      dip_mm: string;
      water_mm?: number;
      temperature_c?: number;
    },
    signal?: AbortSignal,
  ): Promise<DipReading> {
    return this.request<DipReading>(`/api/v1/shifts/${encodeURIComponent(shiftID)}/dip-readings`, {
      method: 'POST',
      body: req,
      signal,
    });
  }

  correctDipReading(
    shiftID: string,
    readingID: string,
    req: { dip_mm: string; water_mm?: number; temperature_c?: number },
    signal?: AbortSignal,
  ): Promise<DipReading> {
    return this.request<DipReading>(
      `/api/v1/shifts/${encodeURIComponent(shiftID)}/dip-readings/${encodeURIComponent(readingID)}/correct`,
      { method: 'POST', body: req, signal },
    );
  }

  // ----------- Shift close & cash -----------

  closeShift(shiftID: string, signal?: AbortSignal): Promise<ShiftCloseSummary> {
    return this.request<ShiftCloseSummary>(`/api/v1/shifts/${encodeURIComponent(shiftID)}/close`, {
      method: 'POST',
      signal,
    });
  }

  getCloseSummary(shiftID: string, signal?: AbortSignal): Promise<ShiftCloseSummary> {
    return this.request<ShiftCloseSummary>(
      `/api/v1/shifts/${encodeURIComponent(shiftID)}/close-summary`,
      { signal },
    );
  }

  submitCash(
    shiftID: string,
    req: {
      cash_amount: string;
      mobile_money_amount?: string;
      card_amount?: string;
      credit_amount?: string;
      notes?: string;
    },
    signal?: AbortSignal,
  ): Promise<CashSubmission> {
    return this.request<CashSubmission>(
      `/api/v1/shifts/${encodeURIComponent(shiftID)}/cash-submission`,
      { method: 'POST', body: req, signal },
    );
  }

  // ----------- Shift approval & exceptions -----------

  approveShift(shiftID: string, signal?: AbortSignal): Promise<Shift> {
    return this.request<Shift>(`/api/v1/shifts/${encodeURIComponent(shiftID)}/status`, {
      method: 'PATCH',
      body: { status: 'approved' },
      signal,
    });
  }

  listShiftExceptions(shiftID: string, signal?: AbortSignal): Promise<Paginated<ShiftException>> {
    return this.request<Paginated<ShiftException>>(
      `/api/v1/shifts/${encodeURIComponent(shiftID)}/exceptions`,
      { signal },
    );
  }

  resolveShiftException(exceptionID: string, signal?: AbortSignal): Promise<ShiftException> {
    return this.request<ShiftException>(
      `/api/v1/shift-exceptions/${encodeURIComponent(exceptionID)}/status`,
      { method: 'PATCH', body: { status: 'resolved' }, signal },
    );
  }

  // ----------- Inventory & reconciliation -----------

  /** Per-tank book vs physical, fill, days-of-stock, variance trend (Stage 7). */
  getInventoryOverview(stationID: string, signal?: AbortSignal): Promise<InventoryOverview> {
    return this.request<InventoryOverview>(
      `/api/v1/stations/${encodeURIComponent(stationID)}/inventory-overview`,
      { signal },
    );
  }

  /** The day's per-tank reconciliations for the review console (Stage 8). */
  getReconciliationOverview(
    stationID: string,
    opts: { operatingDayID?: string } = {},
    signal?: AbortSignal,
  ): Promise<ReconciliationOverview> {
    const qs = opts.operatingDayID
      ? `?operating_day_id=${encodeURIComponent(opts.operatingDayID)}`
      : '';
    return this.request<ReconciliationOverview>(
      `/api/v1/stations/${encodeURIComponent(stationID)}/reconciliation-overview${qs}`,
      { signal },
    );
  }

  /** Compute + persist a draft reconciliation for a tank/day. */
  runReconciliation(
    tankID: string,
    operatingDayID: string,
    signal?: AbortSignal,
  ): Promise<Reconciliation> {
    return this.request<Reconciliation>(
      `/api/v1/tanks/${encodeURIComponent(tankID)}/reconciliations`,
      { method: 'POST', body: { operating_day_id: operatingDayID }, signal },
    );
  }

  /** Record a reasoned stock adjustment and recompute the draft. */
  adjustReconciliation(
    reconciliationID: string,
    req: { litres: number; reason: string },
    signal?: AbortSignal,
  ): Promise<Reconciliation> {
    return this.request<Reconciliation>(
      `/api/v1/reconciliations/${encodeURIComponent(reconciliationID)}/adjustments`,
      { method: 'POST', body: req, signal },
    );
  }

  /** Seal a reconciliation, freezing it and carrying physical forward. */
  sealReconciliation(reconciliationID: string, signal?: AbortSignal): Promise<Reconciliation> {
    return this.request<Reconciliation>(
      `/api/v1/reconciliations/${encodeURIComponent(reconciliationID)}/seal`,
      { method: 'POST', signal },
    );
  }

  // ----------- Pricing (Phase 6) -----------

  setPrice(
    stationID: string,
    req: {
      product_id: string;
      unit_price: string;
      effective_from?: string;
      reason?: string;
      allow_below_cost?: boolean;
    },
    signal?: AbortSignal,
  ): Promise<PriceChange> {
    return this.request<PriceChange>(`/api/v1/stations/${encodeURIComponent(stationID)}/prices`, {
      method: 'POST',
      body: req,
      signal,
    });
  }

  getPriceBoard(stationID: string, signal?: AbortSignal): Promise<Paginated<PriceBoardEntry>> {
    return this.request<Paginated<PriceBoardEntry>>(
      `/api/v1/stations/${encodeURIComponent(stationID)}/price-board`,
      { signal },
    );
  }

  getPriceHistory(
    stationID: string,
    productID: string,
    signal?: AbortSignal,
  ): Promise<Paginated<PriceChange>> {
    return this.request<Paginated<PriceChange>>(
      `/api/v1/stations/${encodeURIComponent(stationID)}/price-history?product_id=${encodeURIComponent(productID)}`,
      { signal },
    );
  }

  // ----------- Recognized sales & valuation (Phase 6) -----------

  listShiftSales(shiftID: string, signal?: AbortSignal): Promise<Paginated<Sale>> {
    return this.request<Paginated<Sale>>(`/api/v1/shifts/${encodeURIComponent(shiftID)}/sales`, {
      signal,
    });
  }

  listStationSales(
    stationID: string,
    operatingDayID: string,
    signal?: AbortSignal,
  ): Promise<Paginated<Sale>> {
    return this.request<Paginated<Sale>>(
      `/api/v1/stations/${encodeURIComponent(stationID)}/sales?operating_day_id=${encodeURIComponent(operatingDayID)}`,
      { signal },
    );
  }

  getInventoryValuation(
    stationID: string,
    signal?: AbortSignal,
  ): Promise<Paginated<TankValuation>> {
    return this.request<Paginated<TankValuation>>(
      `/api/v1/stations/${encodeURIComponent(stationID)}/inventory-valuation`,
      { signal },
    );
  }

  // ----------- Tender & receivables (Phase 6) -----------

  recordPayment(
    shiftID: string,
    req: {
      tender_type: string;
      amount: string;
      reference?: string;
      customer_id?: string;
      allow_over_limit?: boolean;
      notes?: string;
    },
    signal?: AbortSignal,
  ): Promise<Payment> {
    return this.request<Payment>(`/api/v1/shifts/${encodeURIComponent(shiftID)}/payments`, {
      method: 'POST',
      body: req,
      signal,
    });
  }

  listShiftPayments(shiftID: string, signal?: AbortSignal): Promise<Paginated<Payment>> {
    return this.request<Paginated<Payment>>(
      `/api/v1/shifts/${encodeURIComponent(shiftID)}/payments`,
      { signal },
    );
  }

  getShiftPaymentReconciliation(
    shiftID: string,
    signal?: AbortSignal,
  ): Promise<ShiftPaymentReconciliation> {
    return this.request<ShiftPaymentReconciliation>(
      `/api/v1/shifts/${encodeURIComponent(shiftID)}/payment-reconciliation`,
      { signal },
    );
  }

  listCustomers(signal?: AbortSignal): Promise<Paginated<Customer>> {
    return this.request<Paginated<Customer>>('/api/v1/customers', { signal });
  }

  createCustomer(
    req: {
      code: string;
      name: string;
      contact_name?: string;
      contact_phone?: string;
      contact_email?: string;
      credit_limit?: string;
    },
    signal?: AbortSignal,
  ): Promise<Customer> {
    return this.request<Customer>('/api/v1/customers', { method: 'POST', body: req, signal });
  }

  updateCustomer(id: string, req: Partial<Customer>, signal?: AbortSignal): Promise<Customer> {
    return this.request<Customer>(`/api/v1/customers/${encodeURIComponent(id)}`, {
      method: 'PATCH',
      body: req,
      signal,
    });
  }

  getCustomerStatement(id: string, signal?: AbortSignal): Promise<CustomerStatement> {
    return this.request<CustomerStatement>(
      `/api/v1/customers/${encodeURIComponent(id)}/statement`,
      { signal },
    );
  }

  recordCustomerPayment(
    id: string,
    req: { amount: string; reference?: string; notes?: string },
    signal?: AbortSignal,
  ): Promise<ARentry> {
    return this.request<ARentry>(`/api/v1/customers/${encodeURIComponent(id)}/payments`, {
      method: 'POST',
      body: req,
      signal,
    });
  }

  // ----------- Customer credit & fleet (Phase 8) -----------

  setCustomerStatus(id: string, status: string, signal?: AbortSignal): Promise<Customer> {
    return this.request<Customer>(`/api/v1/customers/${encodeURIComponent(id)}/status`, {
      method: 'POST',
      body: { status },
      signal,
    });
  }

  listCustomerContacts(id: string, signal?: AbortSignal): Promise<Paginated<CustomerContact>> {
    return this.request<Paginated<CustomerContact>>(
      `/api/v1/customers/${encodeURIComponent(id)}/contacts`,
      { signal },
    );
  }

  createCustomerContact(
    id: string,
    req: {
      name: string;
      role?: string;
      email?: string;
      phone?: string;
      statement_preference?: string;
      notification_preference?: string;
    },
    signal?: AbortSignal,
  ): Promise<{ id: string }> {
    return this.request(`/api/v1/customers/${encodeURIComponent(id)}/contacts`, {
      method: 'POST',
      body: req,
      signal,
    });
  }

  deleteCustomerContact(id: string, contactID: string, signal?: AbortSignal): Promise<void> {
    return this.request<void>(
      `/api/v1/customers/${encodeURIComponent(id)}/contacts/${encodeURIComponent(contactID)}`,
      { method: 'DELETE', signal },
    );
  }

  getCreditProfile(id: string, signal?: AbortSignal): Promise<CreditProfile> {
    return this.request<CreditProfile>(
      `/api/v1/customers/${encodeURIComponent(id)}/credit-profile`,
      {
        signal,
      },
    );
  }

  upsertCreditProfile(
    id: string,
    req: {
      payment_terms_days?: number;
      grace_days?: number;
      statement_cycle_days?: number;
      risk_category?: string;
      warning_threshold_pct?: string;
      review_date?: string;
    },
    signal?: AbortSignal,
  ): Promise<CreditProfile> {
    return this.request<CreditProfile>(
      `/api/v1/customers/${encodeURIComponent(id)}/credit-profile`,
      {
        method: 'PUT',
        body: req,
        signal,
      },
    );
  }

  getCreditPosition(id: string, signal?: AbortSignal): Promise<CreditPosition> {
    return this.request<CreditPosition>(
      `/api/v1/customers/${encodeURIComponent(id)}/credit-position`,
      { signal },
    );
  }

  setCreditHold(
    id: string,
    req: { hold: boolean; reason?: string },
    signal?: AbortSignal,
  ): Promise<{ customer_id: string; hold: boolean }> {
    return this.request(`/api/v1/customers/${encodeURIComponent(id)}/credit-hold`, {
      method: 'POST',
      body: req,
      signal,
    });
  }

  listCustomerPriceAgreements(
    opts: { customerID?: string } = {},
    signal?: AbortSignal,
  ): Promise<Paginated<CustomerPriceAgreement>> {
    const qs = opts.customerID ? `?customer_id=${encodeURIComponent(opts.customerID)}` : '';
    return this.request<Paginated<CustomerPriceAgreement>>(
      `/api/v1/customer-price-agreements${qs}`,
      { signal },
    );
  }

  createCustomerPriceAgreement(
    req: {
      customer_id: string;
      product_id: string;
      station_id?: string;
      price_type: 'fixed' | 'discount' | 'markup';
      fixed_price?: string;
      discount?: string;
      markup?: string;
      effective_from?: string;
      effective_to?: string;
    },
    signal?: AbortSignal,
  ): Promise<CustomerPriceAgreement> {
    return this.request<CustomerPriceAgreement>('/api/v1/customer-price-agreements', {
      method: 'POST',
      body: req,
      signal,
    });
  }

  transitionCustomerPriceAgreement(
    id: string,
    action: 'approve' | 'activate' | 'cancel',
    signal?: AbortSignal,
  ): Promise<CustomerPriceAgreement> {
    return this.request<CustomerPriceAgreement>(
      `/api/v1/customer-price-agreements/${encodeURIComponent(id)}/${action}`,
      { method: 'POST', signal },
    );
  }

  // ----------- Fleet identity (Phase 8) -----------

  listVehicles(
    opts: { customerID?: string } = {},
    signal?: AbortSignal,
  ): Promise<Paginated<Vehicle>> {
    const qs = opts.customerID ? `?customer_id=${encodeURIComponent(opts.customerID)}` : '';
    return this.request<Paginated<Vehicle>>(`/api/v1/fleet/vehicles${qs}`, { signal });
  }

  createVehicle(
    req: {
      customer_id: string;
      registration: string;
      fleet_number?: string;
      vin?: string;
      vehicle_type?: string;
      default_product_id?: string;
      tank_capacity?: string;
      odometer_required?: boolean;
    },
    signal?: AbortSignal,
  ): Promise<Vehicle> {
    return this.request<Vehicle>('/api/v1/fleet/vehicles', { method: 'POST', body: req, signal });
  }

  setVehicleStatus(id: string, status: string, signal?: AbortSignal): Promise<Vehicle> {
    return this.request<Vehicle>(`/api/v1/fleet/vehicles/${encodeURIComponent(id)}/status`, {
      method: 'POST',
      body: { status },
      signal,
    });
  }

  listDrivers(
    opts: { customerID?: string } = {},
    signal?: AbortSignal,
  ): Promise<Paginated<Driver>> {
    const qs = opts.customerID ? `?customer_id=${encodeURIComponent(opts.customerID)}` : '';
    return this.request<Paginated<Driver>>(`/api/v1/fleet/drivers${qs}`, { signal });
  }

  createDriver(
    req: {
      customer_id: string;
      name: string;
      phone?: string;
      license_number?: string;
      pin?: string;
      allowed_product_ids?: string[];
      assignment_rule?: 'any' | 'assigned' | 'primary';
    },
    signal?: AbortSignal,
  ): Promise<Driver> {
    return this.request<Driver>('/api/v1/fleet/drivers', { method: 'POST', body: req, signal });
  }

  setDriverStatus(id: string, status: string, signal?: AbortSignal): Promise<Driver> {
    return this.request<Driver>(`/api/v1/fleet/drivers/${encodeURIComponent(id)}/status`, {
      method: 'POST',
      body: { status },
      signal,
    });
  }

  resetDriverPIN(
    id: string,
    pin: string,
    signal?: AbortSignal,
  ): Promise<{ driver_id: string; pin_set: boolean }> {
    return this.request(`/api/v1/fleet/drivers/${encodeURIComponent(id)}/reset-pin`, {
      method: 'POST',
      body: { pin },
      signal,
    });
  }

  listFuelCredentials(
    opts: { customerID?: string } = {},
    signal?: AbortSignal,
  ): Promise<Paginated<FuelCredential>> {
    const qs = opts.customerID ? `?customer_id=${encodeURIComponent(opts.customerID)}` : '';
    return this.request<Paginated<FuelCredential>>(`/api/v1/fleet/credentials${qs}`, { signal });
  }

  issueFuelCredential(
    req: {
      customer_id: string;
      vehicle_id?: string;
      driver_id?: string;
      credential_type: 'card' | 'qr' | 'rfid' | 'manual_code';
      token: string;
      expiry_date?: string;
    },
    signal?: AbortSignal,
  ): Promise<FuelCredential> {
    return this.request<FuelCredential>('/api/v1/fleet/credentials', {
      method: 'POST',
      body: req,
      signal,
    });
  }

  setFuelCredentialStatus(
    id: string,
    status: string,
    signal?: AbortSignal,
  ): Promise<FuelCredential> {
    return this.request<FuelCredential>(
      `/api/v1/fleet/credentials/${encodeURIComponent(id)}/status`,
      {
        method: 'POST',
        body: { status },
        signal,
      },
    );
  }

  validateFuelCredential(token: string, signal?: AbortSignal): Promise<CredentialValidation> {
    return this.request<CredentialValidation>('/api/v1/fleet/credentials/validate', {
      method: 'POST',
      body: { token },
      signal,
    });
  }

  // ----------- Fuel authorization & limits (Phase 8) -----------

  /**
   * Request a fuel authorization. On denial the server returns HTTP 422 with a
   * rule_code body, surfaced here as an SdkError whose body is AuthorizationDenied.
   */
  requestFuelAuthorization(
    req: {
      customer_id: string;
      vehicle_id?: string;
      driver_id?: string;
      credential_id?: string;
      station_id: string;
      product_id?: string;
      requested_amount: string;
      odometer?: string;
      source?: string;
      override?: boolean;
    },
    signal?: AbortSignal,
  ): Promise<FuelAuthorization> {
    return this.request<FuelAuthorization>('/api/v1/fuel-authorizations', {
      method: 'POST',
      body: req,
      signal,
    });
  }

  listFuelAuthorizations(
    opts: { customerID?: string } = {},
    signal?: AbortSignal,
  ): Promise<Paginated<FuelAuthorization>> {
    const qs = opts.customerID ? `?customer_id=${encodeURIComponent(opts.customerID)}` : '';
    return this.request<Paginated<FuelAuthorization>>(`/api/v1/fuel-authorizations${qs}`, {
      signal,
    });
  }

  getFuelAuthorization(id: string, signal?: AbortSignal): Promise<FuelAuthorization> {
    return this.request<FuelAuthorization>(
      `/api/v1/fuel-authorizations/${encodeURIComponent(id)}`,
      {
        signal,
      },
    );
  }

  fulfillFuelAuthorization(
    id: string,
    consumedBy: string,
    signal?: AbortSignal,
  ): Promise<FuelAuthorization> {
    return this.request<FuelAuthorization>(
      `/api/v1/fuel-authorizations/${encodeURIComponent(id)}/fulfill`,
      { method: 'POST', body: { consumed_by: consumedBy }, signal },
    );
  }

  transitionFuelAuthorization(
    id: string,
    action: 'cancel' | 'void',
    signal?: AbortSignal,
  ): Promise<FuelAuthorization> {
    return this.request<FuelAuthorization>(
      `/api/v1/fuel-authorizations/${encodeURIComponent(id)}/${action}`,
      { method: 'POST', signal },
    );
  }

  listFuelLimits(
    opts: { customerID?: string } = {},
    signal?: AbortSignal,
  ): Promise<{ items: unknown[]; count: number }> {
    const qs = opts.customerID ? `?customer_id=${encodeURIComponent(opts.customerID)}` : '';
    return this.request(`/api/v1/fuel-limits${qs}`, { signal });
  }

  createFuelLimit(
    req: {
      customer_id?: string;
      vehicle_id?: string;
      product_id?: string;
      scope?: string;
      period?: 'transaction' | 'day' | 'week' | 'month';
      max_amount?: string;
      max_litres?: string;
    },
    signal?: AbortSignal,
  ): Promise<{ id: string }> {
    return this.request('/api/v1/fuel-limits', { method: 'POST', body: req, signal });
  }

  // ----------- Odometer & fleet consumption (Phase 8) -----------

  listOdometerReadings(
    vehicleID: string,
    signal?: AbortSignal,
  ): Promise<Paginated<OdometerReading>> {
    return this.request<Paginated<OdometerReading>>(
      `/api/v1/fleet/vehicles/${encodeURIComponent(vehicleID)}/odometer`,
      { signal },
    );
  }

  recordOdometer(
    vehicleID: string,
    req: {
      reading: string;
      authorization_id?: string;
      station_id?: string;
      note?: string;
      override?: boolean;
    },
    signal?: AbortSignal,
  ): Promise<OdometerReading> {
    return this.request<OdometerReading>(
      `/api/v1/fleet/vehicles/${encodeURIComponent(vehicleID)}/odometer`,
      { method: 'POST', body: req, signal },
    );
  }

  getFleetConsumption(
    customerID: string,
    opts: { from?: string; to?: string } = {},
    signal?: AbortSignal,
  ): Promise<{ from: string; to: string; items: VehicleConsumption[]; count: number }> {
    const qs = new URLSearchParams({ customer_id: customerID });
    if (opts.from) qs.set('from', opts.from);
    if (opts.to) qs.set('to', opts.to);
    return this.request(`/api/v1/fleet/consumption?${qs.toString()}`, { signal });
  }

  // ----------- Customer statements & credit alerts (Phase 8) -----------

  listCreditStatements(
    customerID: string,
    signal?: AbortSignal,
  ): Promise<Paginated<CreditStatement>> {
    return this.request<Paginated<CreditStatement>>(
      `/api/v1/customers/${encodeURIComponent(customerID)}/statements`,
      { signal },
    );
  }

  generateCreditStatement(
    customerID: string,
    req: { period_start: string; period_end: string },
    signal?: AbortSignal,
  ): Promise<CreditStatement> {
    return this.request<CreditStatement>(
      `/api/v1/customers/${encodeURIComponent(customerID)}/statements`,
      { method: 'POST', body: req, signal },
    );
  }

  issueCreditStatement(id: string, signal?: AbortSignal): Promise<CreditStatement> {
    return this.request<CreditStatement>(
      `/api/v1/customer-statements/${encodeURIComponent(id)}/issue`,
      { method: 'POST', signal },
    );
  }

  scanCreditAlerts(signal?: AbortSignal): Promise<{ created: number }> {
    return this.request('/api/v1/credit-alerts/scan', { method: 'POST', signal });
  }

  listCreditAlerts(
    opts: { status?: string } = {},
    signal?: AbortSignal,
  ): Promise<Paginated<CreditAlert>> {
    const qs = opts.status ? `?status=${encodeURIComponent(opts.status)}` : '';
    return this.request<Paginated<CreditAlert>>(`/api/v1/credit-alerts${qs}`, { signal });
  }

  transitionCreditAlert(
    id: string,
    action: 'acknowledge' | 'resolve' | 'dismiss',
    req: { reason?: string } = {},
    signal?: AbortSignal,
  ): Promise<{ id: string; status: string }> {
    return this.request(`/api/v1/credit-alerts/${encodeURIComponent(id)}/${action}`, {
      method: 'POST',
      body: req,
      signal,
    });
  }

  // ----------- Revenue close & dashboard (Phase 6) -----------

  computeRevenueDay(
    stationID: string,
    operatingDayID: string,
    signal?: AbortSignal,
  ): Promise<RevenueDay> {
    return this.request<RevenueDay>(
      `/api/v1/stations/${encodeURIComponent(stationID)}/revenue-days`,
      { method: 'POST', body: { operating_day_id: operatingDayID }, signal },
    );
  }

  getRevenueOverview(stationID: string, signal?: AbortSignal): Promise<RevenueOverview> {
    return this.request<RevenueOverview>(
      `/api/v1/stations/${encodeURIComponent(stationID)}/revenue-overview`,
      { signal },
    );
  }

  lockRevenueDay(revenueDayID: string, signal?: AbortSignal): Promise<RevenueDay> {
    return this.request<RevenueDay>(
      `/api/v1/revenue-days/${encodeURIComponent(revenueDayID)}/lock`,
      { method: 'POST', signal },
    );
  }

  getARaging(signal?: AbortSignal): Promise<Paginated<CustomerBalance>> {
    return this.request<Paginated<CustomerBalance>>('/api/v1/ar-aging', { signal });
  }

  // ----------- Accounting (Phase 7) -----------

  listAccounts(signal?: AbortSignal): Promise<Paginated<Account>> {
    return this.request<Paginated<Account>>('/api/v1/accounts', { signal });
  }

  seedDefaultChart(signal?: AbortSignal): Promise<{ created: number }> {
    return this.request('/api/v1/accounts/seed-defaults', { method: 'POST', signal });
  }

  createAccount(
    req: { code: string; name: string; type: string; normal_balance: string; parent_id?: string },
    signal?: AbortSignal,
  ): Promise<Account> {
    return this.request<Account>('/api/v1/accounts', { method: 'POST', body: req, signal });
  }

  updateAccount(
    id: string,
    req: { name?: string; status?: string },
    signal?: AbortSignal,
  ): Promise<Account> {
    return this.request<Account>(`/api/v1/accounts/${encodeURIComponent(id)}`, {
      method: 'PATCH',
      body: req,
      signal,
    });
  }

  listAccountingPeriods(signal?: AbortSignal): Promise<Paginated<AccountingPeriod>> {
    return this.request<Paginated<AccountingPeriod>>('/api/v1/accounting-periods', { signal });
  }

  createAccountingPeriod(
    req: { start_date: string; end_date: string },
    signal?: AbortSignal,
  ): Promise<AccountingPeriod> {
    return this.request<AccountingPeriod>('/api/v1/accounting-periods', {
      method: 'POST',
      body: req,
      signal,
    });
  }

  transitionAccountingPeriod(
    id: string,
    action: 'start-close' | 'close' | 'reopen' | 'lock',
    signal?: AbortSignal,
  ): Promise<AccountingPeriod> {
    return this.request<AccountingPeriod>(
      `/api/v1/accounting-periods/${encodeURIComponent(id)}/${action}`,
      { method: 'POST', signal },
    );
  }

  listJournalEntries(signal?: AbortSignal): Promise<Paginated<JournalEntry>> {
    return this.request<Paginated<JournalEntry>>('/api/v1/journal-entries', { signal });
  }

  getJournalEntry(id: string, signal?: AbortSignal): Promise<JournalEntry> {
    return this.request<JournalEntry>(`/api/v1/journal-entries/${encodeURIComponent(id)}`, {
      signal,
    });
  }

  postJournalAdjustment(
    req: {
      entry_date: string;
      memo?: string;
      lines: {
        account_id?: string;
        system_key?: string;
        debit: string;
        credit: string;
        memo?: string;
      }[];
    },
    signal?: AbortSignal,
  ): Promise<JournalEntry> {
    return this.request<JournalEntry>('/api/v1/journal-entries', {
      method: 'POST',
      body: req,
      signal,
    });
  }

  reverseJournalEntry(id: string, memo?: string, signal?: AbortSignal): Promise<JournalEntry> {
    return this.request<JournalEntry>(`/api/v1/journal-entries/${encodeURIComponent(id)}/reverse`, {
      method: 'POST',
      body: { memo },
      signal,
    });
  }

  // ----------- Payables & supplier payments (Phase 7) -----------

  listPayables(signal?: AbortSignal): Promise<Paginated<Payable>> {
    return this.request<Paginated<Payable>>('/api/v1/payables', { signal });
  }

  importPayables(signal?: AbortSignal): Promise<{ imported: number }> {
    return this.request('/api/v1/payables/import', { method: 'POST', signal });
  }

  getApAging(signal?: AbortSignal): Promise<Paginated<SupplierAging>> {
    return this.request<Paginated<SupplierAging>>('/api/v1/ap-aging', { signal });
  }

  recordSupplierPayment(
    req: {
      supplier_id: string;
      payment_date: string;
      method: string;
      reference?: string;
      source_account_key?: string;
      allocations: { payable_id: string; amount: string }[];
    },
    signal?: AbortSignal,
  ): Promise<{ payment_id: string; journal_entry_id: string }> {
    return this.request('/api/v1/supplier-payments', { method: 'POST', body: req, signal });
  }

  // ----------- Finance reports & dashboard (Phase 7) -----------

  /** One-call finance dashboard: balance sheet, P&L, AP count, open periods, recent entries. */
  getFinanceOverview(signal?: AbortSignal): Promise<FinanceOverview> {
    return this.request<FinanceOverview>('/api/v1/finance/overview', { signal });
  }

  getTrialBalance(asOf?: string, signal?: AbortSignal): Promise<TrialBalance> {
    const qs = asOf ? `?as_of=${encodeURIComponent(asOf)}` : '';
    return this.request<TrialBalance>(`/api/v1/finance/reports/trial-balance${qs}`, { signal });
  }

  getIncomeStatement(from?: string, to?: string, signal?: AbortSignal): Promise<IncomeStatement> {
    const qs = new URLSearchParams();
    if (from) qs.set('from', from);
    if (to) qs.set('to', to);
    const q = qs.toString();
    return this.request<IncomeStatement>(`/api/v1/finance/reports/profit-loss${q ? `?${q}` : ''}`, {
      signal,
    });
  }

  getBalanceSheet(asOf?: string, signal?: AbortSignal): Promise<BalanceSheet> {
    const qs = asOf ? `?as_of=${encodeURIComponent(asOf)}` : '';
    return this.request<BalanceSheet>(`/api/v1/finance/reports/balance-sheet${qs}`, { signal });
  }

  getGeneralLedger(
    accountID: string,
    signal?: AbortSignal,
  ): Promise<{ items: GeneralLedgerRow[]; count: number }> {
    return this.request(
      `/api/v1/finance/reports/general-ledger?account_id=${encodeURIComponent(accountID)}`,
      { signal },
    );
  }

  /** The period-close checklist: blocker counts and whether a close is clear. */
  getCloseChecklist(signal?: AbortSignal): Promise<CloseChecklist> {
    return this.request<CloseChecklist>('/api/v1/finance/close-checklist', { signal });
  }

  // ----------- Accounting exports (Phase 7) -----------

  generateExport(
    type: 'journal-entries' | 'trial-balance' | 'ap-aging' | 'ar-aging',
    opts: { from?: string; to?: string; asOf?: string } = {},
    signal?: AbortSignal,
  ): Promise<AccountingExportResult> {
    const qs = new URLSearchParams();
    if (opts.from) qs.set('from', opts.from);
    if (opts.to) qs.set('to', opts.to);
    if (opts.asOf) qs.set('as_of', opts.asOf);
    const q = qs.toString();
    return this.request<AccountingExportResult>(
      `/api/v1/finance/exports/${encodeURIComponent(type)}${q ? `?${q}` : ''}`,
      { method: 'POST', signal },
    );
  }

  listAccountingExports(signal?: AbortSignal): Promise<Paginated<AccountingExport>> {
    return this.request<Paginated<AccountingExport>>('/api/v1/finance/exports', { signal });
  }

  // ----------- Standard report exports (CSV) -----------

  /**
   * Build the same-origin URL for a standard CSV report. The endpoints stream
   * `text/csv` with a Content-Disposition attachment and are gated by the
   * matching read permission; each export is recorded in the audit log. Callers
   * fetch this URL (credentials: 'same-origin' through the BFF) and hand the
   * resulting blob to a browser download — JSON parsing does not apply.
   */
  reportUrl(report: ReportSpec): string {
    switch (report.kind) {
      case 'revenue':
        return `${this.baseURL}/api/v1/stations/${encodeURIComponent(report.stationID)}/reports/revenue.csv`;
      case 'inventory':
        return `${this.baseURL}/api/v1/stations/${encodeURIComponent(report.stationID)}/reports/inventory.csv`;
      case 'reconciliation': {
        const qs = report.operatingDayID
          ? `?operating_day_id=${encodeURIComponent(report.operatingDayID)}`
          : '';
        return `${this.baseURL}/api/v1/stations/${encodeURIComponent(report.stationID)}/reports/reconciliation.csv${qs}`;
      }
      case 'financials': {
        const qs = report.period ? `?period=${encodeURIComponent(report.period)}` : '';
        return `${this.baseURL}/api/v1/reports/financials.csv${qs}`;
      }
      case 'ar-aging':
        return `${this.baseURL}/api/v1/reports/ar-aging.csv`;
      case 'daily-close-pdf': {
        const qs = report.operatingDayID
          ? `?operating_day_id=${encodeURIComponent(report.operatingDayID)}`
          : '';
        return `${this.baseURL}/api/v1/stations/${encodeURIComponent(report.stationID)}/reports/daily-close.pdf${qs}`;
      }
      case 'financials-pdf': {
        const qs = report.period ? `?period=${encodeURIComponent(report.period)}` : '';
        return `${this.baseURL}/api/v1/reports/financials.pdf${qs}`;
      }
      case 'gl-export': {
        const params = new URLSearchParams();
        if (report.period) params.set('period', report.period);
        if (report.format) params.set('format', report.format);
        const qs = params.toString();
        return `${this.baseURL}/api/v1/accounting/gl-export.csv${qs ? `?${qs}` : ''}`;
      }
    }
  }

  /** The HTTP Accept header for a report spec — PDF documents vs CSV data. */
  private reportAccept(report: ReportSpec): string {
    return report.kind === 'daily-close-pdf' || report.kind === 'financials-pdf'
      ? 'application/pdf'
      : 'text/csv';
  }

  /**
   * Fetch a standard report's CSV as a Blob (same-origin, cookie-bearing via
   * the BFF). Throws SdkError on a non-2xx response so callers share the app's
   * error handling. The caller is responsible for triggering the download.
   */
  async fetchReportBlob(report: ReportSpec, signal?: AbortSignal): Promise<Blob> {
    const res = await this.fetchImpl(this.reportUrl(report), {
      method: 'GET',
      headers: { Accept: this.reportAccept(report) },
      signal,
      credentials: 'same-origin',
    });
    const requestId = res.headers.get('X-Request-Id');
    if (res.status === 401) this.onUnauthorized?.();
    if (!res.ok) {
      let body: unknown = null;
      try {
        body = safeParse(await res.text());
      } catch {
        body = null;
      }
      const message =
        body && typeof body === 'object' && 'error' in body
          ? String((body as { error: unknown }).error)
          : `HTTP ${res.status}`;
      throw new SdkError(message, res.status, body, requestId);
    }
    return res.blob();
  }

  // ----------- Cash & banking (Phase 7) -----------

  listCashReconciliations(
    stationID: string,
    signal?: AbortSignal,
  ): Promise<Paginated<CashReconciliation>> {
    return this.request<Paginated<CashReconciliation>>(
      `/api/v1/stations/${encodeURIComponent(stationID)}/cash-reconciliations`,
      { signal },
    );
  }

  getCashReconciliation(id: string, signal?: AbortSignal): Promise<CashReconciliation> {
    return this.request<CashReconciliation>(
      `/api/v1/cash-reconciliations/${encodeURIComponent(id)}`,
      { signal },
    );
  }

  createCashReconciliation(
    stationID: string,
    operatingDayID: string,
    signal?: AbortSignal,
  ): Promise<CashReconciliation> {
    return this.request<CashReconciliation>(
      `/api/v1/stations/${encodeURIComponent(stationID)}/cash-reconciliations`,
      { method: 'POST', body: { operating_day_id: operatingDayID }, signal },
    );
  }

  submitCashReconciliation(
    id: string,
    req: { counted_cash: string; notes?: string },
    signal?: AbortSignal,
  ): Promise<CashReconciliation> {
    return this.request<CashReconciliation>(
      `/api/v1/cash-reconciliations/${encodeURIComponent(id)}/submit`,
      { method: 'POST', body: req, signal },
    );
  }

  approveCashReconciliation(id: string, signal?: AbortSignal): Promise<CashReconciliation> {
    return this.request<CashReconciliation>(
      `/api/v1/cash-reconciliations/${encodeURIComponent(id)}/approve`,
      { method: 'POST', signal },
    );
  }

  listBankAccounts(signal?: AbortSignal): Promise<Paginated<BankAccount>> {
    return this.request<Paginated<BankAccount>>('/api/v1/bank-accounts', { signal });
  }

  createBankAccount(
    req: { name: string; account_number?: string; currency?: string },
    signal?: AbortSignal,
  ): Promise<BankAccount> {
    return this.request<BankAccount>('/api/v1/bank-accounts', {
      method: 'POST',
      body: req,
      signal,
    });
  }

  listBankDeposits(
    opts: { stationID?: string } = {},
    signal?: AbortSignal,
  ): Promise<Paginated<BankDeposit>> {
    const qs = opts.stationID ? `?station_id=${encodeURIComponent(opts.stationID)}` : '';
    return this.request<Paginated<BankDeposit>>(`/api/v1/bank-deposits${qs}`, { signal });
  }

  createBankDeposit(
    req: {
      station_id: string;
      bank_account_id: string;
      slip_number?: string;
      reference?: string;
      expected_bank_date?: string;
      lines: Array<{ cash_reconciliation_id: string; amount: string }>;
    },
    signal?: AbortSignal,
  ): Promise<BankDeposit> {
    return this.request<BankDeposit>('/api/v1/bank-deposits', {
      method: 'POST',
      body: req,
      signal,
    });
  }

  prepareBankDeposit(id: string, signal?: AbortSignal): Promise<BankDeposit> {
    return this.request<BankDeposit>(`/api/v1/bank-deposits/${encodeURIComponent(id)}/prepare`, {
      method: 'POST',
      signal,
    });
  }

  confirmBankDeposit(
    id: string,
    req: { actual_bank_date: string; reference?: string },
    signal?: AbortSignal,
  ): Promise<BankDeposit> {
    return this.request<BankDeposit>(`/api/v1/bank-deposits/${encodeURIComponent(id)}/confirm`, {
      method: 'POST',
      body: req,
      signal,
    });
  }

  listBankStatementLines(
    opts: { bankAccountID?: string; status?: string } = {},
    signal?: AbortSignal,
  ): Promise<Paginated<BankStatementLine>> {
    const qs = new URLSearchParams();
    if (opts.bankAccountID) qs.set('bank_account_id', opts.bankAccountID);
    if (opts.status) qs.set('status', opts.status);
    const q = qs.toString();
    return this.request<Paginated<BankStatementLine>>(
      `/api/v1/bank-statement-lines${q ? `?${q}` : ''}`,
      { signal },
    );
  }

  importBankStatement(
    req: {
      bank_account_id: string;
      statement_start?: string;
      statement_end?: string;
      lines: Array<{
        txn_date: string;
        value_date?: string;
        amount: string;
        reference?: string;
        description?: string;
      }>;
    },
    signal?: AbortSignal,
  ): Promise<{ import_id: string; lines: number }> {
    return this.request('/api/v1/bank-statements/import', { method: 'POST', body: req, signal });
  }

  matchBankStatementLine(
    id: string,
    req: { doc_type: string; doc_id: string },
    signal?: AbortSignal,
  ): Promise<{ status: string }> {
    return this.request(`/api/v1/bank-statement-lines/${encodeURIComponent(id)}/match`, {
      method: 'POST',
      body: req,
      signal,
    });
  }

  unmatchBankStatementLine(id: string, signal?: AbortSignal): Promise<{ status: string }> {
    return this.request(`/api/v1/bank-statement-lines/${encodeURIComponent(id)}/unmatch`, {
      method: 'POST',
      signal,
    });
  }

  markBankFeeStatementLine(
    id: string,
    signal?: AbortSignal,
  ): Promise<{ status: string; journal_entry_id: string }> {
    return this.request(`/api/v1/bank-statement-lines/${encodeURIComponent(id)}/bank-fee`, {
      method: 'POST',
      signal,
    });
  }

  // ----------- Customer invoices & payments (Phase 7) -----------

  listCustomerInvoices(
    opts: { customerID?: string } = {},
    signal?: AbortSignal,
  ): Promise<Paginated<CustomerInvoice>> {
    const qs = opts.customerID ? `?customer_id=${encodeURIComponent(opts.customerID)}` : '';
    return this.request<Paginated<CustomerInvoice>>(`/api/v1/customer-invoices${qs}`, { signal });
  }

  getCustomerInvoice(id: string, signal?: AbortSignal): Promise<CustomerInvoice> {
    return this.request<CustomerInvoice>(`/api/v1/customer-invoices/${encodeURIComponent(id)}`, {
      signal,
    });
  }

  createCustomerInvoice(
    req: {
      customer_id: string;
      invoice_number?: string;
      invoice_date?: string;
      due_date?: string;
      source_type?: string;
      source_id?: string;
      station_id?: string;
      lines: Array<{ description?: string; amount: string; revenue_account_key?: string }>;
    },
    signal?: AbortSignal,
  ): Promise<CustomerInvoice> {
    return this.request<CustomerInvoice>('/api/v1/customer-invoices', {
      method: 'POST',
      body: req,
      signal,
    });
  }

  issueCustomerInvoice(id: string, signal?: AbortSignal): Promise<CustomerInvoice> {
    return this.request<CustomerInvoice>(
      `/api/v1/customer-invoices/${encodeURIComponent(id)}/issue`,
      { method: 'POST', signal },
    );
  }

  getCustomerInvoiceAging(signal?: AbortSignal): Promise<Paginated<CustomerBalance>> {
    return this.request<Paginated<CustomerBalance>>('/api/v1/customer-invoices-aging', { signal });
  }

  listCustomerPayments(signal?: AbortSignal): Promise<Paginated<CustomerPayment>> {
    return this.request<Paginated<CustomerPayment>>('/api/v1/customer-payments', { signal });
  }

  postCustomerPayment(
    req: {
      customer_id: string;
      payment_date: string;
      method: string;
      reference?: string;
      source_account_key?: string;
      allocations: Array<{ customer_invoice_id: string; amount: string }>;
    },
    signal?: AbortSignal,
  ): Promise<{ payment_id: string; journal_entry_id: string }> {
    return this.request('/api/v1/customer-payments', { method: 'POST', body: req, signal });
  }

  // ----------- Expenses & petty cash (Phase 7) -----------

  listExpenseCategories(signal?: AbortSignal): Promise<Paginated<ExpenseCategory>> {
    return this.request<Paginated<ExpenseCategory>>('/api/v1/expense-categories', { signal });
  }

  createExpenseCategory(
    req: { name: string; account_key?: string },
    signal?: AbortSignal,
  ): Promise<ExpenseCategory> {
    return this.request<ExpenseCategory>('/api/v1/expense-categories', {
      method: 'POST',
      body: req,
      signal,
    });
  }

  listExpenses(opts: { status?: string } = {}, signal?: AbortSignal): Promise<Paginated<Expense>> {
    const qs = opts.status ? `?status=${encodeURIComponent(opts.status)}` : '';
    return this.request<Paginated<Expense>>(`/api/v1/expenses${qs}`, { signal });
  }

  getExpense(id: string, signal?: AbortSignal): Promise<Expense> {
    return this.request<Expense>(`/api/v1/expenses/${encodeURIComponent(id)}`, { signal });
  }

  createExpense(
    req: {
      station_id?: string;
      category_id?: string;
      payee?: string;
      expense_date?: string;
      amount: string;
      account_key?: string;
      payment_mode?: string;
      reference?: string;
      notes?: string;
    },
    signal?: AbortSignal,
  ): Promise<Expense> {
    return this.request<Expense>('/api/v1/expenses', { method: 'POST', body: req, signal });
  }

  submitExpense(id: string, signal?: AbortSignal): Promise<Expense> {
    return this.request<Expense>(`/api/v1/expenses/${encodeURIComponent(id)}/submit`, {
      method: 'POST',
      signal,
    });
  }

  approveExpense(id: string, signal?: AbortSignal): Promise<Expense> {
    return this.request<Expense>(`/api/v1/expenses/${encodeURIComponent(id)}/approve`, {
      method: 'POST',
      signal,
    });
  }

  postExpense(id: string, signal?: AbortSignal): Promise<Expense> {
    return this.request<Expense>(`/api/v1/expenses/${encodeURIComponent(id)}/post`, {
      method: 'POST',
      signal,
    });
  }

  listPettyCashFloats(signal?: AbortSignal): Promise<Paginated<PettyCashFloat>> {
    return this.request<Paginated<PettyCashFloat>>('/api/v1/petty-cash-floats', { signal });
  }

  getPettyCashFloat(id: string, signal?: AbortSignal): Promise<PettyCashFloat> {
    return this.request<PettyCashFloat>(`/api/v1/petty-cash-floats/${encodeURIComponent(id)}`, {
      signal,
    });
  }

  createPettyCashFloat(
    req: { station_id: string; name: string },
    signal?: AbortSignal,
  ): Promise<PettyCashFloat> {
    return this.request<PettyCashFloat>('/api/v1/petty-cash-floats', {
      method: 'POST',
      body: req,
      signal,
    });
  }

  listPettyCashTransactions(
    floatID: string,
    signal?: AbortSignal,
  ): Promise<Paginated<PettyCashTransaction>> {
    return this.request<Paginated<PettyCashTransaction>>(
      `/api/v1/petty-cash-floats/${encodeURIComponent(floatID)}/transactions`,
      { signal },
    );
  }

  recordPettyCashTransaction(
    floatID: string,
    req: {
      txn_type: 'topup' | 'spend' | 'reimbursement' | 'adjustment' | 'transfer';
      amount: string;
      date?: string;
      description?: string;
      account_key?: string;
      overdraw?: boolean;
    },
    signal?: AbortSignal,
  ): Promise<PettyCashTransaction> {
    return this.request<PettyCashTransaction>(
      `/api/v1/petty-cash-floats/${encodeURIComponent(floatID)}/transactions`,
      { method: 'POST', body: req, signal },
    );
  }

  reconcilePettyCash(
    floatID: string,
    req: { counted_cash: string; date?: string },
    signal?: AbortSignal,
  ): Promise<{ id: string; expected_balance: string; counted_cash: string; variance: string }> {
    return this.request(`/api/v1/petty-cash-floats/${encodeURIComponent(floatID)}/reconcile`, {
      method: 'POST',
      body: req,
      signal,
    });
  }

  // ----------- Enterprise governance (Phase 9) -----------

  listStationGroups(signal?: AbortSignal): Promise<Paginated<StationGroup>> {
    return this.request<Paginated<StationGroup>>('/api/v1/enterprise/station-groups', { signal });
  }

  createStationGroup(
    req: { name: string; kind?: string },
    signal?: AbortSignal,
  ): Promise<StationGroup> {
    return this.request<StationGroup>('/api/v1/enterprise/station-groups', {
      method: 'POST',
      body: req,
      signal,
    });
  }

  addStationGroupMember(
    groupID: string,
    stationID: string,
    signal?: AbortSignal,
  ): Promise<unknown> {
    return this.request(
      `/api/v1/enterprise/station-groups/${encodeURIComponent(groupID)}/members`,
      {
        method: 'POST',
        body: { station_id: stationID },
        signal,
      },
    );
  }

  grantEnterpriseScope(
    req: {
      user_id: string;
      scope_type: 'tenant' | 'company' | 'region' | 'group' | 'station';
      scope_id?: string;
    },
    signal?: AbortSignal,
  ): Promise<{ id: string }> {
    return this.request('/api/v1/enterprise/scope-grants', { method: 'POST', body: req, signal });
  }

  getEffectiveStations(
    userID: string,
    signal?: AbortSignal,
  ): Promise<{ user_id: string; tenant_wide: boolean; station_ids: string[] }> {
    return this.request(
      `/api/v1/enterprise/users/${encodeURIComponent(userID)}/effective-stations`,
      { signal },
    );
  }

  listApprovalPolicies(signal?: AbortSignal): Promise<{ items: unknown[]; count: number }> {
    return this.request('/api/v1/approval-policies', { signal });
  }

  createApprovalPolicy(
    req: {
      workflow_type: string;
      min_amount?: string;
      required_approvals?: number;
      required_role?: string;
    },
    signal?: AbortSignal,
  ): Promise<{ id: string }> {
    return this.request('/api/v1/approval-policies', { method: 'POST', body: req, signal });
  }

  listApprovalRequests(
    opts: { status?: string } = {},
    signal?: AbortSignal,
  ): Promise<Paginated<ApprovalRequest>> {
    const qs = opts.status ? `?status=${encodeURIComponent(opts.status)}` : '';
    return this.request<Paginated<ApprovalRequest>>(`/api/v1/approval-requests${qs}`, { signal });
  }

  raiseApprovalRequest(
    req: {
      workflow_type: string;
      reference_type?: string;
      reference_id?: string;
      amount?: string;
      station_id?: string;
    },
    signal?: AbortSignal,
  ): Promise<ApprovalRequest> {
    return this.request<ApprovalRequest>('/api/v1/approval-requests', {
      method: 'POST',
      body: req,
      signal,
    });
  }

  decideApprovalRequest(
    id: string,
    req: { decision: 'approve' | 'reject'; comment?: string },
    signal?: AbortSignal,
  ): Promise<ApprovalRequest> {
    return this.request<ApprovalRequest>(
      `/api/v1/approval-requests/${encodeURIComponent(id)}/decide`,
      { method: 'POST', body: req, signal },
    );
  }

  // ----------- Enterprise dashboards (Phase 9) -----------

  getEnterpriseOverview(
    opts: { from?: string; to?: string } = {},
    signal?: AbortSignal,
  ): Promise<EnterpriseOverview> {
    const qs = new URLSearchParams();
    if (opts.from) qs.set('from', opts.from);
    if (opts.to) qs.set('to', opts.to);
    const q = qs.toString();
    return this.request<EnterpriseOverview>(`/api/v1/enterprise/overview${q ? `?${q}` : ''}`, {
      signal,
    });
  }

  getStationRanking(
    opts: { regionID?: string; from?: string; to?: string } = {},
    signal?: AbortSignal,
  ): Promise<{ items: StationRank[]; count: number }> {
    const qs = new URLSearchParams();
    if (opts.regionID) qs.set('region_id', opts.regionID);
    if (opts.from) qs.set('from', opts.from);
    if (opts.to) qs.set('to', opts.to);
    const q = qs.toString();
    return this.request(`/api/v1/enterprise/station-ranking${q ? `?${q}` : ''}`, { signal });
  }

  rebuildEnterpriseProjections(
    signal?: AbortSignal,
  ): Promise<{ projection: string; rows: number }> {
    return this.request('/api/v1/enterprise/projections/rebuild', { method: 'POST', signal });
  }

  // ----------- Central commercial control (Phase 9) -----------

  listCentralPriceRollouts(signal?: AbortSignal): Promise<Paginated<CentralPriceRollout>> {
    return this.request<Paginated<CentralPriceRollout>>('/api/v1/central-price-rollouts', {
      signal,
    });
  }

  createCentralPriceRollout(
    req: {
      product_id: string;
      scope_type: 'tenant' | 'region' | 'station';
      scope_id?: string;
      unit_price: string;
      effective_from?: string;
    },
    signal?: AbortSignal,
  ): Promise<CentralPriceRollout> {
    return this.request<CentralPriceRollout>('/api/v1/central-price-rollouts', {
      method: 'POST',
      body: req,
      signal,
    });
  }

  transitionCentralPriceRollout(
    id: string,
    action: 'approve' | 'activate',
    signal?: AbortSignal,
  ): Promise<CentralPriceRollout> {
    return this.request<CentralPriceRollout>(
      `/api/v1/central-price-rollouts/${encodeURIComponent(id)}/${action}`,
      { method: 'POST', signal },
    );
  }

  listProcurementPlans(signal?: AbortSignal): Promise<{ items: unknown[]; count: number }> {
    return this.request('/api/v1/central-procurement-plans', { signal });
  }

  createProcurementPlan(
    req: {
      name: string;
      lines?: Array<{ station_id: string; product_id: string; target_litres: string }>;
    },
    signal?: AbortSignal,
  ): Promise<{ id: string }> {
    return this.request('/api/v1/central-procurement-plans', { method: 'POST', body: req, signal });
  }

  releaseProcurementPlan(
    id: string,
    signal?: AbortSignal,
  ): Promise<{ id: string; released_lines: number }> {
    return this.request(`/api/v1/central-procurement-plans/${encodeURIComponent(id)}/release`, {
      method: 'POST',
      signal,
    });
  }

  listStockTransfers(signal?: AbortSignal): Promise<Paginated<StockTransfer>> {
    return this.request<Paginated<StockTransfer>>('/api/v1/stock-transfers', { signal });
  }

  createStockTransfer(
    req: { from_tank_id: string; to_tank_id: string; product_id: string; litres: string },
    signal?: AbortSignal,
  ): Promise<StockTransfer> {
    return this.request<StockTransfer>('/api/v1/stock-transfers', {
      method: 'POST',
      body: req,
      signal,
    });
  }

  transitionStockTransfer(
    id: string,
    action: 'approve' | 'receive',
    signal?: AbortSignal,
  ): Promise<StockTransfer> {
    return this.request<StockTransfer>(
      `/api/v1/stock-transfers/${encodeURIComponent(id)}/${action}`,
      { method: 'POST', signal },
    );
  }

  // ----------- Consolidated finance & reports (Phase 9) -----------

  getConsolidatedFinance(
    opts: { from?: string; to?: string; asOf?: string } = {},
    signal?: AbortSignal,
  ): Promise<{
    from: string;
    to: string;
    as_of: string;
    income_statement: { revenue: string; expenses: string; net_profit: string };
    balance_sheet: { assets: string; liabilities: string; equity: string };
    by_station: StationRank[];
  }> {
    const qs = new URLSearchParams();
    if (opts.from) qs.set('from', opts.from);
    if (opts.to) qs.set('to', opts.to);
    if (opts.asOf) qs.set('as_of', opts.asOf);
    const q = qs.toString();
    return this.request(`/api/v1/enterprise/finance/consolidated${q ? `?${q}` : ''}`, { signal });
  }

  exportStationKPIs(
    opts: { from?: string; to?: string } = {},
    signal?: AbortSignal,
  ): Promise<{ from: string; to: string; row_count: number; checksum: string; csv: string }> {
    const qs = new URLSearchParams();
    if (opts.from) qs.set('from', opts.from);
    if (opts.to) qs.set('to', opts.to);
    const q = qs.toString();
    return this.request(`/api/v1/enterprise/reports/station-kpis${q ? `?${q}` : ''}`, { signal });
  }

  getEnterpriseExceptions(
    signal?: AbortSignal,
  ): Promise<{ checks: Record<string, number>; total: number }> {
    return this.request('/api/v1/enterprise/exceptions', { signal });
  }

  // ----------- Risk, fraud & intelligence (Phase 10) -----------

  backfillRiskSignals(signal?: AbortSignal): Promise<{ created: number }> {
    return this.request('/api/v1/risk/signals/backfill', { method: 'POST', signal });
  }

  listRiskSignals(
    opts: { type?: string } = {},
    signal?: AbortSignal,
  ): Promise<{ items: unknown[]; count: number }> {
    const qs = opts.type ? `?type=${encodeURIComponent(opts.type)}` : '';
    return this.request(`/api/v1/risk/signals${qs}`, { signal });
  }

  listRiskRules(signal?: AbortSignal): Promise<{ items: unknown[]; count: number }> {
    return this.request('/api/v1/risk/rules', { signal });
  }

  createRiskRule(
    req: {
      code: string;
      name: string;
      rule_type?: string;
      severity?: string;
      description?: string;
      threshold?: string;
      lookback_days?: number;
    },
    signal?: AbortSignal,
  ): Promise<{ id: string }> {
    return this.request('/api/v1/risk/rules', { method: 'POST', body: req, signal });
  }

  setRiskRuleStatus(
    id: string,
    status: 'draft' | 'active' | 'paused' | 'retired',
    signal?: AbortSignal,
  ): Promise<{ id: string; status: string }> {
    return this.request(`/api/v1/risk/rules/${encodeURIComponent(id)}/status`, {
      method: 'POST',
      body: { status },
      signal,
    });
  }

  runRiskDetection(signal?: AbortSignal): Promise<{ alerts_created: number }> {
    return this.request('/api/v1/risk/detect', { method: 'POST', signal });
  }

  listRiskAlerts(
    opts: { status?: string; type?: string } = {},
    signal?: AbortSignal,
  ): Promise<Paginated<RiskAlert>> {
    const qs = new URLSearchParams();
    if (opts.status) qs.set('status', opts.status);
    if (opts.type) qs.set('type', opts.type);
    const q = qs.toString();
    return this.request<Paginated<RiskAlert>>(`/api/v1/risk/alerts${q ? `?${q}` : ''}`, { signal });
  }

  getRiskAlert(id: string, signal?: AbortSignal): Promise<RiskAlert> {
    return this.request<RiskAlert>(`/api/v1/risk/alerts/${encodeURIComponent(id)}`, { signal });
  }

  transitionRiskAlert(
    id: string,
    action: 'acknowledge' | 'investigate' | 'resolve' | 'dismiss' | 'escalate',
    req: { disposition?: string } = {},
    signal?: AbortSignal,
  ): Promise<RiskAlert> {
    return this.request<RiskAlert>(`/api/v1/risk/alerts/${encodeURIComponent(id)}/${action}`, {
      method: 'POST',
      body: req,
      signal,
    });
  }

  getRiskOverview(signal?: AbortSignal): Promise<{
    open_by_severity: Record<string, number>;
    open_total: number;
    top_stations: Array<{ entity_id: string; score: number; band: string; open_alerts: number }>;
    scores_computed_at?: string;
  }> {
    return this.request('/api/v1/risk/overview', { signal });
  }

  listRiskScores(
    opts: { dimension?: string } = {},
    signal?: AbortSignal,
  ): Promise<{ items: unknown[]; count: number }> {
    const qs = opts.dimension ? `?dimension=${encodeURIComponent(opts.dimension)}` : '';
    return this.request(`/api/v1/risk/scores${qs}`, { signal });
  }

  recomputeRiskScores(signal?: AbortSignal): Promise<{ scored_stations: number }> {
    return this.request('/api/v1/risk/scores/recompute', { method: 'POST', signal });
  }

  // ----------- Investigations (Phase 10) -----------

  listInvestigations(
    opts: { status?: string } = {},
    signal?: AbortSignal,
  ): Promise<{ items: unknown[]; count: number }> {
    const qs = opts.status ? `?status=${encodeURIComponent(opts.status)}` : '';
    return this.request(`/api/v1/investigations${qs}`, { signal });
  }

  getInvestigation(id: string, signal?: AbortSignal): Promise<Record<string, unknown>> {
    return this.request(`/api/v1/investigations/${encodeURIComponent(id)}`, { signal });
  }

  createInvestigation(
    req: { title: string; case_type?: string; severity?: string; alert_id?: string },
    signal?: AbortSignal,
  ): Promise<Record<string, unknown>> {
    return this.request('/api/v1/investigations', { method: 'POST', body: req, signal });
  }

  attachAlertToInvestigation(id: string, alertID: string, signal?: AbortSignal): Promise<unknown> {
    return this.request(`/api/v1/investigations/${encodeURIComponent(id)}/alerts`, {
      method: 'POST',
      body: { alert_id: alertID },
      signal,
    });
  }

  addInvestigationComment(id: string, body: string, signal?: AbortSignal): Promise<{ id: string }> {
    return this.request(`/api/v1/investigations/${encodeURIComponent(id)}/comments`, {
      method: 'POST',
      body: { body },
      signal,
    });
  }

  addInvestigationAction(
    id: string,
    req: { action_type: string; detail?: string },
    signal?: AbortSignal,
  ): Promise<{ id: string }> {
    return this.request(`/api/v1/investigations/${encodeURIComponent(id)}/actions`, {
      method: 'POST',
      body: req,
      signal,
    });
  }

  setInvestigationActionStatus(
    id: string,
    actionID: string,
    status: 'suggested' | 'accepted' | 'completed' | 'dismissed',
    signal?: AbortSignal,
  ): Promise<{ id: string; status: string }> {
    return this.request(
      `/api/v1/investigations/${encodeURIComponent(id)}/actions/${encodeURIComponent(actionID)}/status`,
      { method: 'POST', body: { status }, signal },
    );
  }

  setInvestigationStatus(
    id: string,
    req: {
      status: 'open' | 'assigned' | 'in_review' | 'action_required' | 'resolved' | 'closed';
      resolution?: string;
    },
    signal?: AbortSignal,
  ): Promise<Record<string, unknown>> {
    return this.request(`/api/v1/investigations/${encodeURIComponent(id)}/status`, {
      method: 'POST',
      body: req,
      signal,
    });
  }

  // ----------- Risk tuning & governance (Phase 10) -----------

  getRiskGovernance(signal?: AbortSignal): Promise<Record<string, number>> {
    return this.request('/api/v1/risk/governance', { signal });
  }

  listRiskSuppressions(signal?: AbortSignal): Promise<{ items: unknown[]; count: number }> {
    return this.request('/api/v1/risk/suppressions', { signal });
  }

  createRiskSuppression(
    req: { alert_type: string; entity_id?: string; reason: string; expires_at?: string },
    signal?: AbortSignal,
  ): Promise<{ id: string }> {
    return this.request('/api/v1/risk/suppressions', { method: 'POST', body: req, signal });
  }

  tuneRiskRule(
    id: string,
    req: { threshold?: string; lookback_days?: number; severity?: string },
    signal?: AbortSignal,
  ): Promise<{ id: string }> {
    return this.request(`/api/v1/risk/rules/${encodeURIComponent(id)}/tune`, {
      method: 'POST',
      body: req,
      signal,
    });
  }

  pauseRiskEngine(signal?: AbortSignal): Promise<{ paused_rules: number }> {
    return this.request('/api/v1/risk/engine/pause', { method: 'POST', signal });
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

  // ----------- Notifications -----------

  listNotifications(
    opts: { unread?: boolean; limit?: number; offset?: number } = {},
    signal?: AbortSignal,
  ): Promise<Paginated<Notification>> {
    const qs = new URLSearchParams();
    if (opts.unread) qs.set('unread', 'true');
    if (opts.limit) qs.set('limit', String(opts.limit));
    if (opts.offset) qs.set('offset', String(opts.offset));
    const q = qs.toString();
    return this.request<Paginated<Notification>>(`/api/v1/notifications${q ? `?${q}` : ''}`, {
      signal,
    });
  }

  notificationUnreadCount(signal?: AbortSignal): Promise<UnreadCount> {
    return this.request<UnreadCount>('/api/v1/notifications/unread-count', { signal });
  }

  markNotificationRead(id: string, signal?: AbortSignal): Promise<void> {
    return this.request<void>(`/api/v1/notifications/${encodeURIComponent(id)}/read`, {
      method: 'POST',
      signal,
    });
  }

  markAllNotificationsRead(signal?: AbortSignal): Promise<{ marked_read: number }> {
    return this.request<{ marked_read: number }>('/api/v1/notifications/read-all', {
      method: 'POST',
      signal,
    });
  }

  // ----------- Admin / system -----------

  /**
   * Latest run of every background scheduler job (name, last run, status,
   * duration) for the admin System health page. Requires the `audit.read`
   * permission.
   */
  listJobRuns(signal?: AbortSignal): Promise<JobRunList> {
    return this.request<JobRunList>('/api/v1/admin/jobs', { signal });
  }
}

function safeParse(text: string): unknown {
  try {
    return JSON.parse(text);
  } catch {
    return text;
  }
}
