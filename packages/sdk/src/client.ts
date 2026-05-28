import type {
  Account,
  AccountingExport,
  AccountingExportResult,
  AccountingPeriod,
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
  LoginRequest,
  LoginResponse,
  Me,
  MePermissions,
  MeterReading,
  MeterReadingList,
  MyShift,
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
  Role,
  Sale,
  TankValuation,
  ARentry,
  CreditPosition,
  CreditProfile,
  Customer,
  CustomerBalance,
  CustomerContact,
  CustomerInvoice,
  CustomerPayment,
  CustomerPriceAgreement,
  CustomerStatement,
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
  UserSummary,
  Delivery,
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
      lines: Array<{ product_id: string; ordered_litres: number; unit_price: string }>;
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
      lines?: Array<{ product_id: string; ordered_litres: number; unit_price: string }>;
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
    req: { operating_day_id: string; name: string; notes?: string },
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

  // ----------- Meter readings -----------

  listMeterReadings(shiftID: string, signal?: AbortSignal): Promise<MeterReadingList> {
    return this.request<MeterReadingList>(
      `/api/v1/shifts/${encodeURIComponent(shiftID)}/meter-readings`,
      { signal },
    );
  }

  captureMeterReading(
    shiftID: string,
    req: { nozzle_id: string; reading_type: 'opening' | 'closing'; reading: number },
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
    reading: number,
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
      dip_mm: number;
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
    req: { dip_mm: number; water_mm?: number; temperature_c?: number },
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
      cash_amount: number;
      mobile_money_amount?: number;
      card_amount?: number;
      credit_amount?: number;
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
