package fleet

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type Authorization struct {
	ID              uuid.UUID
	TenantID        uuid.UUID
	CustomerID      uuid.UUID
	VehicleID       *uuid.UUID
	DriverID        *uuid.UUID
	CredentialID    *uuid.UUID
	StationID       uuid.UUID
	ProductID       *uuid.UUID
	RequestedAmount string
	ApprovedAmount  string
	Odometer        *string
	Status          string
	ExpiryAt        *time.Time
	Source          string
	ConsumedBy      *uuid.UUID
	CreatedBy       uuid.UUID
	CreatedAt       time.Time
}

type AuthRequest struct {
	CustomerID      uuid.UUID
	VehicleID       *uuid.UUID
	DriverID        *uuid.UUID
	CredentialID    *uuid.UUID
	StationID       uuid.UUID
	ProductID       *uuid.UUID
	RequestedAmount string
	Odometer        *string
	Source          string
}

const authColumns = `
    id, tenant_id, customer_id, vehicle_id, driver_id, credential_id, station_id, product_id,
    requested_amount::text, approved_amount::text, odometer::text, status, expiry_at, source,
    consumed_by, created_by, created_at
`

func scanAuth(row pgx.Row, a *Authorization) error {
	return row.Scan(
		&a.ID, &a.TenantID, &a.CustomerID, &a.VehicleID, &a.DriverID, &a.CredentialID, &a.StationID, &a.ProductID,
		&a.RequestedAmount, &a.ApprovedAmount, &a.Odometer, &a.Status, &a.ExpiryAt, &a.Source,
		&a.ConsumedBy, &a.CreatedBy, &a.CreatedAt,
	)
}

// AuthDecision is returned when a request is denied, naming the rule.
type AuthDecision struct {
	RuleCode string
	Detail   string
}

// logDenial records a denied request for audit and risk (Phase 10).
func (r *Repo) logDenial(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID, in AuthRequest, code, detail string, override bool, actorID uuid.UUID) {
	_, _ = tx.Exec(ctx, `
		INSERT INTO fuel_authorization_denials (tenant_id, customer_id, station_id, rule_code, detail, requested_amount, override_attempted, actor_id)
		VALUES ($1, $2, $3, $4, $5, $6::numeric, $7, $8)
	`, tenantID, in.CustomerID, in.StationID, code, detail, money0(in.RequestedAmount), override, actorID)
}

// RequestAuthorization runs the deterministic decision and, when allowed,
// creates an approved authorization holding the requested amount. When denied
// (and not overridden) it logs the denial and returns ErrDenied with the rule.
func (r *Repo) RequestAuthorization(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID, in AuthRequest, createdBy uuid.UUID, override bool) (*Authorization, *AuthDecision, error) {
	pos, err := r.CreditPosition(ctx, tx, tenantID, in.CustomerID)
	if errors.Is(err, ErrNotFound) {
		return nil, &AuthDecision{RuleCode: "unknown_customer"}, ErrDenied
	}
	if err != nil {
		return nil, nil, err
	}

	deny := func(code, detail string) (*Authorization, *AuthDecision, error) {
		r.logDenial(ctx, tx, tenantID, in, code, detail, override, createdBy)
		return nil, &AuthDecision{RuleCode: code, Detail: detail}, ErrDenied
	}

	if !override {
		if pos.Status == "suspended" || pos.Status == "closed" {
			return deny("account_status", "account is "+pos.Status)
		}
		if pos.Hold {
			return deny("account_hold", "customer account is on hold")
		}
		// Credential, if supplied, must be active.
		if in.CredentialID != nil {
			var status string
			if err := tx.QueryRow(ctx, `SELECT status FROM fuel_credentials WHERE tenant_id = $1 AND id = $2`, tenantID, *in.CredentialID).Scan(&status); err == nil && status != "active" {
				return deny("credential_status", "credential is "+status)
			}
		}
		// Available credit must cover the request.
		var ok bool
		if err := tx.QueryRow(ctx, `SELECT $1::numeric <= $2::numeric`, money0(in.RequestedAmount), pos.Available).Scan(&ok); err != nil {
			return nil, nil, err
		}
		if !ok {
			return deny("insufficient_credit", "requested amount exceeds available credit")
		}
		// Strictest per-transaction limit, if any.
		var overLimit bool
		if err := tx.QueryRow(ctx, `
			SELECT EXISTS (
			    SELECT 1 FROM fuel_limits
			    WHERE tenant_id = $1 AND period = 'transaction' AND max_amount IS NOT NULL
			      AND (customer_id = $2 OR customer_id IS NULL)
			      AND (vehicle_id = $3 OR vehicle_id IS NULL)
			      AND $4::numeric > max_amount
			)
		`, tenantID, in.CustomerID, in.VehicleID, money0(in.RequestedAmount)).Scan(&overLimit); err != nil {
			return nil, nil, err
		}
		if overLimit {
			return deny("transaction_limit", "requested amount exceeds a per-transaction limit")
		}
	}

	expiry := time.Now().Add(time.Hour)
	var a Authorization
	if err := scanAuth(tx.QueryRow(ctx, `
		INSERT INTO fuel_authorizations
		    (tenant_id, customer_id, vehicle_id, driver_id, credential_id, station_id, product_id,
		     requested_amount, approved_amount, odometer, status, expiry_at, source, created_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8::numeric, $8::numeric, $9::numeric, 'approved', $10, COALESCE(NULLIF($11,''),'forecourt'), $12)
		RETURNING `+authColumns,
		tenantID, in.CustomerID, in.VehicleID, in.DriverID, in.CredentialID, in.StationID, in.ProductID,
		money0(in.RequestedAmount), nullableMoney(deref(in.Odometer)), expiry, in.Source, createdBy,
	), &a); err != nil {
		return nil, nil, err
	}
	return &a, nil, nil
}

func (r *Repo) GetAuthorization(ctx context.Context, tenantID, id uuid.UUID) (*Authorization, error) {
	var a Authorization
	err := scanAuth(r.pool.QueryRow(ctx, `SELECT `+authColumns+` FROM fuel_authorizations WHERE tenant_id = $1 AND id = $2`, tenantID, id), &a)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &a, nil
}

func (r *Repo) ListAuthorizations(ctx context.Context, tenantID, customerID uuid.UUID) ([]Authorization, error) {
	var custFilter *uuid.UUID
	if customerID != uuid.Nil {
		custFilter = &customerID
	}
	rows, err := r.pool.Query(ctx, `
		SELECT `+authColumns+` FROM fuel_authorizations
		WHERE tenant_id = $1 AND ($2::uuid IS NULL OR customer_id = $2)
		ORDER BY created_at DESC
	`, tenantID, custFilter)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Authorization{}
	for rows.Next() {
		var a Authorization
		if err := scanAuth(rows, &a); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// FulfillAuthorization consumes an approved authorization exactly once, linking
// it to the Phase-6 sale. A second fulfillment yields ErrConsumed.
func (r *Repo) FulfillAuthorization(ctx context.Context, tx pgx.Tx, tenantID, id, consumedBy uuid.UUID) (*Authorization, error) {
	var a Authorization
	err := scanAuth(tx.QueryRow(ctx, `
		UPDATE fuel_authorizations SET status = 'fulfilled', consumed_by = $3
		WHERE tenant_id = $1 AND id = $2 AND status = 'approved' AND consumed_by IS NULL
		RETURNING `+authColumns,
		tenantID, id, consumedBy,
	), &a)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrConsumed
	}
	if isUniqueViolation(err) {
		return nil, ErrConsumed
	}
	if err != nil {
		return nil, err
	}
	return &a, nil
}

// SetAuthorizationStatus moves an authorization to cancelled or voided.
func (r *Repo) SetAuthorizationStatus(ctx context.Context, tx pgx.Tx, tenantID, id uuid.UUID, to string) (*Authorization, error) {
	var a Authorization
	var sql string
	if to == "cancelled" {
		sql = `UPDATE fuel_authorizations SET status = 'cancelled' WHERE tenant_id = $1 AND id = $2 AND status IN ('requested','approved') RETURNING ` + authColumns
	} else { // voided — reverses a fulfilled authorization's consumption
		sql = `UPDATE fuel_authorizations SET status = 'voided', consumed_by = NULL WHERE tenant_id = $1 AND id = $2 AND status IN ('approved','fulfilled') RETURNING ` + authColumns
	}
	err := scanAuth(tx.QueryRow(ctx, sql, tenantID, id), &a)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrBadState
	}
	if err != nil {
		return nil, err
	}
	return &a, nil
}

// money0 returns "0" for an empty string so SQL numeric casts never fail.
func money0(s string) string {
	if s == "" {
		return "0"
	}
	return s
}

// ---- Fuel limits ----

func (r *Repo) CreateLimit(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID, customerID, vehicleID, productID *uuid.UUID, scope, period string, maxAmount, maxLitres *string) (uuid.UUID, error) {
	var id uuid.UUID
	err := tx.QueryRow(ctx, `
		INSERT INTO fuel_limits (tenant_id, customer_id, vehicle_id, product_id, scope, period, max_amount, max_litres)
		VALUES ($1, $2, $3, $4, COALESCE(NULLIF($5,''),'transaction'), COALESCE(NULLIF($6,''),'transaction'), $7::numeric, $8::numeric)
		RETURNING id
	`, tenantID, customerID, vehicleID, productID, scope, period, maxAmount, maxLitres).Scan(&id)
	return id, err
}

func (r *Repo) ListLimits(ctx context.Context, tenantID, customerID uuid.UUID) ([]map[string]any, error) {
	var custFilter *uuid.UUID
	if customerID != uuid.Nil {
		custFilter = &customerID
	}
	rows, err := r.pool.Query(ctx, `
		SELECT id, customer_id, vehicle_id, product_id, scope, period, max_amount::text, max_litres::text
		FROM fuel_limits WHERE tenant_id = $1 AND ($2::uuid IS NULL OR customer_id = $2) ORDER BY created_at DESC
	`, tenantID, custFilter)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id uuid.UUID
		var cust, veh, prod *uuid.UUID
		var scope, period string
		var maxAmt, maxLit *string
		if err := rows.Scan(&id, &cust, &veh, &prod, &scope, &period, &maxAmt, &maxLit); err != nil {
			return nil, err
		}
		out = append(out, map[string]any{
			"id": id, "customer_id": cust, "vehicle_id": veh, "product_id": prod,
			"scope": scope, "period": period, "max_amount": maxAmt, "max_litres": maxLit,
		})
	}
	return out, rows.Err()
}
