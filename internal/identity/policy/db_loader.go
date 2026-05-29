package policy

import (
	"context"

	"github.com/google/uuid"

	"github.com/japharyroman/fuelgrid-os/internal/database"
	"github.com/japharyroman/fuelgrid-os/internal/identity"
)

// DBLoader pulls a user's permissions, station scope, and tenant-wide flag
// from Postgres. A few round-trips today; once observability is in place
// we'll know whether the per-request cost is worth caching in Redis.
type DBLoader struct {
	pool *database.Pool
}

// NewDBLoader wires a DBLoader against the supplied pool.
func NewDBLoader(pool *database.Pool) *DBLoader {
	return &DBLoader{pool: pool}
}

// Load assembles the PermissionSet for the actor.
func (l *DBLoader) Load(ctx context.Context, actor identity.Actor) (PermissionSet, error) {
	ps := PermissionSet{
		UserID:        actor.UserID,
		TenantID:      actor.TenantID,
		Permissions:   map[string]bool{},
		StationIDs:    map[uuid.UUID]bool{},
		StationScoped: map[string]bool{},
	}

	// Permissions granted by the actor's roles. station_scoped is fetched
	// alongside the code so the evaluator can route the scope check.
	rows, err := l.pool.Query(ctx, `
		SELECT DISTINCT p.code, p.station_scoped
		FROM user_roles ur
		JOIN role_permissions rp ON rp.role_id = ur.role_id
		JOIN permissions p       ON p.id      = rp.permission_id
		WHERE ur.user_id = $1
	`, actor.UserID)
	if err != nil {
		return PermissionSet{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var code string
		var scoped bool
		if err := rows.Scan(&code, &scoped); err != nil {
			return PermissionSet{}, err
		}
		ps.Permissions[code] = true
		ps.StationScoped[code] = scoped
	}
	if err := rows.Err(); err != nil {
		return PermissionSet{}, err
	}

	// Explicit station scope. For a station-restricted actor these are the
	// only stations they may touch for station_scoped permissions; an actor
	// with no rows and no tenant-wide role has no station-scoped access.
	srows, err := l.pool.Query(ctx, `
		SELECT station_id FROM user_station_access WHERE user_id = $1
	`, actor.UserID)
	if err != nil {
		return PermissionSet{}, err
	}
	defer srows.Close()
	for srows.Next() {
		var sid uuid.UUID
		if err := srows.Scan(&sid); err != nil {
			return PermissionSet{}, err
		}
		ps.StationIDs[sid] = true
	}
	if err := srows.Err(); err != nil {
		return PermissionSet{}, err
	}

	// Tenant-wide reach is an explicit role property (AUTH-20), not the mere
	// absence of station grants. Only holders of a role flagged tenant_wide
	// see across the whole tenant; everyone else is confined to StationIDs.
	if err := l.pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM user_roles ur
			JOIN roles r ON r.id = ur.role_id
			WHERE ur.user_id = $1 AND r.tenant_wide
		)
	`, actor.UserID).Scan(&ps.TenantWide); err != nil {
		return PermissionSet{}, err
	}

	return ps, nil
}
