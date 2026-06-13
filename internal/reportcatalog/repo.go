// Package reportcatalog is the data layer for the Reports & Intelligence Center
// registry (Phase 1). It reads the 16 blueprint categories and the catalog of
// reports that back them from the report_categories / reports tables seeded in
// migration 0105.
//
// A category/report row is either a SYSTEM row (tenant_id IS NULL, shared by
// every tenant — the 16 seeded categories) or a tenant override/addition. The
// reads here resolve BOTH: a tenant's own row for a key shadows the system row
// of the same key, so a tenant can re-label or re-target a category without
// touching the shared seed.
//
// No money or litre figure lives in this package's tables — the catalog is
// metadata only. Live figures are computed by the server handler from the
// existing domain repos as exact decimal strings; this package only owns the
// registry and a small set of tenant-wide COUNT aggregates the hub needs.
package reportcatalog

import (
	"context"

	"github.com/google/uuid"

	"github.com/japharyroman/fuelgrid-os/internal/database"
)

// Category is one report category card on the Reports Home (§4.4).
type Category struct {
	Key                string
	Name               string
	Description        string
	SortOrder          int
	Icon               string
	RequiredPermission string
	Availability       string // live | partial | placeholder
	TargetRoute        string
}

// Report is one report in the catalog, backing a category.
type Report struct {
	CategoryKey        string
	Key                string
	Name               string
	Description        string
	Endpoint           string
	RequiredPermission string
	Availability       string // live | partial | placeholder
	IsSystem           bool
}

// Repo reads the report registry and the hub-level count aggregates.
type Repo struct{ pool *database.Pool }

// New constructs the repo over the connection pool.
func New(pool *database.Pool) *Repo { return &Repo{pool: pool} }

// ListCategories returns the report categories visible to the tenant, ordered
// by sort_order. A tenant override (tenant_id = $1) shadows the system row
// (tenant_id IS NULL) of the same key via DISTINCT ON, so each key appears once
// with the tenant's row preferred.
func (r *Repo) ListCategories(ctx context.Context, tenantID uuid.UUID) ([]Category, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT DISTINCT ON (key)
		       key, name, description, sort_order, icon,
		       required_permission, availability, target_route
		FROM report_categories
		WHERE tenant_id IS NULL OR tenant_id = $1
		ORDER BY key, (tenant_id IS NOT NULL) DESC
	`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Category{}
	for rows.Next() {
		var c Category
		if err := rows.Scan(&c.Key, &c.Name, &c.Description, &c.SortOrder, &c.Icon,
			&c.RequiredPermission, &c.Availability, &c.TargetRoute); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sortByOrder(out)
	return out, nil
}

// ListReports returns the catalog reports visible to the tenant, keyed by
// category. A tenant override shadows the system row of the same key.
func (r *Repo) ListReports(ctx context.Context, tenantID uuid.UUID) ([]Report, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT DISTINCT ON (key)
		       category_key, key, name, description, endpoint,
		       required_permission, availability, is_system
		FROM reports
		WHERE tenant_id IS NULL OR tenant_id = $1
		ORDER BY key, (tenant_id IS NOT NULL) DESC
	`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Report{}
	for rows.Next() {
		var rep Report
		if err := rows.Scan(&rep.CategoryKey, &rep.Key, &rep.Name, &rep.Description,
			&rep.Endpoint, &rep.RequiredPermission, &rep.Availability, &rep.IsSystem); err != nil {
			return nil, err
		}
		out = append(out, rep)
	}
	return out, rows.Err()
}

// sortByOrder stable-sorts categories by sort_order (then key) so the hub order
// is deterministic regardless of the DISTINCT ON scan order.
func sortByOrder(cs []Category) {
	for i := 1; i < len(cs); i++ {
		for j := i; j > 0; j-- {
			a, b := cs[j-1], cs[j]
			if a.SortOrder < b.SortOrder || (a.SortOrder == b.SortOrder && a.Key <= b.Key) {
				break
			}
			cs[j-1], cs[j] = cs[j], cs[j-1]
		}
	}
}
