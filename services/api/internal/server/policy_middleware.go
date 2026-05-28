package server

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/japharyroman/fuelgrid-os/internal/identity"
	"github.com/japharyroman/fuelgrid-os/internal/identity/policy"
)

// stationExtractor pulls a station id from the request — typically from a
// chi URL parameter, but custom extractors can read it from the body too.
type stationExtractor func(*http.Request) (*uuid.UUID, error)

// stationFromURLParam extracts a station id from the named chi URL param.
// Returns (nil, nil) when the param is missing so callers can layer this
// over routes where station id is optional.
func stationFromURLParam(param string) stationExtractor {
	return func(r *http.Request) (*uuid.UUID, error) {
		raw := chi.URLParam(r, param)
		if raw == "" {
			return nil, nil
		}
		id, err := uuid.Parse(raw)
		if err != nil {
			return nil, err
		}
		return &id, nil
	}
}

// authorizeStation runs a station-scoped permission check from inside a
// handler, for the cases the URL-param middleware can't cover: the station
// id lives in the request body (create) or on the target row (update /
// delete), so it's only known after decoding or loading. Returns true when
// the actor is allowed; otherwise it writes the error response and returns
// false.
func (s *Server) authorizeStation(w http.ResponseWriter, r *http.Request, actor identity.Actor, perm string, stationID uuid.UUID) bool {
	err := s.policy.Can(r.Context(), actor, perm, policy.AtStation(stationID))
	if err == nil {
		return true
	}
	switch {
	case errors.Is(err, policy.ErrForbidden):
		writeError(w, http.StatusForbidden, "forbidden")
	case errors.Is(err, identity.ErrUnauthenticated):
		writeError(w, http.StatusUnauthorized, "authentication required")
	default:
		s.logger.Error("policy check", "error", err, "permission", perm)
		writeError(w, http.StatusInternalServerError, "authorization error")
	}
	return false
}

// requirePermissionHeld builds a middleware that allows the request when the
// actor *holds* the permission in any scope, without demanding a specific
// station. It's the right gate for tenant-wide catalogue reads (list/get of
// companies, stations, products, tanks, …): the rows are already tenant-
// scoped by the repos, and a plain Can(code, {}) would wrongly reject a
// station-scoped permission like station.read for lack of a station id (see
// policy.Can rule 2). Per-station writes/reads still use requirePermission
// with a real station extractor.
//
//nolint:unparam // perm is "station.read" for every current caller; kept parameterized for the tenant-wide reads later stages will gate differently.
func (s *Server) requirePermissionHeld(perm string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			actor, err := identity.Require(r.Context())
			if err != nil {
				writeError(w, http.StatusUnauthorized, "authentication required")
				return
			}
			ps, err := s.policy.LoadFor(r.Context(), actor)
			if err != nil {
				if errors.Is(err, identity.ErrUnauthenticated) {
					writeError(w, http.StatusUnauthorized, "authentication required")
					return
				}
				s.logger.Error("policy load", "error", err, "permission", perm)
				writeError(w, http.StatusInternalServerError, "authorization error")
				return
			}
			if !ps.HasPermission(perm) {
				writeError(w, http.StatusForbidden, "forbidden")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// requirePermission builds a middleware enforcing a single permission.
// extract may be nil for tenant-wide permissions.
func (s *Server) requirePermission(perm string, extract stationExtractor) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			actor, err := identity.Require(r.Context())
			if err != nil {
				writeError(w, http.StatusUnauthorized, "authentication required")
				return
			}

			res := policy.Resource{}
			if extract != nil {
				sid, err := extract(r)
				if err != nil {
					writeError(w, http.StatusBadRequest, "invalid station id")
					return
				}
				res.StationID = sid
			}

			if err := s.policy.Can(r.Context(), actor, perm, res); err != nil {
				switch {
				case errors.Is(err, policy.ErrForbidden):
					writeError(w, http.StatusForbidden, "forbidden")
				case errors.Is(err, identity.ErrUnauthenticated):
					writeError(w, http.StatusUnauthorized, "authentication required")
				default:
					s.logger.Error("policy check", "error", err, "permission", perm)
					writeError(w, http.StatusInternalServerError, "authorization error")
				}
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
