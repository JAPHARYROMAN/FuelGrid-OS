// Command migrate runs FuelGrid OS database migrations.
//
// Usage:
//
//	migrate up                  apply all pending migrations
//	migrate down                roll back one migration
//	migrate down-all            roll back every migration (DESTRUCTIVE)
//	migrate goto <version>      migrate to a specific version
//	migrate force <version>     mark a version as applied (DESTRUCTIVE, recovery)
//	migrate version             print the current version and dirty flag
//
// Destructive commands (down-all, force) refuse to run unless the operator
// opts in by setting MIGRATE_CONFIRM=1 in the environment or by passing the
// --yes flag. This prevents accidental data loss (rolling the schema to zero)
// or silently overriding the dirty-version recovery marker.
//
// DATABASE_URL must be set. Migrations are read from a path resolved as:
//
//  1. MIGRATIONS_PATH env var, if set, or
//  2. <repo>/services/api/migrations, if that directory exists, or
//  3. ./migrations
package main

import (
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"

	"github.com/golang-migrate/migrate/v4"

	// Database driver (postgres) registered via blank import.
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	// File source driver registered via blank import.
	_ "github.com/golang-migrate/migrate/v4/source/file"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: migrate [--yes] <command> [args]")
		fmt.Fprintln(os.Stderr, "commands: up | down | down-all | goto <n> | force <n> | version")
		fmt.Fprintln(os.Stderr, "destructive commands (down-all, force) require MIGRATE_CONFIRM=1 or --yes")
	}
	// Register --yes so the flag parser accepts it; the actual confirmation
	// check scans the raw args (confirmed) so the flag works in any position.
	_ = flag.Bool("yes", false, "confirm a destructive command (down-all, force)")
	flag.Parse()

	if flag.NArg() == 0 {
		flag.Usage()
		os.Exit(2)
	}

	if err := run(os.Args[1:]); err != nil {
		logger.Error("migrate failed", "error", err)
		os.Exit(1)
	}
}

// destructive lists the subcommands that can cause data loss or override
// recovery state, and therefore require explicit operator confirmation.
// Non-destructive commands (up, down, goto, version) are intentionally absent.
var destructive = map[string]bool{
	"down-all": true,
	"force":    true,
}

// confirmed reports whether the operator has explicitly opted into a
// destructive operation, either via MIGRATE_CONFIRM=1 in the environment or by
// passing a --yes (or -yes) flag anywhere in the argument list.
func confirmed(args []string) bool {
	if os.Getenv("MIGRATE_CONFIRM") == "1" {
		return true
	}
	for _, a := range args {
		if a == "--yes" || a == "-yes" {
			return true
		}
	}
	return false
}

// guard refuses a destructive command unless the operator has confirmed. The
// returned error carries a clear, actionable refusal message.
func guard(cmd string, args []string) error {
	if confirmed(args) {
		return nil
	}
	return fmt.Errorf(
		"refusing to run destructive command %q without confirmation; "+
			"set MIGRATE_CONFIRM=1 or pass --yes to proceed", cmd)
}

func run(rawArgs []string) error {
	// Separate positional arguments (command + operands) from flags such as
	// --yes so subcommand operand indexing stays stable regardless of flag
	// placement.
	positional := make([]string, 0, len(rawArgs))
	for _, a := range rawArgs {
		if a == "--yes" || a == "-yes" {
			continue
		}
		positional = append(positional, a)
	}
	if len(positional) == 0 {
		return errors.New("expected a command")
	}
	args := positional

	// Enforce the confirmation gate before touching the database so an
	// accidental destructive invocation fails fast with a clear message.
	if destructive[args[0]] {
		if err := guard(args[0], rawArgs); err != nil {
			return err
		}
	}

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		return errors.New("DATABASE_URL is required")
	}

	path, err := resolveMigrationsPath()
	if err != nil {
		return err
	}
	slog.Info("running migrations", "source", path)

	m, err := migrate.New("file://"+filepath.ToSlash(path), dbURL)
	if err != nil {
		return fmt.Errorf("init migrate: %w", err)
	}
	defer func() {
		if srcErr, dbErr := m.Close(); srcErr != nil || dbErr != nil {
			slog.Warn("migrate close", "source_err", srcErr, "db_err", dbErr)
		}
	}()

	switch args[0] {
	case "up":
		return done(m.Up())
	case "down":
		return done(m.Steps(-1))
	case "down-all":
		return done(m.Down())
	case "goto":
		if len(args) < 2 {
			return errors.New("goto: expected version")
		}
		v, err := strconv.ParseUint(args[1], 10, 32)
		if err != nil {
			return fmt.Errorf("goto: invalid version: %w", err)
		}
		return done(m.Migrate(uint(v)))
	case "force":
		if len(args) < 2 {
			return errors.New("force: expected version")
		}
		v, err := strconv.Atoi(args[1])
		if err != nil {
			return fmt.Errorf("force: invalid version: %w", err)
		}
		return done(m.Force(v))
	case "version":
		v, dirty, err := m.Version()
		if err != nil {
			if errors.Is(err, migrate.ErrNilVersion) {
				slog.Info("no migrations applied")
				return nil
			}
			return err
		}
		slog.Info("current version", "version", v, "dirty", dirty)
		return nil
	default:
		return fmt.Errorf("unknown command: %s", args[0])
	}
}

// done turns a no-op (ErrNoChange) into success, propagating real errors.
func done(err error) error {
	if err == nil || errors.Is(err, migrate.ErrNoChange) {
		return nil
	}
	return err
}

// resolveMigrationsPath picks the first directory that exists from
// MIGRATIONS_PATH, the repo-relative default, or a local fallback.
func resolveMigrationsPath() (string, error) {
	candidates := []string{}
	if p := os.Getenv("MIGRATIONS_PATH"); p != "" {
		candidates = append(candidates, p)
	}
	candidates = append(candidates,
		"services/api/migrations",
		"./migrations",
	)

	for _, c := range candidates {
		abs, err := filepath.Abs(c)
		if err != nil {
			continue
		}
		if info, err := os.Stat(abs); err == nil && info.IsDir() {
			return abs, nil
		}
	}
	return "", fmt.Errorf("migrations directory not found; checked: %v", candidates)
}
