// Package fleet is the data layer for Phase 8 — Customer Credit & Fleet Fuel
// OS. It owns everything net-new to the phase: customer contacts, credit
// profiles, customer price agreements, fleet vehicles, drivers, fuel
// credentials, authorization policies/limits, fuel authorizations, odometer
// readings, customer statements, and credit alerts. The Phase-6 sale engine and
// Phase-7 AR ledger remain the sources of truth for sales and balances; this
// package decides whether a credit/fleet sale is allowed and captures the
// operational evidence around it. Money is decimal strings; credentials and
// PINs are stored only as salted hashes.
package fleet

import (
	"errors"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/japharyroman/fuelgrid-os/internal/database"
	"github.com/japharyroman/fuelgrid-os/internal/identity/password"
)

var (
	ErrNotFound = errors.New("fleet: not found")
	ErrConflict = errors.New("fleet: conflict")
	ErrBadState = errors.New("fleet: invalid state transition")
	ErrDenied   = errors.New("fleet: authorization denied")
	ErrConsumed = errors.New("fleet: authorization already consumed")
	// ErrSaleNotFound means the consumed_by sale id passed to FulfillAuthorization
	// does not name a sale in the caller's tenant (W1-FLEET-FK).
	ErrSaleNotFound = errors.New("fleet: consuming sale not found")
	ErrValidation   = errors.New("fleet: validation failed")
)

type Repo struct {
	pool *database.Pool
	// hasher hashes driver PINs with argon2id (slow, salted) — PINs are
	// verified against a known driver, so a per-record salt is fine.
	hasher *password.Hasher
	// tokenKey keys the HMAC used for credential token hashes. Tokens are looked
	// up by hash, so they need a deterministic keyed digest (not a per-record
	// salt); HMAC-SHA256 keyed by the pepper makes offline brute-force of a
	// low-entropy token infeasible without the server secret.
	tokenKey []byte
}

// New builds the fleet repo. pepper is the server password pepper; it keys both
// the PIN hasher and the credential-token HMAC. Empty is acceptable in dev.
func New(pool *database.Pool, pepper string) *Repo {
	return &Repo{
		pool:     pool,
		hasher:   password.New(password.DefaultParams, pepper),
		tokenKey: []byte(pepper),
	}
}

// nullableMoney returns nil for an empty string so a SQL COALESCE can fall back
// to a default; otherwise it returns a pointer to the decimal string.
func nullableMoney(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// isUniqueViolation reports whether err is a Postgres unique-constraint error.
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
