package server

import (
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/japharyroman/fuelgrid-os/internal/identity"
	"github.com/japharyroman/fuelgrid-os/internal/notifications"
)

// registerNotificationRoutes mounts the in-app notification feed. Every route
// is scoped to the caller's own user + tenant, so any authenticated user may
// call them — no extra permission gate beyond having a session. It runs inside
// the authenticated self-service group established by registerSelfServiceRoutes.
func (s *Server) registerNotificationRoutes(r chi.Router) {
	r.Get("/notifications", s.handleListNotifications)
	r.Get("/notifications/unread-count", s.handleNotificationUnreadCount)
	r.Post("/notifications/{id}/read", s.handleMarkNotificationRead)
	r.Post("/notifications/read-all", s.handleMarkAllNotificationsRead)

	// Notification preferences (Feature 11.1): the same self-service trust model
	// as the feed — a user reads and writes only their OWN per-category/channel
	// delivery toggles. No extra permission gate beyond the session. Upserts
	// audit notification.preference_changed via the existing writer.
	if s.notifPrefs != nil {
		r.Get("/notifications/preferences", s.handleListNotificationPreferences)
		r.Put("/notifications/preferences", s.handleUpsertNotificationPreference)
	}
}

// notificationDTO is the wire shape for one notification.
type notificationDTO struct {
	ID                uuid.UUID  `json:"id"`
	Type              string     `json:"type"`
	Title             string     `json:"title"`
	Body              string     `json:"body"`
	Severity          string     `json:"severity"`
	RelatedEntityType *string    `json:"related_entity_type,omitempty"`
	RelatedEntityID   *string    `json:"related_entity_id,omitempty"`
	ReadAt            *time.Time `json:"read_at,omitempty"`
	CreatedAt         time.Time  `json:"created_at"`
}

func toNotificationDTO(n notifications.Notification) notificationDTO {
	return notificationDTO{
		ID:                n.ID,
		Type:              n.Type,
		Title:             n.Title,
		Body:              n.Body,
		Severity:          n.Severity,
		RelatedEntityType: n.RelatedEntityType,
		RelatedEntityID:   n.RelatedEntityID,
		ReadAt:            n.ReadAt,
		CreatedAt:         n.CreatedAt,
	}
}

// handleListNotifications returns the caller's notification feed (own +
// tenant-wide), newest first. ?unread=true filters to unread only; paging is
// the standard ?limit/?offset envelope. It fetches limit+1 to compute has_more
// precisely.
func (s *Server) handleListNotifications(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	if s.notifications == nil {
		writeError(w, http.StatusServiceUnavailable, "notifications unavailable")
		return
	}
	limit, offset, ok := s.parsePage(w, r)
	if !ok {
		return
	}
	unreadOnly := r.URL.Query().Get("unread") == "true"

	rows, err := s.notifications.ListForUser(r.Context(), actor.TenantID, actor.UserID, unreadOnly, limit+1, offset)
	if err != nil {
		s.logger.Error("list notifications", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	hasMore := len(rows) > limit
	if hasMore {
		rows = rows[:limit]
	}
	out := make([]notificationDTO, 0, len(rows))
	for _, n := range rows {
		out = append(out, toNotificationDTO(n))
	}
	writePagedMore(w, http.StatusOK, out, len(out), limit, offset, hasMore)
}

// handleNotificationUnreadCount returns the caller's unread notification count
// — the number the topbar bell badge shows. Polled by the UI.
func (s *Server) handleNotificationUnreadCount(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	if s.notifications == nil {
		writeError(w, http.StatusServiceUnavailable, "notifications unavailable")
		return
	}
	count, err := s.notifications.UnreadCount(r.Context(), actor.TenantID, actor.UserID)
	if err != nil {
		s.logger.Error("notification unread count", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"unread_count": count})
}

// handleMarkNotificationRead marks one notification read for the caller. It is
// idempotent: marking an already-read row is a 204, marking an unknown one is a
// 404.
func (s *Server) handleMarkNotificationRead(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	if s.notifications == nil {
		writeError(w, http.StatusServiceUnavailable, "notifications unavailable")
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid notification id")
		return
	}
	if err := s.notifications.MarkRead(r.Context(), actor.TenantID, actor.UserID, id); err != nil {
		if errors.Is(err, notifications.ErrNotFound) {
			writeError(w, http.StatusNotFound, "notification not found")
			return
		}
		s.logger.Error("mark notification read", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleMarkAllNotificationsRead marks every unread notification visible to the
// caller as read and returns how many were updated.
func (s *Server) handleMarkAllNotificationsRead(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	if s.notifications == nil {
		writeError(w, http.StatusServiceUnavailable, "notifications unavailable")
		return
	}
	updated, err := s.notifications.MarkAllRead(r.Context(), actor.TenantID, actor.UserID)
	if err != nil {
		s.logger.Error("mark all notifications read", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"marked_read": updated})
}
