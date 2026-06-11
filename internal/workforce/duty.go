package workforce

// Rotation duty resolution for a single USER (Mobile Attendant App, Phase 1).
// The attendant home screen needs to answer "am I expected at work today, and
// for which slot?" without any station-wide read: the lookup is keyed by the
// actor's own employee record (employees.user_id) and walks employee → team →
// station rotation anchor, then computes the deterministic 3-team rotation
// for the date.

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// UserDuty is one user's rotation duty for a date. OnDuty reports whether
// their team works that date; Slot is only meaningful when OnDuty is true
// (the third team rests).
type UserDuty struct {
	StationID uuid.UUID
	TeamID    uuid.UUID
	TeamName  string
	OnDuty    bool
	Slot      Slot
}

// DutyForUser resolves whether the user's rotation team is on duty on the
// given date (only its calendar date is used). It returns (nil, nil) when the
// user cannot be rostered at all — no active employee record linked to the
// user, no team membership, or a station without a rotation anchor — which
// callers treat as "off duty / not part of the rotation".
func (r *Repo) DutyForUser(ctx context.Context, tenantID, userID uuid.UUID, date time.Time) (*UserDuty, error) {
	var (
		stationID uuid.UUID
		teamID    uuid.UUID
		teamName  string
		order     int
		anchor    *time.Time
	)
	err := r.pool.QueryRow(ctx, `
		SELECT e.station_id, t.id, t.name, t.rotation_order, st.rotation_anchor_date
		FROM employees e
		JOIN shift_team_members m ON m.tenant_id = e.tenant_id AND m.employee_id = e.id
		JOIN shift_teams t        ON t.tenant_id = m.tenant_id AND t.id = m.team_id
		JOIN stations st          ON st.tenant_id = e.tenant_id AND st.id = e.station_id
		WHERE e.tenant_id = $1 AND e.user_id = $2 AND e.status = 'active'
	`, tenantID, userID).Scan(&stationID, &teamID, &teamName, &order, &anchor)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if anchor == nil {
		// Rotation not configured for the station yet.
		return nil, nil
	}

	duty := &UserDuty{StationID: stationID, TeamID: teamID, TeamName: teamName}
	rot := Rotation(*anchor, date)
	switch order {
	case rot.MorningOrder:
		duty.OnDuty = true
		duty.Slot = SlotMorning
	case rot.EveningOrder:
		duty.OnDuty = true
		duty.Slot = SlotEvening
	}
	return duty, nil
}
