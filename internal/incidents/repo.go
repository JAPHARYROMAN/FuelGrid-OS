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
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

type CreateInput struct {
	StationID         uuid.UUID
	RelatedEntityType *string
	RelatedEntityID   *uuid.UUID
	Type              string
	Severity          string
	Description       string
	OpenedBy          uuid.UUID
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
    resolved_at, resolved_by, created_at, updated_at
`

func scan(row pgx.Row, i *Incident) error {
	return row.Scan(
		&i.ID, &i.TenantID, &i.StationID, &i.RelatedEntityType, &i.RelatedEntityID,
		&i.Type, &i.Severity, &i.Description, &i.Status, &i.OpenedAt, &i.OpenedBy,
		&i.ResolvedAt, &i.ResolvedBy, &i.CreatedAt, &i.UpdatedAt,
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
