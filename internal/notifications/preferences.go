package notifications

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/japharyroman/fuelgrid-os/internal/database"
)

// Preference is one per-user notification delivery toggle: for a (category,
// channel) pair, whether delivery is enabled and an optional local quiet window
// (HH:MM 24h) during which the channel should suppress delivery. QuietHoursStart
// / QuietHoursEnd are nil together when no quiet hours are set.
type Preference struct {
	ID              uuid.UUID
	Category        string
	Channel         string
	Enabled         bool
	QuietHoursStart *string
	QuietHoursEnd   *string
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// PreferenceInput is one upserted toggle. The repo validates the keys are
// non-empty and that quiet hours, if present, are a complete pair — leaving the
// CHECK constraints as the final backstop.
type PreferenceInput struct {
	Category        string
	Channel         string
	Enabled         bool
	QuietHoursStart *string
	QuietHoursEnd   *string
}

const preferenceColumns = `id, category, channel, enabled, quiet_hours_start, quiet_hours_end, created_at, updated_at`

func scanPreference(row pgx.Row) (Preference, error) {
	var p Preference
	err := row.Scan(&p.ID, &p.Category, &p.Channel, &p.Enabled,
		&p.QuietHoursStart, &p.QuietHoursEnd, &p.CreatedAt, &p.UpdatedAt)
	return p, err
}

// PreferenceRepo is the data layer for per-user notification preferences. It is
// distinct from the feed Repo but lives in the same package because both are the
// self-service notification surface. Every method is scoped to (tenant, user) —
// a user can only ever read or write their OWN preferences.
type PreferenceRepo struct{ pool *database.Pool }

// NewPreferenceRepo constructs the preferences repository.
func NewPreferenceRepo(pool *database.Pool) *PreferenceRepo { return &PreferenceRepo{pool: pool} }

// ListForUser returns the caller's full preference set, ordered for stable UI
// rendering (category then channel).
func (r *PreferenceRepo) ListForUser(ctx context.Context, tenantID, userID uuid.UUID) ([]Preference, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT `+preferenceColumns+`
		FROM notification_preferences
		WHERE tenant_id = $1 AND user_id = $2
		ORDER BY category, channel`,
		tenantID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Preference
	for rows.Next() {
		p, err := scanPreference(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// Upsert inserts or updates one (category, channel) preference for the caller
// and returns the resulting row. Keyed on (tenant, user, category, channel), so
// re-toggling a category/channel updates the existing row rather than creating a
// duplicate. Runs in tx so the handler can audit in the same transaction.
func (r *PreferenceRepo) Upsert(ctx context.Context, tx pgx.Tx, tenantID, userID uuid.UUID, in PreferenceInput) (Preference, error) {
	row := tx.QueryRow(ctx, `
		INSERT INTO notification_preferences
			(tenant_id, user_id, category, channel, enabled, quiet_hours_start, quiet_hours_end)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (tenant_id, user_id, category, channel) DO UPDATE
		SET enabled           = EXCLUDED.enabled,
		    quiet_hours_start = EXCLUDED.quiet_hours_start,
		    quiet_hours_end   = EXCLUDED.quiet_hours_end
		RETURNING `+preferenceColumns,
		tenantID, userID, in.Category, in.Channel, in.Enabled, in.QuietHoursStart, in.QuietHoursEnd)
	return scanPreference(row)
}
