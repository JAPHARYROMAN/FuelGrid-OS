package policy

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

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

	if err := l.pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM user_roles ur
			JOIN roles r ON r.id = ur.role_id
			WHERE ur.user_id = $1
			  AND ur.tenant_id = $2
			  AND r.is_system
			  AND r.code = 'system_admin'
		)
	`, actor.UserID, actor.TenantID).Scan(&ps.IsSystemAdmin); err != nil {
		return PermissionSet{}, err
	}

	// Permissions granted by the actor's roles. system_admin is a superuser
	// role: it receives every permission from the catalog dynamically, so new
	// permissions do not depend on seed-time role_permissions fan-out.
	var (
		rows pgx.Rows
		err  error
	)
	permissionSQL := `
		SELECT DISTINCT p.code, p.station_scoped
		FROM user_roles ur
		JOIN role_permissions rp ON rp.role_id = ur.role_id
		JOIN permissions p       ON p.id      = rp.permission_id
		WHERE ur.user_id = $1 AND ur.tenant_id = $2
	`
	if ps.IsSystemAdmin {
		rows, err = l.pool.Query(ctx, `SELECT p.code, p.station_scoped FROM permissions p`)
	} else {
		rows, err = l.pool.Query(ctx, permissionSQL, actor.UserID, actor.TenantID)
	}
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
		SELECT station_id FROM user_station_access WHERE user_id = $1 AND tenant_id = $2
	`, actor.UserID, actor.TenantID)
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
			WHERE ur.user_id = $1 AND ur.tenant_id = $2 AND r.tenant_wide
		)
	`, actor.UserID, actor.TenantID).Scan(&ps.TenantWide); err != nil {
		return PermissionSet{}, err
	}
	if ps.IsSystemAdmin {
		ps.TenantWide = true
	}

	return ps, nil
}
