package procurement

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type SupplierInvoice struct {
	ID              uuid.UUID
	TenantID        uuid.UUID
	SupplierID      uuid.UUID
	PurchaseOrderID uuid.UUID
	StationID       uuid.UUID
	InvoiceNumber   string
	Status          string
	ReceivedAt      time.Time
	DueDate         *time.Time
	TotalAmount     string
	RecordedBy      uuid.UUID
	ApprovedBy      *uuid.UUID
	ApprovedAt      *time.Time
	Notes           *string
	CreatedAt       time.Time
	UpdatedAt       time.Time
	Lines           []SupplierInvoiceLine
	Discrepancies   []ProcurementDiscrepancy
}

type SupplierInvoiceLine struct {
	ID                uuid.UUID
	TenantID          uuid.UUID
	SupplierInvoiceID uuid.UUID
	PurchaseOrderID   uuid.UUID
	POLineID          uuid.UUID
	DeliveryID        *uuid.UUID
	ProductID         uuid.UUID
	InvoicedLitres    float64
	UnitPrice         string
	Amount            string
	CreatedAt         time.Time
}

type ProcurementDiscrepancy struct {
	ID                uuid.UUID
	TenantID          uuid.UUID
	SupplierInvoiceID uuid.UUID
	PurchaseOrderID   uuid.UUID
	DeliveryID        *uuid.UUID
	POLineID          *uuid.UUID
	Type              string
	Severity          string
	Detail            string
	VarianceLitres    *float64
	VarianceAmount    *string
	Status            string
	RaisedAt          time.Time
	ResolvedBy        *uuid.UUID
	ResolvedAt        *time.Time
}

type SupplierInvoiceInput struct {
	PurchaseOrderID uuid.UUID
	InvoiceNumber   string
	ReceivedAt      *time.Time
	DueDate         *time.Time
	Notes           *string
	RecordedBy      uuid.UUID
	Lines           []SupplierInvoiceLineInput
}

type SupplierInvoiceLineInput struct {
	POLineID       uuid.UUID
	DeliveryID     *uuid.UUID
	InvoicedLitres float64
	UnitPrice      string
	Amount         *string
}

const supplierInvoiceColumns = `
    id, tenant_id, supplier_id, purchase_order_id, station_id, invoice_number,
    status, received_at, due_date, total_amount::text, recorded_by,
    approved_by, approved_at, notes, created_at, updated_at
`

const supplierInvoiceLineColumns = `
    id, tenant_id, supplier_invoice_id, purchase_order_id, po_line_id, delivery_id,
    product_id, invoiced_litres, unit_price::text, amount::text, created_at
`

const procurementDiscrepancyColumns = `
    id, tenant_id, supplier_invoice_id, purchase_order_id, delivery_id, po_line_id,
    type, severity, detail, variance_litres, variance_amount::text, status,
    raised_at, resolved_by, resolved_at
`

func scanSupplierInvoice(row pgx.Row, inv *SupplierInvoice) error {
	return row.Scan(
		&inv.ID, &inv.TenantID, &inv.SupplierID, &inv.PurchaseOrderID, &inv.StationID, &inv.InvoiceNumber,
		&inv.Status, &inv.ReceivedAt, &inv.DueDate, &inv.TotalAmount, &inv.RecordedBy,
		&inv.ApprovedBy, &inv.ApprovedAt, &inv.Notes, &inv.CreatedAt, &inv.UpdatedAt,
	)
}

func scanSupplierInvoiceLine(row pgx.Row, ln *SupplierInvoiceLine) error {
	return row.Scan(
		&ln.ID, &ln.TenantID, &ln.SupplierInvoiceID, &ln.PurchaseOrderID, &ln.POLineID, &ln.DeliveryID,
		&ln.ProductID, &ln.InvoicedLitres, &ln.UnitPrice, &ln.Amount, &ln.CreatedAt,
	)
}

func scanProcurementDiscrepancy(row pgx.Row, d *ProcurementDiscrepancy) error {
	return row.Scan(
		&d.ID, &d.TenantID, &d.SupplierInvoiceID, &d.PurchaseOrderID, &d.DeliveryID, &d.POLineID,
		&d.Type, &d.Severity, &d.Detail, &d.VarianceLitres, &d.VarianceAmount, &d.Status,
		&d.RaisedAt, &d.ResolvedBy, &d.ResolvedAt,
	)
}

func (r *Repo) RecordSupplierInvoice(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID, in SupplierInvoiceInput) (*SupplierInvoice, error) {
	var supplierID, stationID uuid.UUID
	var poStatus string
	err := tx.QueryRow(ctx, `
		SELECT supplier_id, station_id, status
		FROM purchase_orders
		WHERE tenant_id = $1 AND id = $2
	`, tenantID, in.PurchaseOrderID).Scan(&supplierID, &stationID, &poStatus)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if poStatus == POStatusDraft || poStatus == POStatusCancelled {
		return nil, ErrInvalidTransition
	}

	var inv SupplierInvoice
	if err := scanSupplierInvoice(tx.QueryRow(ctx, `
		INSERT INTO supplier_invoices
		    (tenant_id, supplier_id, purchase_order_id, station_id, invoice_number,
		     received_at, due_date, recorded_by, notes)
		VALUES ($1, $2, $3, $4, $5, COALESCE($6::timestamptz, now()), $7, $8, $9)
		RETURNING `+supplierInvoiceColumns,
		tenantID, supplierID, in.PurchaseOrderID, stationID, in.InvoiceNumber,
		in.ReceivedAt, in.DueDate, in.RecordedBy, in.Notes,
	), &inv); err != nil {
		return nil, err
	}

	for _, ln := range in.Lines {
		tag, err := tx.Exec(ctx, `
			INSERT INTO supplier_invoice_lines
			    (tenant_id, supplier_invoice_id, purchase_order_id, po_line_id, delivery_id,
			     product_id, invoiced_litres, unit_price, amount)
			SELECT $1, $2, $3, pol.id, $5, pol.product_id, $6, $7::numeric,
			       COALESCE($8::numeric, ROUND($6::numeric * $7::numeric, 2))
			FROM purchase_order_lines pol
			WHERE pol.tenant_id = $1 AND pol.purchase_order_id = $3 AND pol.id = $4
		`, tenantID, inv.ID, in.PurchaseOrderID, ln.POLineID, ln.DeliveryID,
			ln.InvoicedLitres, ln.UnitPrice, ln.Amount)
		if err != nil {
			return nil, err
		}
		if tag.RowsAffected() == 0 {
			return nil, ErrNotFound
		}
	}

	if _, err := tx.Exec(ctx, `
		UPDATE supplier_invoices inv
		SET total_amount = (
			SELECT COALESCE(SUM(amount), 0)
			FROM supplier_invoice_lines
			WHERE tenant_id = inv.tenant_id AND supplier_invoice_id = inv.id
		)
		WHERE inv.tenant_id = $1 AND inv.id = $2
	`, tenantID, inv.ID); err != nil {
		return nil, err
	}

	if err := r.raiseInvoiceDiscrepancies(ctx, tx, tenantID, inv.ID); err != nil {
		return nil, err
	}
	if err := r.refreshInvoiceMatchStatus(ctx, tx, tenantID, inv.ID); err != nil {
		return nil, err
	}
	return r.getSupplierInvoice(ctx, tx, tenantID, inv.ID)
}

func (r *Repo) GetSupplierInvoice(ctx context.Context, tenantID, id uuid.UUID) (*SupplierInvoice, error) {
	inv, err := r.getSupplierInvoice(ctx, r.pool, tenantID, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return inv, err
}

func (r *Repo) getSupplierInvoice(ctx context.Context, q pgxQuerier, tenantID, id uuid.UUID) (*SupplierInvoice, error) {
	var inv SupplierInvoice
	if err := scanSupplierInvoice(q.QueryRow(ctx, `
		SELECT `+supplierInvoiceColumns+`
		FROM supplier_invoices
		WHERE tenant_id = $1 AND id = $2
	`, tenantID, id), &inv); err != nil {
		return nil, err
	}
	lines, err := r.listInvoiceLines(ctx, q, tenantID, id)
	if err != nil {
		return nil, err
	}
	discs, err := r.listInvoiceDiscrepancies(ctx, q, tenantID, id)
	if err != nil {
		return nil, err
	}
	inv.Lines = lines
	inv.Discrepancies = discs
	return &inv, nil
}

func (r *Repo) ApproveSupplierInvoice(ctx context.Context, tx pgx.Tx, tenantID, id, actorID uuid.UUID) (*SupplierInvoice, error) {
	open, err := r.OpenDiscrepancyCount(ctx, tx, tenantID, id)
	if err != nil {
		return nil, err
	}
	if open > 0 {
		return nil, ErrInvoiceHasDiscrepancy
	}
	// Separation of duties: the approver must not be the invoice's recorder.
	// Lock the row so the check and the transition are atomic.
	var recordedBy uuid.UUID
	var curStatus string
	if err := tx.QueryRow(ctx, `
		SELECT recorded_by, status FROM supplier_invoices
		WHERE tenant_id = $1 AND id = $2
		FOR UPDATE
	`, tenantID, id).Scan(&recordedBy, &curStatus); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if curStatus != "matched" {
		return nil, ErrInvoiceNotMatched
	}
	if recordedBy == actorID {
		return nil, ErrSelfApproval
	}
	var inv SupplierInvoice
	err = scanSupplierInvoice(tx.QueryRow(ctx, `
		UPDATE supplier_invoices
		SET status = 'approved', approved_by = $3, approved_at = now()
		WHERE tenant_id = $1 AND id = $2 AND status = 'matched'
		RETURNING `+supplierInvoiceColumns,
		tenantID, id, actorID,
	), &inv)
	if errors.Is(err, pgx.ErrNoRows) {
		before, getErr := r.GetSupplierInvoice(ctx, tenantID, id)
		if errors.Is(getErr, ErrNotFound) {
			return nil, ErrNotFound
		}
		if getErr != nil {
			return nil, getErr
		}
		if before.Status != "matched" {
			return nil, ErrInvoiceNotMatched
		}
		return nil, ErrInvoiceHasDiscrepancy
	}
	if err != nil {
		return nil, err
	}
	lines, err := r.listInvoiceLines(ctx, tx, tenantID, id)
	if err != nil {
		return nil, err
	}
	inv.Lines = lines
	discs, err := r.listInvoiceDiscrepancies(ctx, tx, tenantID, id)
	if err != nil {
		return nil, err
	}
	inv.Discrepancies = discs
	return &inv, nil
}

func (r *Repo) GetDiscrepancy(ctx context.Context, tenantID, id uuid.UUID) (*ProcurementDiscrepancy, error) {
	var d ProcurementDiscrepancy
	err := scanProcurementDiscrepancy(r.pool.QueryRow(ctx, `
		SELECT `+procurementDiscrepancyColumns+`
		FROM procurement_discrepancies
		WHERE tenant_id = $1 AND id = $2
	`, tenantID, id), &d)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &d, nil
}

func (r *Repo) ResolveDiscrepancy(ctx context.Context, tx pgx.Tx, tenantID, id, actorID uuid.UUID) (*ProcurementDiscrepancy, error) {
	var d ProcurementDiscrepancy
	err := scanProcurementDiscrepancy(tx.QueryRow(ctx, `
		UPDATE procurement_discrepancies
		SET status = 'resolved', resolved_by = $3, resolved_at = now()
		WHERE tenant_id = $1 AND id = $2 AND status = 'open'
		RETURNING `+procurementDiscrepancyColumns,
		tenantID, id, actorID,
	), &d)
	if errors.Is(err, pgx.ErrNoRows) {
		if _, getErr := r.GetDiscrepancy(ctx, tenantID, id); errors.Is(getErr, ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, ErrAlreadyResolved
	}
	if err != nil {
		return nil, err
	}
	if err := r.refreshInvoiceMatchStatus(ctx, tx, tenantID, d.SupplierInvoiceID); err != nil {
		return nil, err
	}
	return &d, nil
}

func (r *Repo) OpenDiscrepancyCount(ctx context.Context, q pgxQuerier, tenantID, invoiceID uuid.UUID) (int, error) {
	var n int
	err := q.QueryRow(ctx, `
		SELECT count(*)
		FROM procurement_discrepancies
		WHERE tenant_id = $1 AND supplier_invoice_id = $2 AND status = 'open'
	`, tenantID, invoiceID).Scan(&n)
	return n, err
}

func (r *Repo) raiseInvoiceDiscrepancies(ctx context.Context, tx pgx.Tx, tenantID, invoiceID uuid.UUID) error {
	if _, err := tx.Exec(ctx, `
		WITH receipt_totals AS (
			SELECT il.supplier_invoice_id, il.purchase_order_id, il.po_line_id,
			       NULL::uuid AS delivery_id,
			       il.invoiced_litres,
			       COALESCE(SUM(d.volume_litres), 0) AS received_litres
			FROM supplier_invoice_lines il
			LEFT JOIN deliveries d
			  ON d.tenant_id = il.tenant_id
			 AND d.purchase_order_id = il.purchase_order_id
			 AND d.po_line_id = il.po_line_id
			WHERE il.tenant_id = $1 AND il.supplier_invoice_id = $2
			GROUP BY il.supplier_invoice_id, il.purchase_order_id, il.po_line_id, il.invoiced_litres
		)
		INSERT INTO procurement_discrepancies
		    (tenant_id, supplier_invoice_id, purchase_order_id, delivery_id, po_line_id,
		     type, severity, detail, variance_litres)
		SELECT $1, supplier_invoice_id, purchase_order_id, delivery_id, po_line_id,
		       'quantity', 'blocking',
		       'invoice quantity differs from received litres',
		       invoiced_litres - received_litres
		FROM receipt_totals
		WHERE abs(invoiced_litres - received_litres) > greatest(received_litres * 0.005, 1)
	`, tenantID, invoiceID); err != nil {
		return err
	}

	_, err := tx.Exec(ctx, `
		INSERT INTO procurement_discrepancies
		    (tenant_id, supplier_invoice_id, purchase_order_id, delivery_id, po_line_id,
		     type, severity, detail, variance_amount)
		SELECT il.tenant_id, il.supplier_invoice_id, il.purchase_order_id, il.delivery_id, il.po_line_id,
		       'price', 'blocking',
		       'invoice unit price differs from purchase order price',
		       il.unit_price - pol.unit_price
		FROM supplier_invoice_lines il
		JOIN purchase_order_lines pol
		  ON pol.tenant_id = il.tenant_id
		 AND pol.purchase_order_id = il.purchase_order_id
		 AND pol.id = il.po_line_id
		WHERE il.tenant_id = $1 AND il.supplier_invoice_id = $2
		  AND abs(il.unit_price - pol.unit_price) > greatest(pol.unit_price * 0.005, 0.01)
	`, tenantID, invoiceID)
	return err
}

func (r *Repo) refreshInvoiceMatchStatus(ctx context.Context, tx pgx.Tx, tenantID, invoiceID uuid.UUID) error {
	var open int
	if err := tx.QueryRow(ctx, `
		SELECT count(*)
		FROM procurement_discrepancies
		WHERE tenant_id = $1 AND supplier_invoice_id = $2 AND status = 'open'
	`, tenantID, invoiceID).Scan(&open); err != nil {
		return err
	}
	status := "matched"
	if open > 0 {
		status = "discrepancy"
	}
	_, err := tx.Exec(ctx, `
		UPDATE supplier_invoices
		SET status = $3
		WHERE tenant_id = $1 AND id = $2 AND status <> 'approved'
	`, tenantID, invoiceID, status)
	return err
}

func (r *Repo) listInvoiceLines(ctx context.Context, q pgxQuerier, tenantID, invoiceID uuid.UUID) ([]SupplierInvoiceLine, error) {
	rows, err := q.Query(ctx, `
		SELECT `+supplierInvoiceLineColumns+`
		FROM supplier_invoice_lines
		WHERE tenant_id = $1 AND supplier_invoice_id = $2
		ORDER BY created_at, id
	`, tenantID, invoiceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []SupplierInvoiceLine{}
	for rows.Next() {
		var ln SupplierInvoiceLine
		if err := scanSupplierInvoiceLine(rows, &ln); err != nil {
			return nil, err
		}
		out = append(out, ln)
	}
	return out, rows.Err()
}

func (r *Repo) listInvoiceDiscrepancies(ctx context.Context, q pgxQuerier, tenantID, invoiceID uuid.UUID) ([]ProcurementDiscrepancy, error) {
	rows, err := q.Query(ctx, `
		SELECT `+procurementDiscrepancyColumns+`
		FROM procurement_discrepancies
		WHERE tenant_id = $1 AND supplier_invoice_id = $2
		ORDER BY raised_at DESC
	`, tenantID, invoiceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ProcurementDiscrepancy{}
	for rows.Next() {
		var d ProcurementDiscrepancy
		if err := scanProcurementDiscrepancy(rows, &d); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}
