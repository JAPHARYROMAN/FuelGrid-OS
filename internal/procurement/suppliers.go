package procurement

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/japharyroman/fuelgrid-os/internal/database"
)

type Supplier struct {
	ID               uuid.UUID
	TenantID         uuid.UUID
	Code             string
	Name             string
	ContactName      *string
	ContactEmail     *string
	ContactPhone     *string
	PaymentTermsDays int
	Status           string
	DeactivatedAt    *time.Time
	CreatedAt        time.Time
	UpdatedAt        time.Time
	ProductIDs       []uuid.UUID
}

type SupplierInput struct {
	Code             string
	Name             string
	ContactName      *string
	ContactEmail     *string
	ContactPhone     *string
	PaymentTermsDays int
	ProductIDs       []uuid.UUID
}

type SupplierUpdateInput struct {
	Code             *string
	Name             *string
	ContactName      *string
	ContactEmail     *string
	ContactPhone     *string
	PaymentTermsDays *int
	Status           *string
	ProductIDs       []uuid.UUID
	ProductIDsSet    bool
}

const supplierColumns = `
    id, tenant_id, code, name, contact_name, contact_email, contact_phone,
    payment_terms_days, status, deactivated_at, created_at, updated_at
`

func scanSupplier(row pgx.Row, s *Supplier) error {
	return row.Scan(
		&s.ID, &s.TenantID, &s.Code, &s.Name, &s.ContactName, &s.ContactEmail, &s.ContactPhone,
		&s.PaymentTermsDays, &s.Status, &s.DeactivatedAt, &s.CreatedAt, &s.UpdatedAt,
	)
}

func (r *Repo) ListSuppliers(ctx context.Context, tenantID uuid.UUID) ([]Supplier, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT `+supplierColumns+`
		FROM suppliers
		WHERE tenant_id = $1
		ORDER BY name
	`, tenantID)
	if err != nil {
		return nil, err
	}

	out, err := scanSupplierRows(rows)
	if err != nil {
		return nil, err
	}
	return r.withSupplierProductIDs(ctx, r.pool, tenantID, out)
}

// ListSuppliersPage is the paginated variant of ListSuppliers (REL-REPO). name
// is not unique, so id is appended as a deterministic tiebreaker.
func (r *Repo) ListSuppliersPage(ctx context.Context, tenantID uuid.UUID, limit, offset int) ([]Supplier, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT `+supplierColumns+`
		FROM suppliers
		WHERE tenant_id = $1
		ORDER BY name, id
		LIMIT $2 OFFSET $3
	`, tenantID, limit, offset)
	if err != nil {
		return nil, err
	}

	out, err := scanSupplierRows(rows)
	if err != nil {
		return nil, err
	}
	return r.withSupplierProductIDs(ctx, r.pool, tenantID, out)
}

func (r *Repo) GetSupplier(ctx context.Context, tenantID, id uuid.UUID) (*Supplier, error) {
	s, err := r.getSupplier(ctx, r.pool, tenantID, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return s, err
}

func (r *Repo) getSupplier(ctx context.Context, q pgxQuerier, tenantID, id uuid.UUID) (*Supplier, error) {
	var s Supplier
	if err := scanSupplier(q.QueryRow(ctx, `
		SELECT `+supplierColumns+`
		FROM suppliers
		WHERE tenant_id = $1 AND id = $2
	`, tenantID, id), &s); err != nil {
		return nil, err
	}
	ids, err := r.supplierProductIDs(ctx, q, tenantID, id)
	if err != nil {
		return nil, err
	}
	s.ProductIDs = ids
	return &s, nil
}

type pgxQuerier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

func scanSupplierRows(rows pgx.Rows) ([]Supplier, error) {
	defer rows.Close()

	out := []Supplier{}
	for rows.Next() {
		var s Supplier
		if err := scanSupplier(rows, &s); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (r *Repo) withSupplierProductIDs(ctx context.Context, q pgxQuerier, tenantID uuid.UUID, suppliers []Supplier) ([]Supplier, error) {
	if len(suppliers) == 0 {
		return suppliers, nil
	}

	supplierIDs := make([]uuid.UUID, 0, len(suppliers))
	for i := range suppliers {
		supplierIDs = append(supplierIDs, suppliers[i].ID)
	}

	productIDs, err := r.supplierProductIDsBySupplier(ctx, q, tenantID, supplierIDs)
	if err != nil {
		return nil, err
	}
	for i := range suppliers {
		ids := productIDs[suppliers[i].ID]
		if ids == nil {
			ids = []uuid.UUID{}
		}
		suppliers[i].ProductIDs = ids
	}
	return suppliers, nil
}

func (r *Repo) CreateSupplier(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID, in SupplierInput) (*Supplier, error) {
	var s Supplier
	if err := scanSupplier(tx.QueryRow(ctx, `
		INSERT INTO suppliers
		    (tenant_id, code, name, contact_name, contact_email, contact_phone, payment_terms_days)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING `+supplierColumns,
		tenantID, in.Code, in.Name, in.ContactName, in.ContactEmail, in.ContactPhone, in.PaymentTermsDays,
	), &s); err != nil {
		return nil, err
	}
	if err := r.replaceSupplierProducts(ctx, tx, tenantID, s.ID, in.ProductIDs); err != nil {
		return nil, err
	}
	s.ProductIDs = append([]uuid.UUID(nil), in.ProductIDs...)
	return &s, nil
}

func (r *Repo) UpdateSupplier(ctx context.Context, tx pgx.Tx, tenantID, id uuid.UUID, in SupplierUpdateInput) (*Supplier, error) {
	var s Supplier
	err := scanSupplier(tx.QueryRow(ctx, `
		UPDATE suppliers
		SET code               = COALESCE($3, code),
		    name               = COALESCE($4, name),
		    contact_name       = COALESCE($5, contact_name),
		    contact_email      = COALESCE($6, contact_email),
		    contact_phone      = COALESCE($7, contact_phone),
		    payment_terms_days = COALESCE($8, payment_terms_days),
		    status             = COALESCE($9, status),
		    deactivated_at     = CASE WHEN $9 = 'deactivated' THEN COALESCE(deactivated_at, now()) ELSE deactivated_at END
		WHERE tenant_id = $1 AND id = $2
		RETURNING `+supplierColumns,
		tenantID, id, in.Code, in.Name, in.ContactName, in.ContactEmail,
		in.ContactPhone, in.PaymentTermsDays, in.Status,
	), &s)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if in.ProductIDsSet {
		if err := r.replaceSupplierProducts(ctx, tx, tenantID, id, in.ProductIDs); err != nil {
			return nil, err
		}
		s.ProductIDs = append([]uuid.UUID(nil), in.ProductIDs...)
	} else {
		ids, err := r.supplierProductIDs(ctx, tx, tenantID, id)
		if err != nil {
			return nil, err
		}
		s.ProductIDs = ids
	}
	return &s, nil
}

func (r *Repo) DeactivateSupplier(ctx context.Context, tx pgx.Tx, tenantID, id uuid.UUID) (*Supplier, error) {
	var open int
	if err := tx.QueryRow(ctx, `
		SELECT count(*)
		FROM purchase_orders
		WHERE tenant_id = $1 AND supplier_id = $2
		  AND status IN ('draft', 'submitted', 'confirmed', 'partially_received')
	`, tenantID, id).Scan(&open); err != nil {
		return nil, err
	}
	if open > 0 {
		return nil, ErrSupplierInUse
	}
	status := "deactivated"
	return r.UpdateSupplier(ctx, tx, tenantID, id, SupplierUpdateInput{Status: &status})
}

func (r *Repo) supplierProductIDs(ctx context.Context, q pgxQuerier, tenantID, supplierID uuid.UUID) ([]uuid.UUID, error) {
	rows, err := q.Query(ctx, `
		SELECT product_id
		FROM supplier_products
		WHERE tenant_id = $1 AND supplier_id = $2
		ORDER BY product_id
	`, tenantID, supplierID)
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

func (r *Repo) supplierProductIDsBySupplier(ctx context.Context, q pgxQuerier, tenantID uuid.UUID, supplierIDs []uuid.UUID) (map[uuid.UUID][]uuid.UUID, error) {
	if len(supplierIDs) == 0 {
		return map[uuid.UUID][]uuid.UUID{}, nil
	}
	rows, err := q.Query(ctx, `
		SELECT supplier_id, product_id
		FROM supplier_products
		WHERE tenant_id = $1
		  AND supplier_id = ANY($2::uuid[])
		ORDER BY supplier_id, product_id
	`, tenantID, database.UUIDStrings(supplierIDs))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make(map[uuid.UUID][]uuid.UUID, len(supplierIDs))
	for rows.Next() {
		var supplierID, productID uuid.UUID
		if err := rows.Scan(&supplierID, &productID); err != nil {
			return nil, err
		}
		out[supplierID] = append(out[supplierID], productID)
	}
	return out, rows.Err()
}

func (r *Repo) replaceSupplierProducts(ctx context.Context, tx pgx.Tx, tenantID, supplierID uuid.UUID, productIDs []uuid.UUID) error {
	if _, err := tx.Exec(ctx, `
		DELETE FROM supplier_products
		WHERE tenant_id = $1 AND supplier_id = $2
	`, tenantID, supplierID); err != nil {
		return err
	}
	for _, productID := range productIDs {
		if _, err := tx.Exec(ctx, `
			INSERT INTO supplier_products (tenant_id, supplier_id, product_id)
			VALUES ($1, $2, $3)
			ON CONFLICT (supplier_id, product_id) DO NOTHING
		`, tenantID, supplierID, productID); err != nil {
			return err
		}
	}
	return nil
}
