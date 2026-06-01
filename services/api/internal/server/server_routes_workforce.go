package server

import "github.com/go-chi/chi/v5"

// registerWorkforceRoutes (Phase 11): station workforce — employees, the three
// shift teams, the rotation anchor, and the deterministic roster.
//
// Reads ride station.read (held; rows are tenant-scoped, station authz is done
// in-handler). Writes (employee CRUD, ensuring teams, setting team members, and
// the rotation anchor) reuse station.manage — the same tenant-wide management
// permission station CRUD uses — consistent with the org-management surface.
func (s *Server) registerWorkforceRoutes(r chi.Router) {
	// Employees — per-station list/create + id-based patch.
	r.With(s.requirePermissionHeld("station.read")).
		Get("/stations/{stationID}/employees", s.handleListEmployees)
	r.With(s.requirePermission("station.manage", nil)).Group(func(r chi.Router) {
		r.Post("/stations/{stationID}/employees", s.handleCreateEmployee)
		r.Patch("/employees/{employeeID}", s.handleUpdateEmployee)
	})

	// Teams — list + ensure the three rotation teams.
	r.With(s.requirePermissionHeld("station.read")).
		Get("/stations/{stationID}/teams", s.handleListTeams)
	r.With(s.requirePermission("station.manage", nil)).Group(func(r chi.Router) {
		r.Post("/stations/{stationID}/teams", s.handleEnsureTeams)
		r.Put("/teams/{teamID}/members", s.handleSetTeamMembers)
	})

	// Rotation anchor — read + set the cycle-day-0 date.
	r.With(s.requirePermissionHeld("station.read")).
		Get("/stations/{stationID}/rotation-anchor", s.handleGetRotationAnchor)
	r.With(s.requirePermission("station.manage", nil)).
		Put("/stations/{stationID}/rotation-anchor", s.handleSetRotationAnchor)

	// Roster preview + the resolved scheduled team for a date+slot.
	r.With(s.requirePermissionHeld("station.read")).Group(func(r chi.Router) {
		r.Get("/stations/{stationID}/roster", s.handleRoster)
		r.Get("/stations/{stationID}/scheduled-team", s.handleScheduledTeam)
	})
}
