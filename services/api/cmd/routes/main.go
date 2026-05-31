// Command routes enumerates every HTTP route the API registers and prints
// each METHOD + path to stdout, one per line. It builds a Server whose Deps
// are non-nil-but-unconnected so the full route table mounts (route mounting
// is gated on the presence of the backing services, while the handlers gate
// on a live DB only at request time), then walks the resulting chi router.
//
// It backs the route-vs-OpenAPI contract check (WI-1/CICD-1): the same walk
// powers services/api/internal/server/routes_contract_test.go. Run with:
//
//	go run ./services/api/cmd/routes
package main

import (
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"sort"

	"github.com/go-chi/chi/v5"

	"github.com/japharyroman/fuelgrid-os/internal/database"
	"github.com/japharyroman/fuelgrid-os/internal/identity"
	"github.com/japharyroman/fuelgrid-os/internal/identity/password"
	"github.com/japharyroman/fuelgrid-os/internal/identity/policy"
	"github.com/japharyroman/fuelgrid-os/internal/identity/repo"
	"github.com/japharyroman/fuelgrid-os/services/api/internal/config"
	"github.com/japharyroman/fuelgrid-os/services/api/internal/server"
)

func main() {
	if err := run(os.Stdout); err != nil {
		slog.Error("route enumeration failed", "error", err)
		os.Exit(1)
	}
}

func run(out io.Writer) error {
	srv := server.New(config.Config{Host: "127.0.0.1", Port: 0},
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		routeEnumDeps())

	lines := make([]string, 0, 512)
	walkFn := func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		lines = append(lines, fmt.Sprintf("%-7s %s", method, route))
		return nil
	}
	if err := chi.Walk(srv.Router(), walkFn); err != nil {
		return fmt.Errorf("walk routes: %w", err)
	}

	sort.Strings(lines)
	for _, line := range lines {
		if _, err := fmt.Fprintln(out, line); err != nil {
			return err
		}
	}
	return nil
}

// routeEnumDeps builds a Deps whose backing services are non-nil but never
// dialed. server.New only stores these pointers and mounts routes guarded on
// their presence; no DB/Redis call happens during route registration, so an
// unconnected pool is enough to enumerate the complete route table offline.
func routeEnumDeps() server.Deps {
	pool := &database.Pool{} // non-nil, never connected
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	users := repo.NewUserRepo(pool)
	sessions := repo.NewSessionRepo(pool)
	hasher := password.New(password.Params{}, "")
	idSvc := identity.NewService(identity.ServiceConfig{}, pool, hasher,
		users, sessions, nil, nil, nil, logger, "")
	polSvc := policy.NewService(policy.NewDBLoader(pool))

	return server.Deps{
		DB:       pool,
		Identity: idSvc,
		Policy:   polSvc,
	}
}
