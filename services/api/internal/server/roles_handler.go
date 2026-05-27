package server

import (
	"net/http"

	"github.com/google/uuid"

	"github.com/japharyroman/fuelgrid-os/internal/identity"
)

type roleDTO struct {
	ID          uuid.UUID `json:"id"`
	Code        string    `json:"code"`
	Name        string    `json:"name"`
	Description *string   `json:"description,omitempty"`
	IsSystem    bool      `json:"is_system"`
	Permissions []string  `json:"permissions"`
}

// handleListRoles returns the role catalogue available to the actor's
// tenant: every system role plus any custom tenant roles. Each role
// carries the permission codes it grants so the admin UI can render a
// matrix.
func (s *Server) handleListRoles(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	rows, err := s.deps.DB.Query(r.Context(), `
		SELECT r.id, r.code, r.name, r.description, r.is_system,
		       coalesce(array_agg(p.code ORDER BY p.code)
		                FILTER (WHERE p.code IS NOT NULL), ARRAY[]::text[]) AS perms
		FROM roles r
		LEFT JOIN role_permissions rp ON rp.role_id = r.id
		LEFT JOIN permissions      p ON p.id      = rp.permission_id
		WHERE r.is_system OR r.tenant_id = $1
		GROUP BY r.id
		ORDER BY r.is_system DESC, r.code
	`, actor.TenantID)
	if err != nil {
		s.logger.Error("list roles", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	defer rows.Close()

	out := make([]roleDTO, 0)
	for rows.Next() {
		var d roleDTO
		if err := rows.Scan(&d.ID, &d.Code, &d.Name, &d.Description, &d.IsSystem, &d.Permissions); err != nil {
			s.logger.Error("scan role", "error", err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		out = append(out, d)
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"items": out, "count": len(out)})
}
