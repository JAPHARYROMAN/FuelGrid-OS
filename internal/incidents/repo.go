// Package incidents is the data layer for the `incidents` table — the
// operational issue queue raised against stations and their entities.
package incidents

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/japharyroman/fuelgrid-os/internal/database"
)

type Incident struct {
	ID                uuid.UUID
	TenantID          uuid.UUID
	StationID         uuid.UUID
	RelatedEntityType *string
	RelatedEntityID   *uuid.UUID
	Type              string
	Severity          string
	Description       string
	Status            string
	OpenedAt          time.Time
	OpenedBy          uuid.UUID
	ResolvedAt        *time.Time
	ResolvedBy        *uuid.UUID
	// DedupeKey is the client-supplied offline-replay key (0103); nil when the
	// create carried none.
	DedupeKey *string
	CreatedAt time.Time
	UpdatedAt time.Time
}

type CreateInput struct {
	StationID         uuid.UUID
	RelatedEntityType *string
	RelatedEntityID   *uuid.UUID
	Type              string
	Severity          string
	Description       string
	OpenedBy          uuid.UUID
	// DedupeKey is an optional client-supplied key (Mobile Attendant Phase 7).
	// When set, a replayed create carrying the same key for the same tenant
	// returns the already-opened incident instead of inserting a duplicate —
	// the mobile offline queue replays creations, which are otherwise
	// non-idempotent. When nil, the prior always-insert behaviour is preserved.
	DedupeKey *string
}

// ListFilter narrows the incident queue. Nil/empty fields are ignored.
// StationIDs, when non-empty, restricts results to those stations.
type ListFilter struct {
	StationIDs []uuid.UUID
	Status     *string
	Severity   *string
}

type Repo struct{ pool *database.Pool }

func New(pool *database.Pool) *Repo { return &Repo{pool: pool} }

var ErrNotFound = errors.New("incidents: not found")

const columns = `
    id, tenant_id, station_id, related_entity_type, related_entity_id,
    type, severity, description, status, opened_at, opened_by,
    resolved_at, resolved_by, dedupe_key, created_at, updated_at
`

func scan(row pgx.Row, i *Incident) error {
	return row.Scan(
		&i.ID, &i.TenantID, &i.StationID, &i.RelatedEntityType, &i.RelatedEntityID,
		&i.Type, &i.Severity, &i.Description, &i.Status, &i.OpenedAt, &i.OpenedBy,
		&i.ResolvedAt, &i.ResolvedBy, &i.DedupeKey, &i.CreatedAt, &i.UpdatedAt,
	)
}

func (r *Repo) List(ctx context.Context, tenantID uuid.UUID, f ListFilter) ([]Incident, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT `+columns+`
		FROM incidents
		WHERE tenant_id = $1
		  AND ($2::uuid[] IS NULL OR station_id = ANY($2::uuid[]))
		  AND ($3::text IS NULL OR status = $3)
		  AND ($4::text IS NULL OR severity = $4)
		ORDER BY opened_at DESC
	`, tenantID, database.UUIDStrings(f.StationIDs), f.Status, f.Severity)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Incident
	for rows.Next() {
		var i Incident
		if err := scan(rows, &i); err != nil {
			return nil, err
		}
		out = append(out, i)
	}
	return out, rows.Err()
}

// ListPage mirrors List (same filter) with limit/offset paging and a stable
// (opened_at DESC, id) ordering. Callers fetch limit+1 to detect a further
// page.
func (r *Repo) ListPage(ctx context.Context, tenantID uuid.UUID, f ListFilter, limit, offset int) ([]Incident, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT `+columns+`
		FROM incidents
		WHERE tenant_id = $1
		  AND ($2::uuid[] IS NULL OR station_id = ANY($2::uuid[]))
		  AND ($3::text IS NULL OR status = $3)
		  AND ($4::text IS NULL OR severity = $4)
		ORDER BY opened_at DESC, id
		LIMIT $5 OFFSET $6
	`, tenantID, database.UUIDStrings(f.StationIDs), f.Status, f.Severity, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Incident
	for rows.Next() {
		var i Incident
		if err := scan(rows, &i); err != nil {
			return nil, err
		}
		out = append(out, i)
	}
	return out, rows.Err()
}

// ListActiveForStation returns a station's unresolved incidents (anything
// not yet resolved or closed), newest first — the set the station dashboard
// surfaces.
func (r *Repo) ListActiveForStation(ctx context.Context, tenantID, stationID uuid.UUID) ([]Incident, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT `+columns+`
		FROM incidents
		WHERE tenant_id = $1 AND station_id = $2 AND status NOT IN ('resolved', 'closed')
		ORDER BY opened_at DESC
	`, tenantID, stationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Incident
	for rows.Next() {
		var i Incident
		if err := scan(rows, &i); err != nil {
			return nil, err
		}
		out = append(out, i)
	}
	return out, rows.Err()
}

func (r *Repo) Get(ctx context.Context, tenantID, id uuid.UUID) (*Incident, error) {
	var i Incident
	if err := scan(r.pool.QueryRow(ctx, `
		SELECT `+columns+`
		FROM incidents WHERE id = $1 AND tenant_id = $2
	`, id, tenantID), &i); err != nil {
		return nil, err
	}
	return &i, nil
}

func (r *Repo) Create(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID, in CreateInput) (*Incident, error) {
	typ := in.Type
	if typ == "" {
		typ = "other"
	}
	sev := in.Severity
	if sev == "" {
		sev = "medium"
	}
	var i Incident
	if err := scan(tx.QueryRow(ctx, `
		INSERT INTO incidents
		    (tenant_id, station_id, related_entity_type, related_entity_id,
		     type, severity, description, opened_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING `+columns,
		tenantID, in.StationID, in.RelatedEntityType, in.RelatedEntityID,
		typ, sev, in.Description, in.OpenedBy,
	), &i); err != nil {
		return nil, err
	}
	return &i, nil
}

// CreateResult is the outcome of CreateDeduped. Replayed reports whether the
// returned incident is a pre-existing row matched by dedupe key (true) rather
// than a freshly inserted one (false). The handler uses it to skip side
// effects (audit event, outbox notification) on a replay so a replayed
// offline create cannot double-report.
type CreateResult struct {
	Incident *Incident
	Replayed bool
}

// CreateDeduped inserts an incident. When in.DedupeKey is supplied and an
// incident already exists for (tenant_id, dedupe_key), the existing row is
// returned with Replayed=true instead of inserting a duplicate (Mobile
// Attendant Phase 7 offline replay). The dedup is tenant-scoped: the same key
// under a different tenant inserts normally.
//
// It relies on the partial unique index uq_incidents_tenant_dedupe_key
// (migration 0103) via INSERT ... ON CONFLICT, so the dedup is enforced by the
// database under concurrency, not by a check-then-insert race — exactly the
// payments idempotency pattern (0096).
func (r *Repo) CreateDeduped(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID, in CreateInput) (*CreateResult, error) {
	// No key supplied: preserve the prior always-insert behaviour.
	if in.DedupeKey == nil {
		i, err := r.Create(ctx, tx, tenantID, in)
		if err != nil {
			return nil, err
		}
		return &CreateResult{Incident: i, Replayed: false}, nil
	}

	typ := in.Type
	if typ == "" {
		typ = "other"
	}
	sev := in.Severity
	if sev == "" {
		sev = "medium"
	}

	// Key supplied: insert, but on a (tenant_id, dedupe_key) conflict do
	// nothing so we can return the already-opened row unchanged. ON CONFLICT
	// DO NOTHING yields no RETURNING row, so an empty result means the key
	// already existed — we then SELECT the original.
	var i Incident
	err := scan(tx.QueryRow(ctx, `
		INSERT INTO incidents
		    (tenant_id, station_id, related_entity_type, related_entity_id,
		     type, severity, description, opened_by, dedupe_key)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (tenant_id, dedupe_key) WHERE dedupe_key IS NOT NULL
		DO NOTHING
		RETURNING `+columns,
		tenantID, in.StationID, in.RelatedEntityType, in.RelatedEntityID,
		typ, sev, in.Description, in.OpenedBy, *in.DedupeKey,
	), &i)
	if err == nil {
		return &CreateResult{Incident: &i, Replayed: false}, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}

	// Conflict: an incident with this key already exists for the tenant.
	// Return it so the caller responds idempotently rather than erroring.
	var existing Incident
	if err := scan(tx.QueryRow(ctx, `
		SELECT `+columns+` FROM incidents
		WHERE tenant_id = $1 AND dedupe_key = $2
	`, tenantID, *in.DedupeKey), &existing); err != nil {
		return nil, err
	}
	return &CreateResult{Incident: &existing, Replayed: true}, nil
}

// UpdateStatus transitions an incident. When the target status is resolved
// or closed, resolved_at is stamped now() and resolved_by is set; moving
// back to an open state clears them.
func (r *Repo) UpdateStatus(ctx context.Context, tx pgx.Tx, tenantID, id uuid.UUID, status string, actorID uuid.UUID) (*Incident, error) {
	resolving := status == "resolved" || status == "closed"
	var i Incident
	err := scan(tx.QueryRow(ctx, `
		UPDATE incidents
		SET status      = $3,
		    resolved_at = CASE WHEN $4 THEN now() ELSE NULL END,
		    resolved_by = CASE WHEN $4 THEN $5::uuid ELSE NULL END
		WHERE id = $1 AND tenant_id = $2
		RETURNING `+columns,
		id, tenantID, status, resolving, actorID,
	), &i)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &i, nil
}
