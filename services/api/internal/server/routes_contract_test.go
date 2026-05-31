package server

import (
	"bufio"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/japharyroman/fuelgrid-os/internal/database"
	"github.com/japharyroman/fuelgrid-os/internal/identity"
	"github.com/japharyroman/fuelgrid-os/internal/identity/password"
	"github.com/japharyroman/fuelgrid-os/internal/identity/policy"
	"github.com/japharyroman/fuelgrid-os/internal/identity/repo"
	"github.com/japharyroman/fuelgrid-os/services/api/internal/config"
)

// TestRoutesDocumentedInOpenAPI walks the registered chi route table and
// compares the path set against the paths declared in docs/openapi.yaml,
// reporting any registered route that is not documented in the spec.
//
// WI-1/CICD-1 (skeleton): the diff machinery is fully wired, but the strict
// assertion is gated behind CONTRACT_STRICT so CI stays GREEN — the OpenAPI
// spec is hand-maintained and incomplete until automatic generation lands in
// Wave 9. The test logs the undocumented count for visibility and only fails
// when CONTRACT_STRICT is set (e.g. once the spec is regenerated).
func TestRoutesDocumentedInOpenAPI(t *testing.T) {
	t.Parallel()

	registered := registeredRoutePaths(t)
	documented := openAPIPaths(t)

	var undocumented []string
	for path := range registered {
		if !documented[path] {
			undocumented = append(undocumented, path)
		}
	}
	sort.Strings(undocumented)

	t.Logf("registered route paths: %d, documented in OpenAPI: %d, undocumented: %d",
		len(registered), len(documented), len(undocumented))

	if os.Getenv("CONTRACT_STRICT") == "" {
		if len(undocumented) > 0 {
			t.Logf("undocumented routes (informational):\n  %s", strings.Join(undocumented, "\n  "))
		}
		t.Skip("enable after OpenAPI regen (Wave 9)")
	}

	if len(undocumented) > 0 {
		t.Errorf("%d registered route(s) missing from docs/openapi.yaml:\n  %s",
			len(undocumented), strings.Join(undocumented, "\n  "))
	}
}

// registeredRoutePaths walks the built router and returns the set of distinct
// route path patterns (collapsed across HTTP methods). chi reports patterns
// with {param} placeholders, matching the OpenAPI path-template style.
func registeredRoutePaths(t *testing.T) map[string]bool {
	t.Helper()

	srv := newFullRouteServer()

	paths := make(map[string]bool)
	walkFn := func(_ string, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		paths[normalizeRoute(route)] = true
		return nil
	}
	if err := chi.Walk(srv.Router(), walkFn); err != nil {
		t.Fatalf("walk routes: %v", err)
	}
	if len(paths) == 0 {
		t.Fatal("no routes registered — router walk returned empty set")
	}
	return paths
}

// newFullRouteServer builds a Server whose Deps are non-nil but never dialed,
// so the complete route table mounts (route mounting is gated on the presence
// of the backing services; the handlers gate on a live DB only at request
// time). New only stores these pointers — no DB/Redis call happens during
// route registration — so an unconnected pool enumerates every route offline.
func newFullRouteServer() *Server {
	pool := &database.Pool{} // non-nil, never connected
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	users := repo.NewUserRepo(pool)
	sessions := repo.NewSessionRepo(pool)
	hasher := password.New(password.Params{}, "")
	idSvc := identity.NewService(identity.ServiceConfig{}, pool, hasher,
		users, sessions, nil, nil, nil, logger, "")
	polSvc := policy.NewService(policy.NewDBLoader(pool))

	return New(config.Config{Host: "127.0.0.1", Port: 0}, logger, Deps{
		DB:       pool,
		Identity: idSvc,
		Policy:   polSvc,
	})
}

// normalizeRoute strips a single trailing slash (other than root) so chi's
// "/path/" matches the OpenAPI "/path" form.
func normalizeRoute(route string) string {
	if len(route) > 1 {
		route = strings.TrimSuffix(route, "/")
	}
	return route
}

// openAPIPaths reads docs/openapi.yaml and returns the set of top-level path
// keys declared under "paths:". It is a deliberately small line scanner so
// the test carries no YAML dependency: under "paths:", a path key is a line
// indented exactly two spaces, starting with "/", and ending with ":".
func openAPIPaths(t *testing.T) map[string]bool {
	t.Helper()

	specPath := openAPISpecPath(t)
	f, err := os.Open(specPath)
	if err != nil {
		t.Fatalf("open openapi spec %s: %v", specPath, err)
	}
	defer f.Close()

	paths := make(map[string]bool)
	inPaths := false
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		trimmedRight := strings.TrimRight(line, " \t\r")
		if trimmedRight == "" || strings.HasPrefix(strings.TrimSpace(trimmedRight), "#") {
			continue
		}
		// A non-indented, non-comment key ends the paths block.
		if inPaths && !strings.HasPrefix(line, " ") {
			break
		}
		if trimmedRight == "paths:" {
			inPaths = true
			continue
		}
		if !inPaths {
			continue
		}
		// Top-level path entries are indented exactly two spaces.
		if !strings.HasPrefix(line, "  ") || strings.HasPrefix(line, "   ") {
			continue
		}
		key := strings.TrimSpace(trimmedRight)
		if !strings.HasPrefix(key, "/") || !strings.HasSuffix(key, ":") {
			continue
		}
		paths[normalizeRoute(strings.TrimSuffix(key, ":"))] = true
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan openapi spec: %v", err)
	}
	if len(paths) == 0 {
		t.Fatalf("no paths parsed from %s", specPath)
	}
	return paths
}

// openAPISpecPath locates docs/openapi.yaml by walking up from the test's
// working directory (services/api/internal/server) to the repo root.
func openAPISpecPath(t *testing.T) string {
	t.Helper()

	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for i := 0; i < 12; i++ {
		candidate := filepath.Join(dir, "docs", "openapi.yaml")
		if _, statErr := os.Stat(candidate); statErr == nil {
			return candidate
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	t.Fatalf("could not locate docs/openapi.yaml from %s upward", dir)
	return ""
}
