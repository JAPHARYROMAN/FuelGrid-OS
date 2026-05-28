// Package banking is the data layer for Phase 7 Category B — cash and banking.
// It covers cash reconciliations (verifying Phase-6 expected cash against
// counted cash), bank deposits (moving station cash to the bank through a
// clearing account), and bank statement import/matching. Money is carried as
// decimal strings; all arithmetic and balance comparisons run in SQL.
package banking

import (
	"errors"

	"github.com/japharyroman/fuelgrid-os/internal/database"
)

var (
	// ErrDuplicate is returned when a uniqueness guard rejects a row (e.g. a
	// second reconciliation for an operating day, or depositing the same
	// reconciliation twice, or re-importing the same statement).
	ErrDuplicate = errors.New("banking: duplicate")
	// ErrNotFound is returned when a row does not exist for the tenant.
	ErrNotFound = errors.New("banking: not found")
	// ErrBadState is returned when a lifecycle transition is not allowed from
	// the current status.
	ErrBadState = errors.New("banking: invalid state transition")
)

type Repo struct{ pool *database.Pool }

func New(pool *database.Pool) *Repo { return &Repo{pool: pool} }
