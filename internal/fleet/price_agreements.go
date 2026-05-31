package fleet

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/japharyroman/fuelgrid-os/internal/database"
)

type PriceAgreement struct {
	ID            uuid.UUID
	TenantID      uuid.UUID
	CustomerID    uuid.UUID
	ProductID     uuid.UUID
	StationID     *uuid.UUID
	PriceType     string
	FixedPrice    *string
	Discount      *string
	Markup        *string
	EffectiveFrom time.Time
	EffectiveTo   *time.Time
	Status        string
	Version       int
	ApprovedBy    *uuid.UUID
	CreatedBy     uuid.UUID
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

type PriceAgreementInput struct {
	CustomerID    uuid.UUID
	ProductID     uuid.UUID
	StationID     *uuid.UUID
	PriceType     string
	FixedPrice    *string
	Discount      *string
	Markup        *string
	EffectiveFrom time.Time
	EffectiveTo   *time.Time
	CreatedBy     uuid.UUID
}

const priceAgreementColumns = `
    id, tenant_id, customer_id, product_id, station_id, price_type,
    fixed_price::text, discount::text, markup::text, effective_from, effective_to,
    status, version, approved_by, created_by, created_at, updated_at
`

func scanPriceAgreement(row pgx.Row, a *PriceAgreement) error {
	return row.Scan(
		&a.ID, &a.TenantID, &a.CustomerID, &a.ProductID, &a.StationID, &a.PriceType,
		&a.FixedPrice, &a.Discount, &a.Markup, &a.EffectiveFrom, &a.EffectiveTo,
		&a.Status, &a.Version, &a.ApprovedBy, &a.CreatedBy, &a.CreatedAt, &a.UpdatedAt,
	)
}

func (r *Repo) CreatePriceAgreement(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID, in PriceAgreementInput) (*PriceAgreement, error) {
	pt := in.PriceType
	if pt == "" {
		pt = "fixed"
	}
	var a PriceAgreement
	if err := scanPriceAgreement(tx.QueryRow(ctx, `
		INSERT INTO customer_price_agreements
		    (tenant_id, customer_id, product_id, station_id, price_type, fixed_price, discount, markup, effective_from, effective_to, created_by)
		VALUES ($1, $2, $3, $4, $5, $6::numeric, $7::numeric, $8::numeric, $9, $10, $11)
		RETURNING `+priceAgreementColumns,
		tenantID, in.CustomerID, in.ProductID, in.StationID, pt,
		nullableMoney(deref(in.FixedPrice)), nullableMoney(deref(in.Discount)), nullableMoney(deref(in.Markup)),
		in.EffectiveFrom, in.EffectiveTo, in.CreatedBy,
	), &a); err != nil {
		return nil, err
	}
	return &a, nil
}

func (r *Repo) GetPriceAgreement(ctx context.Context, tenantID, id uuid.UUID) (*PriceAgreement, error) {
	var a PriceAgreement
	err := scanPriceAgreement(r.pool.QueryRow(ctx, `SELECT `+priceAgreementColumns+` FROM customer_price_agreements WHERE tenant_id = $1 AND id = $2`, tenantID, id), &a)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &a, nil
}

func (r *Repo) ListPriceAgreements(ctx context.Context, tenantID, customerID uuid.UUID) ([]PriceAgreement, error) {
	var custFilter *uuid.UUID
	if customerID != uuid.Nil {
		custFilter = &customerID
	}
	rows, err := r.pool.Query(ctx, `
		SELECT `+priceAgreementColumns+` FROM customer_price_agreements
		WHERE tenant_id = $1 AND ($2::uuid IS NULL OR customer_id = $2)
		ORDER BY created_at DESC
	`, tenantID, custFilter)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PriceAgreement{}
	for rows.Next() {
		var a PriceAgreement
		if err := scanPriceAgreement(rows, &a); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// ListPriceAgreementsPage is the paginated variant of ListPriceAgreements
// (REL-REPO). created_at is not unique, so id is appended as a tiebreaker.
func (r *Repo) ListPriceAgreementsPage(ctx context.Context, tenantID, customerID uuid.UUID, limit, offset int) ([]PriceAgreement, error) {
	var custFilter *uuid.UUID
	if customerID != uuid.Nil {
		custFilter = &customerID
	}
	rows, err := r.pool.Query(ctx, `
		SELECT `+priceAgreementColumns+` FROM customer_price_agreements
		WHERE tenant_id = $1 AND ($2::uuid IS NULL OR customer_id = $2)
		ORDER BY created_at DESC, id
		LIMIT $3 OFFSET $4
	`, tenantID, custFilter, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PriceAgreement{}
	for rows.Next() {
		var a PriceAgreement
		if err := scanPriceAgreement(rows, &a); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// TransitionPriceAgreement moves an agreement through its lifecycle. Activating
// is guarded by the partial unique indexes (one active per scope).
func (r *Repo) TransitionPriceAgreement(ctx context.Context, tx pgx.Tx, tenantID, id uuid.UUID, to string, approverID *uuid.UUID) (*PriceAgreement, error) {
	from := map[string]string{
		"approved":  "draft",
		"active":    "approved",
		"expired":   "active",
		"cancelled": "", // allowed from any non-terminal
	}[to]
	var a PriceAgreement
	var err error
	if to == "cancelled" {
		err = scanPriceAgreement(tx.QueryRow(ctx, `
			UPDATE customer_price_agreements SET status = 'cancelled'
			WHERE tenant_id = $1 AND id = $2 AND status NOT IN ('expired', 'cancelled')
			RETURNING `+priceAgreementColumns, tenantID, id), &a)
	} else {
		err = scanPriceAgreement(tx.QueryRow(ctx, `
			UPDATE customer_price_agreements
			SET status = $3, approved_by = COALESCE($4, approved_by)
			WHERE tenant_id = $1 AND id = $2 AND status = $5
			RETURNING `+priceAgreementColumns, tenantID, id, to, approverID, from), &a)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrBadState
	}
	if isUniqueViolation(err) {
		return nil, ErrConflict
	}
	if err != nil {
		return nil, err
	}
	return &a, nil
}

// ResolveCustomerPrice returns the unit price a customer pays for a product at a
// station, applying the active agreement (station-specific first, then
// customer-wide) against the supplied retail price. Returns ("", false) when no
// agreement applies, so the caller falls back to the retail price.
func (r *Repo) ResolveCustomerPrice(ctx context.Context, q database.Querier, tenantID, customerID, productID, stationID uuid.UUID, retailPrice string) (string, bool, error) {
	var price string
	err := q.QueryRow(ctx, `
		SELECT CASE price_type
		         WHEN 'fixed'    THEN fixed_price
		         WHEN 'discount' THEN GREATEST($5::numeric - COALESCE(discount, 0), 0)
		         WHEN 'markup'   THEN $5::numeric + COALESCE(markup, 0)
		       END::text
		FROM customer_price_agreements
		WHERE tenant_id = $1 AND customer_id = $2 AND product_id = $3 AND status = 'active'
		  AND effective_from <= CURRENT_DATE AND (effective_to IS NULL OR effective_to >= CURRENT_DATE)
		  AND (station_id = $4 OR station_id IS NULL)
		ORDER BY station_id NULLS LAST
		LIMIT 1
	`, tenantID, customerID, productID, stationID, retailPrice).Scan(&price)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return price, true, nil
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
