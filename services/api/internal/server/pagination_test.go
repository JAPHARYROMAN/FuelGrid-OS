package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/japharyroman/fuelgrid-os/services/api/internal/config"
)

// newPageServer builds a bare Server carrying only the page-size config the
// helper reads; it never touches a database.
func newPageServer(defSize, maxSize int) *Server {
	return &Server{cfg: config.Config{DefaultPageSize: defSize, MaxPageSize: maxSize}}
}

func TestParsePage(t *testing.T) {
	tests := []struct {
		name             string
		defSize, maxSize int
		query            string
		wantLimit        int
		wantOffset       int
		wantOK           bool
		wantStatus       int // only checked when wantOK is false
	}{
		{name: "default when absent", defSize: 50, maxSize: 200, query: "", wantLimit: 50, wantOffset: 0, wantOK: true},
		{name: "config fallback when zero", defSize: 0, maxSize: 0, query: "", wantLimit: fallbackDefaultPageSize, wantOffset: 0, wantOK: true},
		{name: "explicit limit within range", defSize: 50, maxSize: 200, query: "limit=25", wantLimit: 25, wantOffset: 0, wantOK: true},
		{name: "clamp over-max", defSize: 50, maxSize: 200, query: "limit=1000", wantLimit: 200, wantOffset: 0, wantOK: true},
		{name: "non-positive limit falls to default", defSize: 50, maxSize: 200, query: "limit=0", wantLimit: 50, wantOffset: 0, wantOK: true},
		{name: "negative limit falls to default", defSize: 50, maxSize: 200, query: "limit=-5", wantLimit: 50, wantOffset: 0, wantOK: true},
		{name: "offset parsed", defSize: 50, maxSize: 200, query: "limit=10&offset=30", wantLimit: 10, wantOffset: 30, wantOK: true},
		{name: "negative offset clamps to zero", defSize: 50, maxSize: 200, query: "offset=-10", wantLimit: 50, wantOffset: 0, wantOK: true},
		{name: "garbage limit rejected", defSize: 50, maxSize: 200, query: "limit=abc", wantOK: false, wantStatus: http.StatusBadRequest},
		{name: "garbage offset rejected", defSize: 50, maxSize: 200, query: "offset=xyz", wantOK: false, wantStatus: http.StatusBadRequest},
		{name: "float limit rejected", defSize: 50, maxSize: 200, query: "limit=1.5", wantOK: false, wantStatus: http.StatusBadRequest},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := newPageServer(tc.defSize, tc.maxSize)
			req := httptest.NewRequest(http.MethodGet, "/?"+tc.query, nil)
			rec := httptest.NewRecorder()

			limit, offset, ok := s.parsePage(rec, req)

			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tc.wantOK)
			}
			if !tc.wantOK {
				if rec.Code != tc.wantStatus {
					t.Fatalf("status = %d, want %d", rec.Code, tc.wantStatus)
				}
				return
			}
			if limit != tc.wantLimit {
				t.Errorf("limit = %d, want %d", limit, tc.wantLimit)
			}
			if offset != tc.wantOffset {
				t.Errorf("offset = %d, want %d", offset, tc.wantOffset)
			}
		})
	}
}

func TestWritePagedEnvelope(t *testing.T) {
	tests := []struct {
		name        string
		items       []string
		limit       int
		offset      int
		wantHasMore bool
	}{
		{name: "has_more true when full page", items: []string{"a", "b", "c"}, limit: 3, offset: 0, wantHasMore: true},
		{name: "has_more false when partial page", items: []string{"a", "b"}, limit: 3, offset: 0, wantHasMore: false},
		{name: "has_more false on empty", items: []string{}, limit: 50, offset: 0, wantHasMore: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			writePaged(rec, http.StatusOK, tc.items, len(tc.items), tc.limit, tc.offset)

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200", rec.Code)
			}

			var env struct {
				Items   []string `json:"items"`
				Count   int      `json:"count"`
				Limit   int      `json:"limit"`
				Offset  int      `json:"offset"`
				HasMore bool     `json:"has_more"`
			}
			if err := json.NewDecoder(rec.Body).Decode(&env); err != nil {
				t.Fatalf("decode envelope: %v", err)
			}
			if env.Count != len(tc.items) {
				t.Errorf("count = %d, want %d", env.Count, len(tc.items))
			}
			if env.Limit != tc.limit {
				t.Errorf("limit = %d, want %d", env.Limit, tc.limit)
			}
			if env.Offset != tc.offset {
				t.Errorf("offset = %d, want %d", env.Offset, tc.offset)
			}
			if env.HasMore != tc.wantHasMore {
				t.Errorf("has_more = %v, want %v", env.HasMore, tc.wantHasMore)
			}
		})
	}

	t.Run("explicit has_more via writePagedMore", func(t *testing.T) {
		rec := httptest.NewRecorder()
		writePagedMore(rec, http.StatusOK, []string{"a"}, 1, 1, 0, false)
		var env struct {
			HasMore bool `json:"has_more"`
		}
		if err := json.NewDecoder(rec.Body).Decode(&env); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if env.HasMore {
			t.Errorf("has_more = true, want false (explicit override)")
		}
	})
}
