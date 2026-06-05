import { z } from 'zod';

/**
 * SCOPED runtime validation (SDK-01 / FE-10).
 *
 * These schemas cover ONLY a curated set of critical/auth responses:
 *   - the login result            (drives session establishment)
 *   - /me (current user)          (identity + tenant)
 *   - /me/permissions             (authorization gates the whole UI)
 *   - the generic paged-list envelope shape
 *
 * Deliberately NOT a blanket validator for every endpoint: there is no e2e
 * contract test, the response types are hand-maintained, and an over-strict
 * schema would throw at runtime on any drift the types missed — breaking the
 * running app even with a green build. Money/litre fields are exact DECIMAL
 * STRINGS from the Go DTOs, so they are validated as z.string() to match
 * types.ts, never z.number().
 *
 * Schemas are intentionally permissive about extra/unknown keys (zod objects
 * strip unknowns by default) and only assert the fields the app relies on, so
 * additive server changes don't trip validation.
 */

export const loginResponseSchema = z.object({
  token: z.string().optional(),
  expires_at: z.string().optional(),
  mfa_required: z.boolean().optional(),
});

export const meSchema = z.object({
  user_id: z.string(),
  tenant_id: z.string(),
  session_id: z.string(),
  mfa_satisfied: z.boolean(),
  mfa_enabled: z.boolean().optional(),
  mfa_required: z.boolean().optional(),
  mfa_backup_codes_remaining: z.number().optional(),
});

export const permissionItemSchema = z.object({
  code: z.string(),
  station_scoped: z.boolean(),
});

export const mePermissionsSchema = z.object({
  permissions: z.array(permissionItemSchema),
  station_ids: z.array(z.string()).optional(),
  tenant_wide: z.boolean(),
  is_system_admin: z.boolean().optional(),
});

/**
 * The generic paged-list envelope. The server returns {items,count} and
 * (depending on the endpoint) limit/offset/has_more for cursor-style paging.
 * Only the envelope is validated here — item shapes are left to the typed
 * cast, so this stays a safe, reusable guard for any list call that opts in.
 */
export function pagedEnvelopeSchema<T extends z.ZodTypeAny>(item: T) {
  return z.object({
    items: z.array(item),
    count: z.number(),
    limit: z.number().optional(),
    offset: z.number().optional(),
    has_more: z.boolean().optional(),
  });
}
