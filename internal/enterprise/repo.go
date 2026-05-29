// Package enterprise is the data layer for Phase 9 — Chain & Enterprise
// Command: station groups, delegated enterprise scopes, the generic approval
// engine, enterprise read-model projections, central pricing/procurement, stock
// transfers, and consolidated finance. It coordinates and governs station
// workflows across companies, regions, and stations without rebuilding them.
package enterprise

import (
	"errors"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/japharyroman/fuelgrid-os/internal/database"
)

var (
	ErrNotFound     = errors.New("enterprise: not found")
	ErrConflict     = errors.New("enterprise: conflict")
	ErrBadState     = errors.New("enterprise: invalid state transition")
	ErrSelfApproval = errors.New("enterprise: requester cannot decide their own approval request")
)

type Repo struct{ pool *database.Pool }

func New(pool *database.Pool) *Repo { return &Repo{pool: pool} }

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

func nullableMoney(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
