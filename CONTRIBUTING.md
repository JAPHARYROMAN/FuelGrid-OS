# Contributing to FuelGrid OS

Quick reference for how we work in this repo. Keep it open while you're shipping.

## Toolchain

| Tool | Version                         |
| ---- | ------------------------------- |
| Node | 22 (LTS) — see [.nvmrc](.nvmrc) |
| pnpm | 10.x — enabled via Corepack     |
| Go   | 1.25 — see [go.mod](go.mod)     |

Install:

```sh
corepack enable
pnpm install
```

## Branching

- `main` is the only long-lived branch and is protected.
- Branch names: `type/short-slug` — e.g. `feat/identity-mfa`, `fix/audit-log-actor`, `chore/upgrade-pnpm`.
- One concern per branch. Open a draft PR early.

## Commit messages — Conventional Commits

Format: `<type>(<scope>)?: <subject>`

```
feat(auth): add TOTP MFA enrollment endpoint
fix(inventory): correct variance sign on transfer-out
chore(ci): bump actions/checkout to v4
docs(roadmap): mark Stage 3 stock ledger complete
```

| Type       | When to use                         |
| ---------- | ----------------------------------- |
| `feat`     | New user-visible capability         |
| `fix`      | Bug fix                             |
| `refactor` | Code change with no behavior change |
| `perf`     | Performance improvement             |
| `docs`     | Docs only                           |
| `test`     | Test additions/fixes only           |
| `build`    | Build system, dependencies          |
| `ci`       | CI/CD config                        |
| `chore`    | Tooling, repo housekeeping          |

Scopes follow domain boundaries from [docs/architecture.md](docs/architecture.md) §6:
`auth`, `tenant`, `station`, `tank`, `pump`, `shift`, `inventory`, `delivery`, `sales`, `finance`, `customer`, `fleet`, `risk`, `alert`, `reporting`, `audit`, `integration`, `ai`, plus tooling scopes: `web`, `api`, `mobile`, `sdk`, `config`, `ci`, `docs`, `repo`.

Subject line: imperative mood, no trailing period, ≤72 chars. Use the body to explain _why_, not _what_.

## Pull requests

- Title follows the same Conventional Commit format as the squash commit.
- Description must cover **what changed** and **why**, plus a **how to verify** section a reviewer can paste into a terminal.
- Link the roadmap stage if relevant: "Closes Stage 3 step: stock ledger migrations".
- Keep PRs reviewable — under ~400 changed lines is the soft target.
- All CI checks must be green; merge via squash.

## Code style

- TypeScript: enforced via [packages/config](packages/config). Don't loosen `strict`, `noUncheckedIndexedAccess`, or `verbatimModuleSyntax` in a package's local `tsconfig.json`.
- Go: `gofmt` + `golangci-lint` (config in [.golangci.yml](.golangci.yml)). Use `slog`, not `log`.
- No emojis in code or commit messages.
- No comments that restate the code. Use comments only for non-obvious _why_.

## Pre-commit hooks

Husky runs `lint-staged` on staged files: Prettier formats everything, ESLint auto-fixes JS/TS. If the hook fails, fix the issue and re-stage — don't `--no-verify`.

## Database migrations (from Stage 3)

- One concern per migration file.
- File naming: `NNNN_short_slug.up.sql` / `.down.sql` (zero-padded sequential).
- Always include a working `down` migration during development; production migrations may be irreversible by design but require explicit sign-off.

## Domain rules to internalize early

These come from [docs/architecture.md](docs/architecture.md) and aren't negotiable per-feature:

1. **Every tenant-owned table has `tenant_id`** and is scoped by it. No exceptions.
2. **The inventory ledger is append-only.** Corrections create adjustment entries; they never edit prior rows.
3. **Sensitive writes emit both an audit log and an outbox event** in the same DB transaction.
4. **Frontend permission checks are UX hints only.** The API is authoritative.
5. **Offline data preserves both attempts on conflict.** Never silently overwrite.

## Reporting issues

Open a GitHub issue with the relevant stage label. For security issues, do not open a public issue — contact the maintainer directly.
