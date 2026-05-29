// Package risk is the data layer for Phase 10 — Risk, Fraud & Intelligence. It
// normalizes operational events into idempotent risk signals, runs explainable
// detection packs that raise alerts linked to immutable source facts, scores
// entities, and supports investigation cases. Risk never rewrites source data;
// closing an alert or case records a disposition.
package risk

import (
	"errors"

	"github.com/japharyroman/fuelgrid-os/internal/database"
)

var (
	ErrNotFound = errors.New("risk: not found")
	ErrBadState = errors.New("risk: invalid state transition")
	// ErrDispositionRequired is returned when an alert is resolved or dismissed
	// without a disposition — a terminal outcome must record why (RISK-003).
	ErrDispositionRequired = errors.New("risk: disposition required to resolve or dismiss")
)

type Repo struct{ pool *database.Pool }

func New(pool *database.Pool) *Repo { return &Repo{pool: pool} }

func nullableMoney(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
