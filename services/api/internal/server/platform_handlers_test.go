package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/japharyroman/fuelgrid-os/services/api/internal/config"
)

func TestRequirePlatformAdmin(t *testing.T) {
	t.Parallel()

	const token = "platform-admin-token"
	tests := []struct {
		name       string
		configured config.Secret
		provided   string
		wantStatus int
	}{
		{
			name:       "accepts configured token",
			configured: config.Secret(token),
			provided:   token,
			wantStatus: http.StatusNoContent,
		},
		{
			name:       "rejects invalid token",
			configured: config.Secret(token),
			provided:   "wrong-token",
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "hides unconfigured route",
			wantStatus: http.StatusNotFound,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			s := &Server{cfg: config.Config{PlatformAdminToken: tc.configured}}
			handler := s.requirePlatformAdmin(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusNoContent)
			}))
			req := httptest.NewRequest(http.MethodPost, "/api/v1/platform/tenants", http.NoBody)
			if tc.provided != "" {
				req.Header.Set("Authorization", "Bearer "+tc.provided)
			}
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d; body = %s", rec.Code, tc.wantStatus, rec.Body.String())
			}
		})
	}
}
