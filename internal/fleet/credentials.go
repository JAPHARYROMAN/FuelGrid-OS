package fleet

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type Credential struct {
	ID             uuid.UUID
	TenantID       uuid.UUID
	CustomerID     uuid.UUID
	VehicleID      *uuid.UUID
	DriverID       *uuid.UUID
	CredentialType string
	MaskedLabel    string
	Status         string
	IssuedAt       time.Time
	ExpiryDate     *time.Time
	LastUsedAt     *time.Time
	CreatedAt      time.Time
}

type CredentialInput struct {
	CustomerID     uuid.UUID
	VehicleID      *uuid.UUID
	DriverID       *uuid.UUID
	CredentialType string
	RawToken       string
	ExpiryDate     *time.Time
}

// maskToken returns a display-safe label exposing only the last four
// characters of a raw credential value.
func maskToken(raw string) string {
	if len(raw) <= 4 {
		return "****"
	}
	return "****" + raw[len(raw)-4:]
}

const credentialColumns = `
    id, tenant_id, customer_id, vehicle_id, driver_id, credential_type, masked_label,
    status, issued_at, expiry_date, last_used_at, created_at
`

func scanCredential(row pgx.Row, c *Credential) error {
	return row.Scan(
		&c.ID, &c.TenantID, &c.CustomerID, &c.VehicleID, &c.DriverID, &c.CredentialType, &c.MaskedLabel,
		&c.Status, &c.IssuedAt, &c.ExpiryDate, &c.LastUsedAt, &c.CreatedAt,
	)
}

// IssueCredential stores the salted hash + mask of a raw token. The raw token
// is never persisted; the caller must surface it to the operator once.
func (r *Repo) IssueCredential(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID, in CredentialInput) (*Credential, error) {
	ct := in.CredentialType
	if ct == "" {
		ct = "card"
	}
	var c Credential
	err := scanCredential(tx.QueryRow(ctx, `
		INSERT INTO fuel_credentials
		    (tenant_id, customer_id, vehicle_id, driver_id, credential_type, token_hash, masked_label, expiry_date)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING `+credentialColumns,
		tenantID, in.CustomerID, in.VehicleID, in.DriverID, ct,
		hashSecret(tenantID, in.RawToken), maskToken(in.RawToken), in.ExpiryDate,
	), &c)
	if isUniqueViolation(err) {
		return nil, ErrConflict
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func (r *Repo) ListCredentials(ctx context.Context, tenantID, customerID uuid.UUID) ([]Credential, error) {
	var custFilter *uuid.UUID
	if customerID != uuid.Nil {
		custFilter = &customerID
	}
	rows, err := r.pool.Query(ctx, `
		SELECT `+credentialColumns+` FROM fuel_credentials
		WHERE tenant_id = $1 AND ($2::uuid IS NULL OR customer_id = $2)
		ORDER BY created_at DESC
	`, tenantID, custFilter)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Credential{}
	for rows.Next() {
		var c Credential
		if err := scanCredential(rows, &c); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (r *Repo) SetCredentialStatus(ctx context.Context, tx pgx.Tx, tenantID, id uuid.UUID, status string) (*Credential, error) {
	var c Credential
	err := scanCredential(tx.QueryRow(ctx, `
		UPDATE fuel_credentials SET status = $3 WHERE tenant_id = $1 AND id = $2
		RETURNING `+credentialColumns, tenantID, id, status), &c)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// CredentialContext is the result of validating a raw credential: the linked
// customer/vehicle/driver and a usable flag.
type CredentialContext struct {
	Credential   Credential
	CustomerName string
	Expired      bool
	Usable       bool
}

// ValidateCredential resolves a raw token to its credential + customer context,
// updating last_used_at. It returns ErrNotFound when no credential matches.
func (r *Repo) ValidateCredential(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID, rawToken string) (*CredentialContext, error) {
	var out CredentialContext
	var expiry *time.Time
	err := tx.QueryRow(ctx, `
		SELECT fc.id, fc.tenant_id, fc.customer_id, fc.vehicle_id, fc.driver_id, fc.credential_type,
		       fc.masked_label, fc.status, fc.issued_at, fc.expiry_date, fc.last_used_at, fc.created_at,
		       c.name, fc.expiry_date
		FROM fuel_credentials fc
		JOIN customers c ON c.id = fc.customer_id AND c.tenant_id = fc.tenant_id
		WHERE fc.tenant_id = $1 AND fc.token_hash = $2
	`, tenantID, hashSecret(tenantID, rawToken)).Scan(
		&out.Credential.ID, &out.Credential.TenantID, &out.Credential.CustomerID, &out.Credential.VehicleID,
		&out.Credential.DriverID, &out.Credential.CredentialType, &out.Credential.MaskedLabel, &out.Credential.Status,
		&out.Credential.IssuedAt, &out.Credential.ExpiryDate, &out.Credential.LastUsedAt, &out.Credential.CreatedAt,
		&out.CustomerName, &expiry,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	out.Expired = expiry != nil && expiry.Before(time.Now())
	out.Usable = out.Credential.Status == "active" && !out.Expired
	if out.Usable {
		_, _ = tx.Exec(ctx, `UPDATE fuel_credentials SET last_used_at = now() WHERE tenant_id = $1 AND id = $2`, tenantID, out.Credential.ID)
	}
	return &out, nil
}
