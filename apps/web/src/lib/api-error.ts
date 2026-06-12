import { SdkError } from '@fuelgrid/sdk';

/**
 * Helpers over the API error contract: handlers attach a machine-readable
 * `code` (and sometimes extra fields like `unverified_count` or
 * `unapproved_shift_ids`) to refusal bodies — e.g. the shift-approval gates
 * `readings_unverified` / `collection_unconfirmed` and the handover gate
 * `prior_shift_unapproved` (Mobile Attendant Phase 0). Pages branch on the
 * code to render plain-language guidance instead of the raw error string.
 */

/** The parsed error body of an SdkError, when it is a JSON object. */
export function apiErrorBody(e: unknown): Record<string, unknown> | null {
  if (e instanceof SdkError && e.body && typeof e.body === 'object') {
    return e.body as Record<string, unknown>;
  }
  return null;
}

/** The error body's machine-readable `code`, when present. */
export function apiErrorCode(e: unknown): string | undefined {
  const code = apiErrorBody(e)?.code;
  return typeof code === 'string' ? code : undefined;
}

/** The server's message for an SdkError, with a fallback for anything else. */
export function apiErrorMessage(e: unknown, fallback: string): string {
  return e instanceof SdkError && e.message ? e.message : fallback;
}
