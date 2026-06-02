// Package branding is the data layer for the tenant-level company letterhead
// stored in the `tenant_branding` table (one row per tenant). The text fields
// drive the header of every downloadable PDF via the shared letterhead helper;
// the logo bytes are stored inline and streamed/embedded separately.
package branding

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/japharyroman/fuelgrid-os/internal/database"
)

// Branding is the row shape consumed by handlers, the SDK, and the shared
// letterhead PDF helper. The logo bytes are deliberately NOT carried here — the
// list/get path only needs to know whether a logo exists (HasLogo) and its
// content type; the bytes are fetched separately via GetLogo so a branding read
// never drags a megabyte of image through every PDF render.
type Branding struct {
	TenantID        uuid.UUID
	DisplayName     string
	LegalName       string
	TaxID           string
	RegistrationNo  string
	AddressLine1    string
	AddressLine2    string
	City            string
	Country         string
	Phone           string
	Email           string
	Website         string
	FooterNote      string
	HasLogo         bool
	LogoContentType string
	UpdatedAt       time.Time
}

// UpsertInput captures the writable text fields. Empty strings are stored as
// SQL NULL, so clearing a field round-trips cleanly. The logo is managed
// separately (SetLogo / ClearLogo) and is never touched by Upsert.
type UpsertInput struct {
	DisplayName    string
	LegalName      string
	TaxID          string
	RegistrationNo string
	AddressLine1   string
	AddressLine2   string
	City           string
	Country        string
	Phone          string
	Email          string
	Website        string
	FooterNote     string
}

// Repo is the Postgres-backed tenant_branding repository.
type Repo struct {
	pool *database.Pool
}

// New wires a Repo against the supplied pool.
func New(pool *database.Pool) *Repo {
	return &Repo{pool: pool}
}

// nullable maps an empty string to nil so it is stored as SQL NULL.
func nullable(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// deref maps a nil *string to "".
func deref(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// Get returns the tenant's branding row, defaulting to an empty (but valid)
// Branding for the tenant when no row exists yet. It never returns
// pgx.ErrNoRows, so handlers can treat "branding" as always present. The
// logo bytes are not loaded — only presence + content type.
func (r *Repo) Get(ctx context.Context, tenantID uuid.UUID) (*Branding, error) {
	var (
		b               = Branding{TenantID: tenantID}
		displayName     *string
		legalName       *string
		taxID           *string
		registrationNo  *string
		addressLine1    *string
		addressLine2    *string
		city            *string
		country         *string
		phone           *string
		email           *string
		website         *string
		footerNote      *string
		logoContentType *string
		hasLogo         bool
		updatedAt       *time.Time
	)
	err := r.pool.QueryRow(ctx, `
		SELECT display_name, legal_name, tax_id, registration_no,
		       address_line1, address_line2, city, country,
		       phone, email, website, footer_note,
		       (logo IS NOT NULL) AS has_logo, logo_content_type, updated_at
		FROM tenant_branding
		WHERE tenant_id = $1
	`, tenantID).Scan(
		&displayName, &legalName, &taxID, &registrationNo,
		&addressLine1, &addressLine2, &city, &country,
		&phone, &email, &website, &footerNote,
		&hasLogo, &logoContentType, &updatedAt,
	)
	if err != nil {
		// No row yet — return an empty, valid default the handler can render.
		// pgx returns ErrNoRows; any other error propagates.
		if errors.Is(err, pgx.ErrNoRows) {
			return &b, nil
		}
		return nil, err
	}
	b.DisplayName = deref(displayName)
	b.LegalName = deref(legalName)
	b.TaxID = deref(taxID)
	b.RegistrationNo = deref(registrationNo)
	b.AddressLine1 = deref(addressLine1)
	b.AddressLine2 = deref(addressLine2)
	b.City = deref(city)
	b.Country = deref(country)
	b.Phone = deref(phone)
	b.Email = deref(email)
	b.Website = deref(website)
	b.FooterNote = deref(footerNote)
	b.HasLogo = hasLogo
	b.LogoContentType = deref(logoContentType)
	if updatedAt != nil {
		b.UpdatedAt = *updatedAt
	}
	return &b, nil
}

// Upsert writes the text fields, creating the row when absent and otherwise
// overwriting every text field (the logo is left untouched). Returns the stored
// row (logo presence preserved).
func (r *Repo) Upsert(ctx context.Context, tenantID uuid.UUID, in UpsertInput) (*Branding, error) {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO tenant_branding
			(tenant_id, display_name, legal_name, tax_id, registration_no,
			 address_line1, address_line2, city, country,
			 phone, email, website, footer_note)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
		ON CONFLICT (tenant_id) DO UPDATE SET
			display_name    = EXCLUDED.display_name,
			legal_name      = EXCLUDED.legal_name,
			tax_id          = EXCLUDED.tax_id,
			registration_no = EXCLUDED.registration_no,
			address_line1   = EXCLUDED.address_line1,
			address_line2   = EXCLUDED.address_line2,
			city            = EXCLUDED.city,
			country         = EXCLUDED.country,
			phone           = EXCLUDED.phone,
			email           = EXCLUDED.email,
			website         = EXCLUDED.website,
			footer_note     = EXCLUDED.footer_note,
			updated_at      = now()
	`,
		tenantID,
		nullable(in.DisplayName), nullable(in.LegalName), nullable(in.TaxID), nullable(in.RegistrationNo),
		nullable(in.AddressLine1), nullable(in.AddressLine2), nullable(in.City), nullable(in.Country),
		nullable(in.Phone), nullable(in.Email), nullable(in.Website), nullable(in.FooterNote),
	)
	if err != nil {
		return nil, err
	}
	return r.Get(ctx, tenantID)
}

// SetLogo stores the logo bytes + content type, creating the row when absent
// (so a tenant can upload a logo before filling in any text). On an existing
// row only the logo columns change.
func (r *Repo) SetLogo(ctx context.Context, tenantID uuid.UUID, data []byte, contentType string) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO tenant_branding (tenant_id, logo, logo_content_type)
		VALUES ($1, $2, $3)
		ON CONFLICT (tenant_id) DO UPDATE SET
			logo              = EXCLUDED.logo,
			logo_content_type = EXCLUDED.logo_content_type,
			updated_at        = now()
	`, tenantID, data, contentType)
	return err
}

// ClearLogo removes the stored logo (no-op when there is none). The text fields
// are preserved.
func (r *Repo) ClearLogo(ctx context.Context, tenantID uuid.UUID) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE tenant_branding
		SET logo = NULL, logo_content_type = NULL, updated_at = now()
		WHERE tenant_id = $1
	`, tenantID)
	return err
}

// GetLogo returns the stored logo bytes and content type. found is false when
// no logo is stored (the caller turns that into a 404).
func (r *Repo) GetLogo(ctx context.Context, tenantID uuid.UUID) (data []byte, contentType string, found bool, err error) {
	var (
		logo *[]byte
		ct   *string
	)
	err = r.pool.QueryRow(ctx, `
		SELECT logo, logo_content_type FROM tenant_branding WHERE tenant_id = $1
	`, tenantID).Scan(&logo, &ct)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, "", false, nil
		}
		return nil, "", false, err
	}
	if logo == nil || len(*logo) == 0 {
		return nil, "", false, nil
	}
	return *logo, deref(ct), true, nil
}
