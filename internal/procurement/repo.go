// Package procurement owns the supplier, purchase-order, invoice, and match
// data for Phase 5. Goods receipt stock posting stays in inventory because it
// mutates the tank ledger.
package procurement

import (
	"errors"

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
	ErrSelfApproval          = errors.New("procurement: approver cannot be the invoice recorder")
)
