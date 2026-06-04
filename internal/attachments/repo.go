// Package attachments is the data layer for the generic per-entity file
// Attachments framework (C.3): files attached to any business entity, keyed by
// an opaque (entity_type, entity_id) pair and stored inline as bytea (this
// deployment has no object store, mirroring the tenant logo). Rows are
// append-only plus a soft delete — never mutated.
package attachments

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/japharyroman/fuelgrid-os/internal/database"
)

// MaxSizeBytes caps a single attachment's bytes at 5 MiB. The handler bounds
// the request body before reading; this is the domain-level backstop the DB
// CHECK constraint also enforces.
const MaxSizeBytes = 5 << 20 // 5 MiB

// AllowedContentTypes is the upload allowlist: PDF, PNG, JPEG. A content type
// outside this set is rejected before any bytes are stored.
var AllowedContentTypes = map[string]bool{
	"application/pdf": true,
	"image/png":       true,
	"image/jpeg":      true,
}

// Sentinel errors the handler maps to HTTP status codes.
var (
	// ErrNotFound is returned when an attachment id does not resolve within the
	// tenant (or has been soft-deleted).
	ErrNotFound = errors.New("attachment not found")
	// ErrContentType is returned when the supplied content type is not on the
	// allowlist — the handler maps it to 400.
	ErrContentType = errors.New("attachment content type not allowed")
	// ErrTooLarge is returned when the bytes exceed MaxSizeBytes — the handler
	// maps it to 413.
	ErrTooLarge = errors.New("attachment exceeds the size cap")
)

// Attachment is the metadata row shape (no bytes). List/Get return this; the
// raw bytes are fetched separately via Stream so a list never drags megabytes.
type Attachment struct {
	ID          uuid.UUID
	TenantID    uuid.UUID
	StationID   *uuid.UUID
	EntityType  string
	EntityID    uuid.UUID
	Filename    string
	ContentType string
	SizeBytes   int64
	Checksum    string
	UploadedBy  *uuid.UUID
	CreatedAt   time.Time
}

// CreateInput captures a new attachment. Data carries the already-read bytes;
// the repo computes the checksum and validates type/size before inserting.
type CreateInput struct {
	TenantID    uuid.UUID
	StationID   *uuid.UUID
	EntityType  string
	EntityID    uuid.UUID
	Filename    string
	ContentType string
	Data        []byte
	UploadedBy  uuid.UUID
}

// Repo is the Postgres-backed attachments repository.
type Repo struct {
	pool *database.Pool
}

// New wires a Repo against the supplied pool.
func New(pool *database.Pool) *Repo {
	return &Repo{pool: pool}
}

const metaColumns = `
	id, tenant_id, station_id, entity_type, entity_id,
	filename, content_type, size_bytes, checksum, uploaded_by, created_at`

func scanMeta(row pgx.Row, a *Attachment) error {
	return row.Scan(
		&a.ID, &a.TenantID, &a.StationID, &a.EntityType, &a.EntityID,
		&a.Filename, &a.ContentType, &a.SizeBytes, &a.Checksum, &a.UploadedBy, &a.CreatedAt,
	)
}

// Create validates the content type and size, computes the sha-256 checksum,
// and inserts the row. It returns ErrContentType / ErrTooLarge for the two
// client-fixable failures so the handler can map them to 400 / 413.
func (r *Repo) Create(ctx context.Context, in CreateInput) (*Attachment, error) {
	if !AllowedContentTypes[in.ContentType] {
		return nil, ErrContentType
	}
	if len(in.Data) == 0 {
		return nil, ErrContentType
	}
	if int64(len(in.Data)) > MaxSizeBytes {
		return nil, ErrTooLarge
	}

	sum := sha256.Sum256(in.Data)
	checksum := hex.EncodeToString(sum[:])

	var a Attachment
	err := scanMeta(r.pool.QueryRow(ctx, `
		INSERT INTO attachments
			(tenant_id, station_id, entity_type, entity_id,
			 filename, content_type, size_bytes, data, checksum, uploaded_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		RETURNING `+metaColumns,
		in.TenantID, in.StationID, in.EntityType, in.EntityID,
		in.Filename, in.ContentType, int64(len(in.Data)), in.Data, checksum, in.UploadedBy,
	), &a)
	if err != nil {
		return nil, err
	}
	return &a, nil
}

// ListByEntity returns the live (non-deleted) attachments for one entity,
// newest first. The tenant filter is redundant under RLS but kept explicit so
// the query is correct on the owner pool too.
func (r *Repo) ListByEntity(ctx context.Context, tenantID uuid.UUID, entityType string, entityID uuid.UUID) ([]Attachment, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT `+metaColumns+`
		FROM attachments
		WHERE tenant_id = $1 AND entity_type = $2 AND entity_id = $3 AND deleted_at IS NULL
		ORDER BY created_at DESC, id DESC
	`, tenantID, entityType, entityID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]Attachment, 0)
	for rows.Next() {
		var a Attachment
		if err := scanMeta(rows, &a); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// GetMeta returns one live attachment's metadata, or ErrNotFound.
func (r *Repo) GetMeta(ctx context.Context, tenantID, id uuid.UUID) (*Attachment, error) {
	var a Attachment
	err := scanMeta(r.pool.QueryRow(ctx, `
		SELECT `+metaColumns+`
		FROM attachments
		WHERE tenant_id = $1 AND id = $2 AND deleted_at IS NULL
	`, tenantID, id), &a)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &a, nil
}

// Stream returns the bytes + content type + filename for one live attachment,
// or ErrNotFound. Used by the download endpoint.
func (r *Repo) Stream(ctx context.Context, tenantID, id uuid.UUID) (data []byte, contentType, filename string, err error) {
	err = r.pool.QueryRow(ctx, `
		SELECT data, content_type, filename
		FROM attachments
		WHERE tenant_id = $1 AND id = $2 AND deleted_at IS NULL
	`, tenantID, id).Scan(&data, &contentType, &filename)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, "", "", ErrNotFound
	}
	if err != nil {
		return nil, "", "", err
	}
	return data, contentType, filename, nil
}

// SoftDelete marks one live attachment deleted (idempotent only over live
// rows: a second call returns ErrNotFound). Returns the row's metadata as it
// stood before deletion so the handler can audit it. The bytes are retained in
// the row — soft delete keeps the evidence for a posted/locked parent.
func (r *Repo) SoftDelete(ctx context.Context, tenantID, id uuid.UUID) (*Attachment, error) {
	var a Attachment
	err := scanMeta(r.pool.QueryRow(ctx, `
		UPDATE attachments
		SET deleted_at = now()
		WHERE tenant_id = $1 AND id = $2 AND deleted_at IS NULL
		RETURNING `+metaColumns, tenantID, id), &a)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &a, nil
}
