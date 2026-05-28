package fleet

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type Driver struct {
	ID                uuid.UUID
	TenantID          uuid.UUID
	CustomerID        uuid.UUID
	Name              string
	Phone             *string
	LicenseNumber     *string
	HasPIN            bool
	Status            string
	AllowedProductIDs []uuid.UUID
	AssignmentRule    string
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

type DriverInput struct {
	CustomerID        uuid.UUID
	Name              string
	Phone             *string
	LicenseNumber     *string
	PIN               *string
	AllowedProductIDs []uuid.UUID
	AssignmentRule    string
}

// hashSecret salts a PIN/token with the tenant id and returns a hex SHA-256.
// Raw secrets are never stored; validation recomputes this hash.
func hashSecret(tenantID uuid.UUID, raw string) string {
	sum := sha256.Sum256([]byte(tenantID.String() + ":" + raw))
	return hex.EncodeToString(sum[:])
}

const driverColumns = `
    id, tenant_id, customer_id, name, phone, license_number, (pin_hash IS NOT NULL),
    status, allowed_product_ids, assignment_rule, created_at, updated_at
`

func scanDriver(row pgx.Row, d *Driver) error {
	return row.Scan(
		&d.ID, &d.TenantID, &d.CustomerID, &d.Name, &d.Phone, &d.LicenseNumber, &d.HasPIN,
		&d.Status, &d.AllowedProductIDs, &d.AssignmentRule, &d.CreatedAt, &d.UpdatedAt,
	)
}

func (r *Repo) CreateDriver(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID, in DriverInput) (*Driver, error) {
	var pinHash *string
	if in.PIN != nil && *in.PIN != "" {
		h := hashSecret(tenantID, *in.PIN)
		pinHash = &h
	}
	rule := in.AssignmentRule
	if rule == "" {
		rule = "any"
	}
	allowed := in.AllowedProductIDs
	if allowed == nil {
		allowed = []uuid.UUID{}
	}
	var d Driver
	err := scanDriver(tx.QueryRow(ctx, `
		INSERT INTO customer_drivers
		    (tenant_id, customer_id, name, phone, license_number, pin_hash, allowed_product_ids, assignment_rule)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING `+driverColumns,
		tenantID, in.CustomerID, in.Name, in.Phone, in.LicenseNumber, pinHash, allowed, rule,
	), &d)
	if err != nil {
		return nil, err
	}
	return &d, nil
}

func (r *Repo) GetDriver(ctx context.Context, tenantID, id uuid.UUID) (*Driver, error) {
	var d Driver
	err := scanDriver(r.pool.QueryRow(ctx, `SELECT `+driverColumns+` FROM customer_drivers WHERE tenant_id = $1 AND id = $2`, tenantID, id), &d)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &d, nil
}

func (r *Repo) ListDrivers(ctx context.Context, tenantID, customerID uuid.UUID) ([]Driver, error) {
	var custFilter *uuid.UUID
	if customerID != uuid.Nil {
		custFilter = &customerID
	}
	rows, err := r.pool.Query(ctx, `
		SELECT `+driverColumns+` FROM customer_drivers
		WHERE tenant_id = $1 AND ($2::uuid IS NULL OR customer_id = $2)
		ORDER BY name
	`, tenantID, custFilter)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Driver{}
	for rows.Next() {
		var d Driver
		if err := scanDriver(rows, &d); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (r *Repo) SetDriverStatus(ctx context.Context, tx pgx.Tx, tenantID, id uuid.UUID, status string) (*Driver, error) {
	var d Driver
	err := scanDriver(tx.QueryRow(ctx, `
		UPDATE customer_drivers SET status = $3 WHERE tenant_id = $1 AND id = $2
		RETURNING `+driverColumns, tenantID, id, status), &d)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &d, nil
}

// ResetDriverPIN sets a new salted PIN hash (or clears it when pin is empty).
func (r *Repo) ResetDriverPIN(ctx context.Context, tx pgx.Tx, tenantID, id uuid.UUID, pin string) error {
	var pinHash *string
	if pin != "" {
		h := hashSecret(tenantID, pin)
		pinHash = &h
	}
	tag, err := tx.Exec(ctx, `UPDATE customer_drivers SET pin_hash = $3 WHERE tenant_id = $1 AND id = $2`, tenantID, id, pinHash)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// VerifyDriverPIN reports whether the supplied PIN matches the driver's hash.
func (r *Repo) VerifyDriverPIN(ctx context.Context, tenantID, id uuid.UUID, pin string) (bool, error) {
	var stored *string
	err := r.pool.QueryRow(ctx, `SELECT pin_hash FROM customer_drivers WHERE tenant_id = $1 AND id = $2`, tenantID, id).Scan(&stored)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, ErrNotFound
	}
	if err != nil {
		return false, err
	}
	if stored == nil {
		return false, nil
	}
	return *stored == hashSecret(tenantID, pin), nil
}
