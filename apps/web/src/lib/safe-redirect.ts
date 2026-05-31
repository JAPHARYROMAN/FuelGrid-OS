/**
 * safeRedirect guards a post-login `?next=` value against open-redirect.
 *
 * A value is only honoured when it is a same-origin absolute path: it must
 * start with a single "/" and NOT with "//" (protocol-relative, e.g.
 * //evil.com) or "/\" (which some browsers also normalise to a
 * protocol-relative URL). Absolute URLs ("https://evil.com"), scheme-relative
 * URLs, javascript: payloads, and anything that isn't a leading-slash path
 * fall back to the default (the command center). This keeps an attacker from
 * crafting a /login?next=https://evil.com link that bounces a freshly
 * authenticated user off-site.
 */
const DEFAULT_REDIRECT = '/command-center';

export function safeRedirect(next: string | null | undefined): string {
  if (!next) return DEFAULT_REDIRECT;
  // Must be an absolute, same-origin path.
  if (!next.startsWith('/')) return DEFAULT_REDIRECT;
  // Reject protocol-relative ("//host") and backslash-tricked ("/\\host")
  // forms that browsers can resolve to a different origin.
  if (next.startsWith('//') || next.startsWith('/\\')) return DEFAULT_REDIRECT;
  return next;
}
