package procurement

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/japharyroman/fuelgrid-os/internal/database"
)

const (
	POStatusDraft             = "draft"
	POStatusSubmitted         = "submitted"
	POStatusConfirmed         = "confirmed"
	POStatusPartiallyReceived = "partially_received"
	POStatusReceived          = "received"
	POStatusClosed            = "closed"
	POStatusCancelled         = "cancelled"
)

type PurchaseOrder struct {
	ID                   uuid.UUID
	TenantID             uuid.UUID
	StationID            uuid.UUID
	SupplierID           uuid.UUID
	ExpectedDeliveryDate *time.Time
	Status               string
	RaisedBy             uuid.UUID
	SubmittedBy          *uuid.UUID
	SubmittedAt          *time.Time
	ConfirmedBy          *uuid.UUID
	ConfirmedAt          *time.Time
	CancelledBy          *uuid.UUID
	CancelledAt          *time.Time
	ClosedBy             *uuid.UUID
	ClosedAt             *time.Time
	Notes                *string
	CreatedAt            time.Time
	UpdatedAt            time.Time
	Lines                []PurchaseOrderLine
}

type PurchaseOrderLine struct {
	ID              uuid.UUID
	TenantID        uuid.UUID
	PurchaseOrderID uuid.UUID
	ProductID       uuid.UUID
	OrderedLitres   float64
	UnitPrice       string
	ReceivedLitres  float64
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

type PurchaseOrderLineInput struct {
	ProductID     uuid.UUID
	OrderedLitres float64
	UnitPrice     string
}

type PurchaseOrderInput struct {
	StationID            uuid.UUID
	SupplierID           uuid.UUID
	ExpectedDeliveryDate *time.Time
	Notes                *string
	Lines                []PurchaseOrderLineInput
	RaisedBy             uuid.UUID
}

type PurchaseOrderUpdateInput struct {
	ExpectedDeliveryDate *time.Time
	ExpectedDateSet      bool
	Notes                *string
	NotesSet             bool
	Lines                []PurchaseOrderLineInput
	LinesSet             bool
}

type PurchaseOrderFilter struct {
	StationIDs []uuid.UUID
	SupplierID *uuid.UUID
	Status     *string
}

const purchaseOrderColumns = `
    id, tenant_id, station_id, supplier_id, expected_delivery_date, status,
    raised_by, submitted_by, submitted_at, confirmed_by, confirmed_at,
    cancelled_by, cancelled_at, closed_by, closed_at, notes, created_at, updated_at
`

const purchaseOrderLineColumns = `
    id, tenant_id, purchase_order_id, product_id, ordered_litres,
    unit_price::text, received_litres, created_at, updated_at
`

func scanPurchaseOrder(row pgx.Row, po *PurchaseOrder) error {
	return row.Scan(
		&po.ID, &po.TenantID, &po.StationID, &po.SupplierID, &po.ExpectedDeliveryDate, &po.Status,
		&po.RaisedBy, &po.SubmittedBy, &po.SubmittedAt, &po.ConfirmedBy, &po.ConfirmedAt,
		&po.CancelledBy, &po.CancelledAt, &po.ClosedBy, &po.ClosedAt, &po.Notes, &po.CreatedAt, &po.UpdatedAt,
	)
}

func scanPurchaseOrderLine(row pgx.Row, ln *PurchaseOrderLine) error {
	return row.Scan(
		&ln.ID, &ln.TenantID, &ln.PurchaseOrderID, &ln.ProductID, &ln.OrderedLitres,
		&ln.UnitPrice, &ln.ReceivedLitres, &ln.CreatedAt, &ln.UpdatedAt,
	)
}

func (r *Repo) ListPurchaseOrders(ctx context.Context, tenantID uuid.UUID, f PurchaseOrderFilter) ([]PurchaseOrder, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT `+purchaseOrderColumns+`
		FROM purchase_orders
		WHERE tenant_id = $1
		  AND ($2::uuid[] IS NULL OR station_id = ANY($2::uuid[]))
		  AND ($3::uuid IS NULL OR supplier_id = $3)
		  AND ($4::text IS NULL OR status = $4)
		ORDER BY created_at DESC
	`, tenantID, database.UUIDStrings(f.StationIDs), f.SupplierID, f.Status)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []PurchaseOrder{}
	for rows.Next() {
		var po PurchaseOrder
		if err := scanPurchaseOrder(rows, &po); err != nil {
			return nil, err
		}
		lines, err := r.listPurchaseOrderLines(ctx, r.pool, tenantID, po.ID)
		if err != nil {
			return nil, err
		}
		po.Lines = lines
		out = append(out, po)
	}
	return out, rows.Err()
}

func (r *Repo) GetPurchaseOrder(ctx context.Context, tenantID, id uuid.UUID) (*PurchaseOrder, error) {
	po, err := r.getPurchaseOrder(ctx, r.pool, tenantID, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return po, err
}

func (r *Repo) getPurchaseOrder(ctx context.Context, q pgxQuerier, tenantID, id uuid.UUID) (*PurchaseOrder, error) {
	var po PurchaseOrder
	if err := scanPurchaseOrder(q.QueryRow(ctx, `
		SELECT `+purchaseOrderColumns+`
		FROM purchase_orders
		WHERE tenant_id = $1 AND id = $2
	`, tenantID, id), &po); err != nil {
		return nil, err
	}
	lines, err := r.listPurchaseOrderLines(ctx, q, tenantID, id)
	if err != nil {
		return nil, err
	}
	po.Lines = lines
	return &po, nil
}

func (r *Repo) CreatePurchaseOrder(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID, in PurchaseOrderInput) (*PurchaseOrder, error) {
	var active bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM suppliers
			WHERE tenant_id = $1 AND id = $2 AND status = 'active'
		)
	`, tenantID, in.SupplierID).Scan(&active); err != nil {
		return nil, err
	}
	if !active {
		return nil, ErrSupplierUnavailable
	}

	var po PurchaseOrder
	if err := scanPurchaseOrder(tx.QueryRow(ctx, `
		INSERT INTO purchase_orders
		    (tenant_id, station_id, supplier_id, expected_delivery_date, raised_by, notes)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING `+purchaseOrderColumns,
		tenantID, in.StationID, in.SupplierID, in.ExpectedDeliveryDate, in.RaisedBy, in.Notes,
	), &po); err != nil {
		return nil, err
	}
	if err := r.insertPurchaseOrderLines(ctx, tx, tenantID, po.ID, in.Lines); err != nil {
		return nil, err
	}
	lines, err := r.listPurchaseOrderLines(ctx, tx, tenantID, po.ID)
	if err != nil {
		return nil, err
	}
	po.Lines = lines
	return &po, nil
}

func (r *Repo) UpdatePurchaseOrderDraft(ctx context.Context, tx pgx.Tx, tenantID, id uuid.UUID, in PurchaseOrderUpdateInput) (*PurchaseOrder, error) {
	var po PurchaseOrder
	err := scanPurchaseOrder(tx.QueryRow(ctx, `
		UPDATE purchase_orders
		SET expected_delivery_date = CASE WHEN $3 THEN $4 ELSE expected_delivery_date END,
		    notes                  = CASE WHEN $5 THEN $6 ELSE notes END
		WHERE tenant_id = $1 AND id = $2 AND status = 'draft'
		RETURNING `+purchaseOrderColumns,
		tenantID, id, in.ExpectedDateSet, in.ExpectedDeliveryDate, in.NotesSet, in.Notes,
	), &po)
	if errors.Is(err, pgx.ErrNoRows) {
		if _, getErr := r.GetPurchaseOrder(ctx, tenantID, id); errors.Is(getErr, ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, ErrPurchaseOrderNotDraft
	}
	if err != nil {
		return nil, err
	}
	if in.LinesSet {
		if _, err := tx.Exec(ctx, `
			DELETE FROM purchase_order_lines
			WHERE tenant_id = $1 AND purchase_order_id = $2
		`, tenantID, id); err != nil {
			return nil, err
		}
		if err := r.insertPurchaseOrderLines(ctx, tx, tenantID, id, in.Lines); err != nil {
			return nil, err
		}
	}
	lines, err := r.listPurchaseOrderLines(ctx, tx, tenantID, id)
	if err != nil {
		return nil, err
	}
	po.Lines = lines
	return &po, nil
}

func (r *Repo) TransitionPurchaseOrder(ctx context.Context, tx pgx.Tx, tenantID, id, actorID uuid.UUID, fromStatus, toStatus string) (*PurchaseOrder, error) {
	var po PurchaseOrder
	err := scanPurchaseOrder(tx.QueryRow(ctx, `
		UPDATE purchase_orders
		SET status = $4,
		    submitted_by = CASE WHEN $4 = 'submitted' THEN $5 ELSE submitted_by END,
		    submitted_at = CASE WHEN $4 = 'submitted' THEN now() ELSE submitted_at END,
		    confirmed_by = CASE WHEN $4 = 'confirmed' THEN $5 ELSE confirmed_by END,
		    confirmed_at = CASE WHEN $4 = 'confirmed' THEN now() ELSE confirmed_at END,
		    cancelled_by = CASE WHEN $4 = 'cancelled' THEN $5 ELSE cancelled_by END,
		    cancelled_at = CASE WHEN $4 = 'cancelled' THEN now() ELSE cancelled_at END,
		    closed_by    = CASE WHEN $4 = 'closed' THEN $5 ELSE closed_by END,
		    closed_at    = CASE WHEN $4 = 'closed' THEN now() ELSE closed_at END
		WHERE tenant_id = $1 AND id = $2 AND status = $3
		RETURNING `+purchaseOrderColumns,
		tenantID, id, fromStatus, toStatus, actorID,
	), &po)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrInvalidTransition
	}
	if err != nil {
		return nil, err
	}
	lines, err := r.listPurchaseOrderLines(ctx, tx, tenantID, id)
	if err != nil {
		return nil, err
	}
	po.Lines = lines
	return &po, nil
}

func ValidPurchaseOrderTransition(fromStatus, toStatus string) bool {
	switch fromStatus {
	case POStatusDraft:
		return toStatus == POStatusSubmitted || toStatus == POStatusCancelled
	case POStatusSubmitted:
		return toStatus == POStatusConfirmed || toStatus == POStatusCancelled
	case POStatusConfirmed:
		return toStatus == POStatusCancelled
	case POStatusReceived:
		return toStatus == POStatusClosed
	default:
		return false
	}
}

func (r *Repo) listPurchaseOrderLines(ctx context.Context, q pgxQuerier, tenantID, poID uuid.UUID) ([]PurchaseOrderLine, error) {
	rows, err := q.Query(ctx, `
		SELECT `+purchaseOrderLineColumns+`
		FROM purchase_order_lines
		WHERE tenant_id = $1 AND purchase_order_id = $2
		ORDER BY created_at, id
	`, tenantID, poID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PurchaseOrderLine{}
	for rows.Next() {
		var ln PurchaseOrderLine
		if err := scanPurchaseOrderLine(rows, &ln); err != nil {
			return nil, err
		}
		out = append(out, ln)
	}
	return out, rows.Err()
}

func (r *Repo) insertPurchaseOrderLines(ctx context.Context, tx pgx.Tx, tenantID, poID uuid.UUID, lines []PurchaseOrderLineInput) error {
	for _, ln := range lines {
		if _, err := tx.Exec(ctx, `
			INSERT INTO purchase_order_lines
			    (tenant_id, purchase_order_id, product_id, ordered_litres, unit_price)
			VALUES ($1, $2, $3, $4, $5::numeric)
		`, tenantID, poID, ln.ProductID, ln.OrderedLitres, ln.UnitPrice); err != nil {
			return err
		}
	}
	return nil
}
