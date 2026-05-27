package server

import (
	"net/http"
	"sort"

	"github.com/google/uuid"

	"github.com/japharyroman/fuelgrid-os/internal/identity"
)

// mePermissionsResponse is what the frontend uses to gate UI elements.
// Tokens never need a database round-trip to decide whether to show a
// button — this payload is all they need.
type mePermissionsResponse struct {
	Permissions []permissionItem `json:"permissions"`
	StationIDs  []uuid.UUID      `json:"station_ids,omitempty"`
	TenantWide  bool             `json:"tenant_wide"`
}

type permissionItem struct {
	Code          string `json:"code"`
	StationScoped bool   `json:"station_scoped"`
}

func (s *Server) handleMePermissions(w http.ResponseWriter, r *http.Request) {
	actor, err := identity.Require(r.Context())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	ps, err := s.policy.LoadFor(r.Context(), actor)
	if err != nil {
		s.logger.Error("load permissions", "error", err)
		writeError(w, http.StatusInternalServerError, "could not load permissions")
		return
	}

	resp := mePermissionsResponse{TenantWide: ps.TenantWide}
	for code := range ps.Permissions {
		resp.Permissions = append(resp.Permissions, permissionItem{
			Code:          code,
			StationScoped: ps.StationScoped[code],
		})
	}
	// Deterministic order — easier to grep in logs / test outputs.
	sort.Slice(resp.Permissions, func(i, j int) bool {
		return resp.Permissions[i].Code < resp.Permissions[j].Code
	})

	for sid := range ps.StationIDs {
		resp.StationIDs = append(resp.StationIDs, sid)
	}
	sort.Slice(resp.StationIDs, func(i, j int) bool {
		return resp.StationIDs[i].String() < resp.StationIDs[j].String()
	})

	writeJSON(w, http.StatusOK, resp)
}
