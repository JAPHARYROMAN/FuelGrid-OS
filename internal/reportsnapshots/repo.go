// Package reportsnapshots is the data layer for immutable report snapshots
// (Reports Center Phase 14 — Report Locking & Snapshots, blueprint §15).
//
// A snapshot is a point-in-time capture of a rendered report: the exact
// ReportEnvelope a live structured endpoint produced (stored verbatim — every
// money/litre figure is already the exact decimal string the repos returned)
// plus a content_hash = sha256 of a CANONICAL serialization of that envelope.
//
// IMMUTABILITY IS THE CORE PROPERTY. The captured payload (envelope,
// content_hash) and provenance (captured_by/at, report_key, filters_used,
// revision, supersedes_id) are frozen once written; a DB trigger (migration
// 0113) blocks any UPDATE that would mutate them. Only the sign-off lifecycle
// (status, signed_off_by/at, correction_note) transitions. A correction never
// overwrites: Capture inserts a NEW row (revision+1, supersedes_id -> prior), so
// the original revision is preserved forever.
//
// Snapshots are tenant-isolated by RLS; the per-snapshot read/capture
// authorization (the SAME permission as running the report live) is enforced in
// the handler layer (internal/server), never weakened here.
package reportsnapshots

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/japharyroman/fuelgrid-os/internal/database"
)

// ErrNotFound is returned when a snapshot is absent for the tenant.
var ErrNotFound = errors.New("reportsnapshots: snapshot not found")

// Status values for a snapshot's sign-off lifecycle.
const (
	StatusDraft     = "draft"
	StatusSignedOff = "signed_off"
	StatusReopened  = "reopened"
)

// Snapshot is one immutable captured report plus its sign-off lifecycle state.
// Envelope carries the rendered ReportEnvelope JSON verbatim (the caller
// unmarshals it into the wire shape); FiltersUsed is the filter map that
// produced it. ContentHash is the sha256 of the canonical envelope.
type Snapshot struct {
	ID           uuid.UUID
	TenantID     uuid.UUID
	ReportKey    string
	FiltersUsed  map[string]string
	Envelope     json.RawMessage
	ContentHash  string
	CapturedBy   uuid.UUID
	CapturedAt   time.Time
	Status       string
	Revision     int
	SupersedesID *uuid.UUID

	SignedOffBy    *uuid.UUID
	SignedOffAt    *time.Time
	CorrectionNote *string
	CreatedAt      time.Time
}

// CaptureInput is everything needed to write a new immutable snapshot row. The
// caller has already rendered the envelope, computed its canonical content hash,
// and resolved the revision/supersedes chain.
type CaptureInput struct {
	ReportKey    string
	FiltersUsed  map[string]string
	Envelope     json.RawMessage
	ContentHash  string
	CapturedBy   uuid.UUID
	Revision     int
	SupersedesID *uuid.UUID
}

// Repo is the snapshot data layer over the connection pool.
type Repo struct{ pool *database.Pool }

// New constructs the repo over the connection pool.
func New(pool *database.Pool) *Repo { return &Repo{pool: pool} }

// snapshotColumns is the full projection (the envelope payload is included — a
// snapshot is small and the point-in-time view returns it verbatim).
const snapshotColumns = `
    id, tenant_id, report_key, filters_used, envelope, content_hash,
    captured_by, captured_at, status, revision, supersedes_id,
    signed_off_by, signed_off_at, correction_note, created_at
`

func scanSnapshot(row pgx.Row, s *Snapshot) error {
	return row.Scan(
		&s.ID, &s.TenantID, &s.ReportKey, &s.FiltersUsed, &s.Envelope, &s.ContentHash,
		&s.CapturedBy, &s.CapturedAt, &s.Status, &s.Revision, &s.SupersedesID,
		&s.SignedOffBy, &s.SignedOffAt, &s.CorrectionNote, &s.CreatedAt,
	)
}

// CaptureTx inserts a new immutable snapshot row WITHIN the supplied transaction,
// so the capture and its audit/outbox write commit atomically. Status is always
// 'draft' on capture (sign-off is a separate, later transition).
func (r *Repo) CaptureTx(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID, in CaptureInput) (*Snapshot, error) {
	filters := in.FiltersUsed
	if filters == nil {
		filters = map[string]string{}
	}
	revision := in.Revision
	if revision < 1 {
		revision = 1
	}
	var s Snapshot
	if err := scanSnapshot(tx.QueryRow(ctx, `
		INSERT INTO report_snapshots
		    (tenant_id, report_key, filters_used, envelope, content_hash,
		     captured_by, status, revision, supersedes_id)
		VALUES ($1, $2, $3, $4, $5, $6, 'draft', $7, $8)
		RETURNING `+snapshotColumns,
		tenantID, in.ReportKey, filters, in.Envelope, in.ContentHash,
		in.CapturedBy, revision, in.SupersedesID,
	), &s); err != nil {
		return nil, err
	}
	return &s, nil
}

// Get returns one snapshot for the tenant (with its stored envelope), or ErrNotFound.
func (r *Repo) Get(ctx context.Context, tenantID, id uuid.UUID) (*Snapshot, error) {
	var s Snapshot
	err := scanSnapshot(r.pool.QueryRow(ctx, `
		SELECT `+snapshotColumns+` FROM report_snapshots WHERE tenant_id = $1 AND id = $2
	`, tenantID, id), &s)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &s, nil
}

// ListForReport returns the tenant's snapshots for one report key, newest first
// (the report page's snapshot panel + revision chain). id breaks ties for stable
// paging. When stationID is non-nil/non-empty the result is SCOPED to that station
// (filters_used->>'station_id'), mirroring LatestSignedOffForReport /
// MaxRevisionForChain — so a station-scoped report's list never returns another
// station's snapshot metadata (notes, signer/capturer ids, hashes, timestamps) to
// an actor authorized only for their own station. A nil stationID lists every
// scope (tenant-wide reports, or an explicit cross-scope listing).
func (r *Repo) ListForReport(ctx context.Context, tenantID uuid.UUID, reportKey string, stationID *string, limit, offset int) ([]Snapshot, error) {
	var rows pgx.Rows
	var err error
	if stationID != nil && *stationID != "" {
		rows, err = r.pool.Query(ctx, `
			SELECT `+snapshotColumns+` FROM report_snapshots
			WHERE tenant_id = $1 AND report_key = $2
			  AND filters_used->>'station_id' = $3
			ORDER BY captured_at DESC, id LIMIT $4 OFFSET $5
		`, tenantID, reportKey, *stationID, limit, offset)
	} else {
		rows, err = r.pool.Query(ctx, `
			SELECT `+snapshotColumns+` FROM report_snapshots
			WHERE tenant_id = $1 AND report_key = $2
			ORDER BY captured_at DESC, id LIMIT $3 OFFSET $4
		`, tenantID, reportKey, limit, offset)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collect(rows)
}

// ListSignedOff returns the tenant's most recent SIGNED-OFF snapshots across all
// reports (the hub "Locked" rail). The caller permission-filters the result so a
// signed-off snapshot never leaks a report the actor cannot run live.
func (r *Repo) ListSignedOff(ctx context.Context, tenantID uuid.UUID, limit int) ([]Snapshot, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT `+snapshotColumns+` FROM report_snapshots
		WHERE tenant_id = $1 AND status = 'signed_off'
		ORDER BY signed_off_at DESC, id LIMIT $2
	`, tenantID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collect(rows)
}

// LatestSignedOffForReport returns the most recent signed-off snapshot for a
// report key (and optional station filter), or ErrNotFound. Drives the lock
// badge on a report view: a present row means a signed-off snapshot exists for
// the current report/scope. The optional stationID matches the filters_used
// station_id so the badge is scope-accurate.
func (r *Repo) LatestSignedOffForReport(ctx context.Context, tenantID uuid.UUID, reportKey string, stationID *string) (*Snapshot, error) {
	var s Snapshot
	var err error
	if stationID != nil && *stationID != "" {
		err = scanSnapshot(r.pool.QueryRow(ctx, `
			SELECT `+snapshotColumns+` FROM report_snapshots
			WHERE tenant_id = $1 AND report_key = $2 AND status = 'signed_off'
			  AND filters_used->>'station_id' = $3
			ORDER BY signed_off_at DESC, id LIMIT 1
		`, tenantID, reportKey, *stationID), &s)
	} else {
		err = scanSnapshot(r.pool.QueryRow(ctx, `
			SELECT `+snapshotColumns+` FROM report_snapshots
			WHERE tenant_id = $1 AND report_key = $2 AND status = 'signed_off'
			ORDER BY signed_off_at DESC, id LIMIT 1
		`, tenantID, reportKey), &s)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &s, nil
}

// MaxRevisionForChain returns the highest revision currently in the chain a new
// capture would extend (matched by report_key + the station_id filter, so each
// report/scope has its own revision sequence). Returns 0 when no prior snapshot
// exists. Used by the handler to compute the next revision on a reopen+recapture.
func (r *Repo) MaxRevisionForChain(ctx context.Context, tenantID uuid.UUID, reportKey string, stationID *string) (int, error) {
	var maxRev *int
	var err error
	if stationID != nil && *stationID != "" {
		err = r.pool.QueryRow(ctx, `
			SELECT max(revision) FROM report_snapshots
			WHERE tenant_id = $1 AND report_key = $2 AND filters_used->>'station_id' = $3
		`, tenantID, reportKey, *stationID).Scan(&maxRev)
	} else {
		err = r.pool.QueryRow(ctx, `
			SELECT max(revision) FROM report_snapshots
			WHERE tenant_id = $1 AND report_key = $2 AND filters_used->>'station_id' IS NULL
		`, tenantID, reportKey).Scan(&maxRev)
	}
	if err != nil {
		return 0, err
	}
	if maxRev == nil {
		return 0, nil
	}
	return *maxRev, nil
}

// SignOffTx transitions a DRAFT (or reopened) snapshot to 'signed_off', stamping
// the signer + time, within the supplied transaction (so the transition and its
// audit commit atomically). The captured payload is untouched — the DB trigger
// guarantees it. Returns ErrNotFound when no matching draft/reopened row exists
// (already signed off, or absent), so the caller can 404/409 honestly.
func (r *Repo) SignOffTx(ctx context.Context, tx pgx.Tx, tenantID, id, signedBy uuid.UUID) (*Snapshot, error) {
	var s Snapshot
	err := scanSnapshot(tx.QueryRow(ctx, `
		UPDATE report_snapshots SET
		    status        = 'signed_off',
		    signed_off_by = $3,
		    signed_off_at = now()
		WHERE tenant_id = $1 AND id = $2 AND status IN ('draft', 'reopened')
		RETURNING `+snapshotColumns,
		tenantID, id, signedBy), &s)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &s, nil
}

// ReopenTx transitions a SIGNED-OFF snapshot to 'reopened' with a required
// correction note, clearing the sign-off stamp (so the chk_signoff constraint
// holds), within the supplied transaction. The captured payload stays immutable;
// a corrected figure is captured as the NEXT revision, never an overwrite.
// Returns ErrNotFound when no matching signed-off row exists.
func (r *Repo) ReopenTx(ctx context.Context, tx pgx.Tx, tenantID, id uuid.UUID, note string) (*Snapshot, error) {
	var s Snapshot
	err := scanSnapshot(tx.QueryRow(ctx, `
		UPDATE report_snapshots SET
		    status          = 'reopened',
		    signed_off_by   = NULL,
		    signed_off_at   = NULL,
		    correction_note = $3
		WHERE tenant_id = $1 AND id = $2 AND status = 'signed_off'
		RETURNING `+snapshotColumns,
		tenantID, id, note), &s)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &s, nil
}

// collect drains a result set into a slice of snapshots.
func collect(rows pgx.Rows) ([]Snapshot, error) {
	out := []Snapshot{}
	for rows.Next() {
		var s Snapshot
		if err := scanSnapshot(rows, &s); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}
