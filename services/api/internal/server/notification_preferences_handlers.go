package server

import (
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/japharyroman/fuelgrid-os/internal/audit"
	"github.com/japharyroman/fuelgrid-os/internal/identity"
	"github.com/japharyroman/fuelgrid-os/internal/notifications"
)

// notificationCategories is the closed set of notification categories the feed
// subscriber raises (see cmd/api/notifications_subscriber.go). Preferences may
// only target one of these so a typo cannot create an orphan toggle the UI
// never renders.
var notificationCategories = map[string]bool{
	"revenue":  true,
	"shift":    true,
	"risk":     true,
	"incident": true,
	"approval": true,
}

// notificationChannels is the closed set of delivery channels. in_app is the
// topbar feed itself; email is the (subscriber-side) digest channel.
var notificationChannels = map[string]bool{
	"in_app": true,
	"email":  true,
}

// notificationPreferenceDTO is the wire shape for one preference toggle.
type notificationPreferenceDTO struct {
	Category        string    `json:"category"`
	Channel         string    `json:"channel"`
	Enabled         bool      `json:"enabled"`
	QuietHoursStart *string   `json:"quiet_hours_start,omitempty"`
	QuietHoursEnd   *string   `json:"quiet_hours_end,omitempty"`
	UpdatedAt       time.Time `json:"updated_at"`
}

func toNotificationPreferenceDTO(p notifications.Preference) notificationPreferenceDTO {
	return notificationPreferenceDTO{
		Category:        p.Category,
		Channel:         p.Channel,
		Enabled:         p.Enabled,
		QuietHoursStart: p.QuietHoursStart,
		QuietHoursEnd:   p.QuietHoursEnd,
		UpdatedAt:       p.UpdatedAt,
	}
}

// handleListNotificationPreferences returns the caller's own per-category /
// channel notification preferences. Self-service: scoped to the actor's user +
// tenant, no permission gate beyond a session — the same model as the feed.
// The response also echoes the valid category/channel keys so the settings UI
// can render a toggle for every (category, channel) even before any row exists.
func (s *Server) handleListNotificationPreferences(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	if s.notifPrefs == nil {
		writeError(w, http.StatusServiceUnavailable, "notification preferences unavailable")
		return
	}
	rows, err := s.notifPrefs.ListForUser(r.Context(), actor.TenantID, actor.UserID)
	if err != nil {
		s.logger.Error("list notification preferences", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	items := make([]notificationPreferenceDTO, 0, len(rows))
	for _, p := range rows {
		items = append(items, toNotificationPreferenceDTO(p))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items":      items,
		"categories": sortedKeys(notificationCategories),
		"channels":   sortedKeys(notificationChannels),
	})
}

type upsertNotificationPreferenceRequest struct {
	Category        string  `json:"category"`
	Channel         string  `json:"channel"`
	Enabled         *bool   `json:"enabled"`
	QuietHoursStart *string `json:"quiet_hours_start"`
	QuietHoursEnd   *string `json:"quiet_hours_end"`
}

// handleUpsertNotificationPreference creates or updates one (category, channel)
// preference for the caller. Self-service and audited
// (notification.preference_changed). Validates the category/channel against the
// closed sets and the quiet-hours window as a complete HH:MM pair.
func (s *Server) handleUpsertNotificationPreference(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	if s.notifPrefs == nil {
		writeError(w, http.StatusServiceUnavailable, "notification preferences unavailable")
		return
	}
	var req upsertNotificationPreferenceRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	req.Category = strings.TrimSpace(req.Category)
	req.Channel = strings.TrimSpace(req.Channel)
	if !notificationCategories[req.Category] {
		writeError(w, http.StatusBadRequest, "unknown notification category")
		return
	}
	if !notificationChannels[req.Channel] {
		writeError(w, http.StatusBadRequest, "unknown notification channel")
		return
	}
	if req.Enabled == nil {
		writeError(w, http.StatusBadRequest, "enabled is required")
		return
	}
	start, end, ok := normalizeQuietHours(w, req.QuietHoursStart, req.QuietHoursEnd)
	if !ok {
		return
	}

	var out notifications.Preference
	committed := s.txAudit(w, r, audit.TxRecord{
		TenantID: actor.TenantID, ActorID: actor.UserID,
		Action: "notification.preference_changed", EventType: "NotificationPreferenceChanged",
		EntityType: "notification_preference", EntityID: actor.UserID.String(),
		NewValue: map[string]any{
			"category":          req.Category,
			"channel":           req.Channel,
			"enabled":           *req.Enabled,
			"quiet_hours_start": start,
			"quiet_hours_end":   end,
		},
	}, func(tx pgx.Tx) (string, error) {
		p, err := s.notifPrefs.Upsert(r.Context(), tx, actor.TenantID, actor.UserID, notifications.PreferenceInput{
			Category: req.Category, Channel: req.Channel, Enabled: *req.Enabled,
			QuietHoursStart: start, QuietHoursEnd: end,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal error")
			return "", err
		}
		out = p
		return actor.UserID.String(), nil
	})
	if !committed {
		return
	}
	writeJSON(w, http.StatusOK, toNotificationPreferenceDTO(out))
}

// normalizeQuietHours validates the optional quiet-hours window: both bounds
// must be present together (or both absent) and each must be HH:MM 24h. Returns
// the trimmed values and ok=false (after writing a 400) on any violation.
func normalizeQuietHours(w http.ResponseWriter, rawStart, rawEnd *string) (start, end *string, ok bool) {
	hasStart := rawStart != nil && strings.TrimSpace(*rawStart) != ""
	hasEnd := rawEnd != nil && strings.TrimSpace(*rawEnd) != ""
	if hasStart != hasEnd {
		writeError(w, http.StatusBadRequest, "quiet hours require both a start and an end")
		return nil, nil, false
	}
	if !hasStart {
		return nil, nil, true
	}
	s := strings.TrimSpace(*rawStart)
	e := strings.TrimSpace(*rawEnd)
	if !isHHMM(s) || !isHHMM(e) {
		writeError(w, http.StatusBadRequest, "quiet hours must be HH:MM (24h)")
		return nil, nil, false
	}
	return &s, &e, true
}

// isHHMM reports whether s is a 24h HH:MM time string.
func isHHMM(s string) bool {
	if len(s) != 5 || s[2] != ':' {
		return false
	}
	h := (int(s[0]-'0') * 10) + int(s[1]-'0')
	m := (int(s[3]-'0') * 10) + int(s[4]-'0')
	if s[0] < '0' || s[0] > '9' || s[1] < '0' || s[1] > '9' ||
		s[3] < '0' || s[3] > '9' || s[4] < '0' || s[4] > '9' {
		return false
	}
	return h >= 0 && h <= 23 && m >= 0 && m <= 59
}

// sortedKeys returns the keys of a string-set map in ascending order, for a
// stable response.
func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j-1] > out[j]; j-- {
			out[j-1], out[j] = out[j], out[j-1]
		}
	}
	return out
}
