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
