# Secrets handling

How FuelGrid OS classifies, stores, loads, redacts, and rotates sensitive
configuration. Addresses OPS-2 / MT-5 / SEC-7: secrets must never leak into
logs, error output, or version control.

## Inventory

Every value below carries a credential or grants privileged access. All are
read from the environment by [`services/api/internal/config`](../../services/api/internal/config/config.go);
the ones marked **Secret type** are typed as `config.Secret` so they are
redacted on any stringification or structured-log attribute.

| Env var | Used by | Sensitivity | Redacted (`config.Secret`) |
|---|---|---|---|
| `DATABASE_URL` | Owner DB pool (migrations, pre-auth reads, background jobs) | DSN embeds the DB password | yes |
| `DATABASE_APP_URL` | Request-scoped `fuelgrid_app` pool (RLS enforced) | DSN embeds the DB password | yes |
| `REDIS_URL` | Session store, rate limiter | URL may embed the Redis password | yes |
| `AUTH_PASSWORD_PEPPER` | argon2id HMAC pepper for every password hash | Compromise weakens all stored hashes | yes |
| `PLATFORM_ADMIN_TOKEN` | Bearer for `POST /api/v1/platform/tenants` | Static operator/IaC credential; provisions tenants | yes |
| `SMTP_PASSWORD` | Outbound mail (resets, invites) | Mail-relay credential | managed at the SMTP layer; not yet in `config.Config` |
| `SENTRY_DSN` | Error reporting | Write key for the Sentry project | low-sensitivity; not redacted today |

`DEMO_USER_PASSWORD` and `DEMO_ADMIN_PASSWORD` are local-seed conveniences only.
They must never be set in production.

## Redaction: the `config.Secret` type

`config.Secret` is a string-backed type (so [`envconfig`](https://github.com/kelseyhightower/envconfig)
populates it directly from the environment with no custom decoder). It defends
against accidental disclosure two ways:

- `String() string` (fmt.Stringer) returns `***redacted***` for any non-empty
  value, so `%s`/`%v` formatting — including the default rendering Go uses when
  a value lands in a log field or an error string — prints the placeholder, not
  the plaintext.
- `LogValue() slog.Value` (slog.LogValuer) returns the same placeholder, so
  structured logging redacts the value even when the secret is passed directly
  as a slog attribute (slog consults `LogValue` rather than `String` on that
  path).

An **empty** secret renders as `""` (not the placeholder) so "is this
configured?" log lines stay truthful.

To use the plaintext at the boundary where it is genuinely required (building a
DB DSN, seeding the password hasher), call `Secret.Reveal()`. The explicit name
makes every disclosure greppable and reviewable. Never pass a `Reveal()` result
to a logger.

A regression test lives in
[`services/api/internal/config/secret_test.go`](../../services/api/internal/config/secret_test.go).

### Rule of thumb

- Log the **fact** a dependency is configured, never its value
  (`logger.Info("postgres connected")`, not the DSN).
- Add new credential-bearing config as `config.Secret`, not `string`.
- Reach for `.Reveal()` only at the consuming call site, never near a logger.

## Storage and loading

- **Local development**: copy [`.env.example`](../../.env.example) to `.env`
  (gitignored) and fill in values. The pepper and platform-admin token may be
  empty in `NODE_ENV=development`.
- **Production**: secrets live in the deployment platform's secret store (Fly
  secrets / Vault — see [deployment.md](../deployment.md)), injected as
  environment variables at boot. They are **never** committed to git and never
  baked into the container image.
- `.env` is gitignored; only `.env.example` (placeholders, no real values) is
  tracked.

## Rotation

| Secret | Rotation impact | Procedure |
|---|---|---|
| `AUTH_PASSWORD_PEPPER` | **Invalidates every stored password hash.** | Rotate only with a coordinated forced-password-reset wave. Do not rotate casually. |
| `PLATFORM_ADMIN_TOKEN` | Old token stops working immediately; provisioning automation must update. | Set the new value in the secret store, redeploy, update IaC/CI that calls the tenant endpoint. |
| `DATABASE_URL` / `DATABASE_APP_URL` | Connections using the old credential fail. | Rotate the DB role password in Postgres, update the secret, redeploy. `DATABASE_APP_URL` must stay a distinct non-owner role from `DATABASE_URL`. |
| `REDIS_URL` | Cache/session/rate-limit connections fail. | Rotate the Redis credential, update the secret, redeploy. Active sessions in the store survive if the data is preserved. |

## If a secret leaks

1. Rotate the affected credential immediately (table above).
2. For `AUTH_PASSWORD_PEPPER`, treat all stored hashes as compromised and force
   a reset wave.
3. Audit access logs for use of the leaked credential.
4. Purge the value from wherever it leaked (logs, tickets, chat) and confirm it
   was never committed (`git log -S`).
