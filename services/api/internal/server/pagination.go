package server

import (
	"net/http"
	"strconv"
)

// Pagination defaults used when config supplies no (or a zero) value. They
// keep the helper usable in tests and in any code path that builds a Server
// without fully populating config.
const (
	fallbackDefaultPageSize = 50
	fallbackMaxPageSize     = 200
)

// parsePage extracts limit/offset from the request query string and writes a
// 400 (returning ok=false) when a param is present but not a valid integer.
//
// Rules:
//   - ?limit absent      -> default page size (config, fallback 50)
//   - ?limit <= 0        -> default page size
//   - ?limit > max       -> clamped down to max page size (config, fallback 200)
//   - ?offset absent/<=0 -> 0
//   - garbage in either  -> 400 written, ok=false
//
// It is intentionally a method on *Server so it can read the configured page
// sizes; the page-clamping logic itself lives in clampPageSize for unit tests.
func (s *Server) parsePage(w http.ResponseWriter, r *http.Request) (limit, offset int, ok bool) {
	defSize, maxSize := s.pageSizes()
	q := r.URL.Query()

	limit = defSize
	if raw := q.Get("limit"); raw != "" {
		v, err := strconv.Atoi(raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid limit: must be an integer")
			return 0, 0, false
		}
		limit = clampPageSize(v, defSize, maxSize)
	}

	if raw := q.Get("offset"); raw != "" {
		v, err := strconv.Atoi(raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid offset: must be an integer")
			return 0, 0, false
		}
		if v > 0 {
			offset = v
		}
	}

	return limit, offset, true
}

// pageSizes returns the effective default and max page sizes, applying the
// package fallbacks whenever config carries a non-positive (e.g. zero) value.
func (s *Server) pageSizes() (defSize, maxSize int) {
	defSize = s.cfg.DefaultPageSize
	if defSize <= 0 {
		defSize = fallbackDefaultPageSize
	}
	maxSize = s.cfg.MaxPageSize
	if maxSize <= 0 {
		maxSize = fallbackMaxPageSize
	}
	if defSize > maxSize {
		defSize = maxSize
	}
	return defSize, maxSize
}

// clampPageSize normalizes a requested limit: non-positive falls back to the
// default, over-max clamps down to max.
func clampPageSize(requested, defSize, maxSize int) int {
	if requested <= 0 {
		return defSize
	}
	if requested > maxSize {
		return maxSize
	}
	return requested
}

// writePaged writes the standard paged JSON envelope. hasMore is true when the
// returned window may not be the tail of the result set, i.e. the caller asked
// for a full page (count == limit); callers that know the true total can pass
// their own value via writePagedMore.
func writePaged(w http.ResponseWriter, status int, items any, count, limit, offset int) {
	writePagedMore(w, status, items, count, limit, offset, count == limit && limit > 0)
}

// writePagedMore writes the paged envelope with an explicit has_more flag, for
// callers that can compute it precisely (e.g. by fetching limit+1 rows).
func writePagedMore(w http.ResponseWriter, status int, items any, count, limit, offset int, hasMore bool) {
	writeJSON(w, status, map[string]any{
		"items":    items,
		"count":    count,
		"limit":    limit,
		"offset":   offset,
		"has_more": hasMore,
	})
}
