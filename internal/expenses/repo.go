// Package expenses is the data layer for Phase 7 Category E — operating
// expenses and petty cash. Expenses move draft -> submitted -> approved ->
// posted, debiting an expense account and crediting the payment-mode account.
// Petty cash floats hold station cash topped up from the bank and drawn down by
// spend, with reconciliation posting variance to cash over/short. Money is
// carried as decimal strings; arithmetic and guards run in SQL.
package expenses

import (
	"errors"

	"github.com/japharyroman/fuelgrid-os/internal/database"
)

var (
	ErrNotFound  = errors.New("expenses: not found")
	ErrBadState  = errors.New("expenses: invalid state transition")
	ErrOverdraw  = errors.New("expenses: transaction would overdraw the float")
	ErrFloatBusy = errors.New("expenses: float is not active")
)

type Repo struct{ pool *database.Pool }

func New(pool *database.Pool) *Repo { return &Repo{pool: pool} }
