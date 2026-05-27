import type { LoginRequest, LoginResponse, Me, MePermissions, Station } from './types';

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
  /** Force the request to skip the Authorization header (e.g. login). */
  unauthenticated?: boolean;
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
      headers['Content-Type'] = 'application/json';
      body = JSON.stringify(opts.body);
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

  // ----------- Stations -----------

  getStation(stationID: string, signal?: AbortSignal): Promise<Station> {
    return this.request<Station>(`/api/v1/stations/${encodeURIComponent(stationID)}`, {
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
