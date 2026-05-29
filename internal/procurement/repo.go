// Package procurement owns the supplier, purchase-order, invoice, and match
// data for Phase 5. Goods receipt stock posting stays in inventory because it
// mutates the tank ledger.
package procurement

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/japharyroman/fuelgrid-os/internal/database"
)

type Repo struct{ pool *database.Pool }

func New(pool *database.Pool) *Repo { return &Repo{pool: pool} }

var (
	ErrNotFound              = errors.New("procurement: not found")
	ErrSupplierInUse         = errors.New("procurement: supplier has open purchase orders")
	ErrSupplierUnavailable   = errors.New("procurement: supplier is not active")
	ErrPurchaseOrderNotDraft = errors.New("procurement: purchase order is not draft")
	ErrInvalidTransition     = errors.New("procurement: invalid purchase order transition")
	ErrInvoiceNotMatched     = errors.New("procurement: supplier invoice is not matched")
	ErrInvoiceHasDiscrepancy = errors.New("procurement: supplier invoice has open discrepancies")
	ErrAlreadyResolved       = errors.New("procurement: discrepancy already resolved")
)

func scanTimePtr(t *time.Time) *time.Time { return t }

func uuidSliceFromRows(ctx context.Context, q database.Querier, sql string, args ...any) ([]uuid.UUID, error) {
	rows, err := q.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []uuid.UUID{}
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}
