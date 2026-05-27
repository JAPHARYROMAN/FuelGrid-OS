package policy

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/japharyroman/fuelgrid-os/internal/identity"
)

// fakeLoader returns a hand-built PermissionSet for tests. Lets us exercise
// Service.Can without touching Postgres.
type fakeLoader struct{ ps PermissionSet }

func (f fakeLoader) Load(_ context.Context, _ identity.Actor) (PermissionSet, error) {
	return f.ps, nil
}

func actor() identity.Actor {
	return identity.Actor{
		UserID:   uuid.MustParse("aaaaaaaa-0000-0000-0000-000000000001"),
		TenantID: uuid.MustParse("bbbbbbbb-0000-0000-0000-000000000001"),
	}
}

func TestUnauthenticatedActorRejected(t *testing.T) {
	t.Parallel()
	svc := NewService(fakeLoader{})
	err := svc.Can(context.Background(), identity.Actor{}, "station.read", Resource{})
	if !errors.Is(err, identity.ErrUnauthenticated) {
		t.Fatalf("got %v, want ErrUnauthenticated", err)
	}
}

func TestMissingPermissionForbidden(t *testing.T) {
	t.Parallel()
	svc := NewService(fakeLoader{ps: PermissionSet{
		Permissions:   map[string]bool{},
		StationScoped: map[string]bool{"station.read": true},
		TenantWide:    true,
	}})
	if err := svc.Can(context.Background(), actor(), "station.read", Resource{}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("got %v, want ErrForbidden", err)
	}
}

func TestTenantWidePermissionDoesNotRequireResource(t *testing.T) {
	t.Parallel()
	svc := NewService(fakeLoader{ps: PermissionSet{
		Permissions:   map[string]bool{"reports.export": true},
		StationScoped: map[string]bool{"reports.export": false},
	}})
	if err := svc.Can(context.Background(), actor(), "reports.export", Resource{}); err != nil {
		t.Fatalf("got %v, want nil", err)
	}
}

func TestStationScopedPermissionRequiresStation(t *testing.T) {
	t.Parallel()
	svc := NewService(fakeLoader{ps: PermissionSet{
		Permissions:   map[string]bool{"shift.close": true},
		StationScoped: map[string]bool{"shift.close": true},
		TenantWide:    true,
	}})
	// Tenant-wide actor missing station argument: still forbidden — the
	// caller must say what they're acting on, even when access is broad.
	if err := svc.Can(context.Background(), actor(), "shift.close", Resource{}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("got %v, want ErrForbidden", err)
	}
}

func TestTenantWideActorAccessesAnyStation(t *testing.T) {
	t.Parallel()
	station := uuid.New()
	svc := NewService(fakeLoader{ps: PermissionSet{
		Permissions:   map[string]bool{"station.read": true},
		StationScoped: map[string]bool{"station.read": true},
		TenantWide:    true,
	}})
	if err := svc.Can(context.Background(), actor(), "station.read", AtStation(station)); err != nil {
		t.Fatalf("tenant-wide actor denied: %v", err)
	}
}

func TestStationScopedActorAllowedAtAssignedStation(t *testing.T) {
	t.Parallel()
	assigned := uuid.New()
	other := uuid.New()

	svc := NewService(fakeLoader{ps: PermissionSet{
		Permissions:   map[string]bool{"shift.close": true},
		StationScoped: map[string]bool{"shift.close": true},
		StationIDs:    map[uuid.UUID]bool{assigned: true},
		TenantWide:    false,
	}})

	if err := svc.Can(context.Background(), actor(), "shift.close", AtStation(assigned)); err != nil {
		t.Fatalf("expected allow for assigned station: %v", err)
	}
	if err := svc.Can(context.Background(), actor(), "shift.close", AtStation(other)); !errors.Is(err, ErrForbidden) {
		t.Fatalf("expected forbidden for unassigned station, got %v", err)
	}
}

// Mirrors the realistic seed: attendant role has shift.open only.
func TestAttendantCannotReadStation(t *testing.T) {
	t.Parallel()
	station := uuid.New()
	svc := NewService(fakeLoader{ps: PermissionSet{
		Permissions:   map[string]bool{"shift.open": true},
		StationScoped: map[string]bool{"shift.open": true, "station.read": true},
		StationIDs:    map[uuid.UUID]bool{station: true},
	}})
	if err := svc.Can(context.Background(), actor(), "station.read", AtStation(station)); !errors.Is(err, ErrForbidden) {
		t.Fatalf("attendant should not read station: %v", err)
	}
}

// Mirrors the realistic seed: station_manager at their assigned station.
func TestStationManagerAtAssignedStationAllowed(t *testing.T) {
	t.Parallel()
	mine := uuid.New()
	other := uuid.New()

	managerPerms := []string{
		"station.read", "shift.open", "shift.close", "shift.approve",
		"reading.edit", "stock.adjust", "stock.approve_adjustment",
		"margin.view", "reports.export",
	}
	perms := map[string]bool{}
	scoped := map[string]bool{}
	for _, p := range managerPerms {
		perms[p] = true
		scoped[p] = p != "reports.export"
	}
	ps := PermissionSet{
		Permissions:   perms,
		StationScoped: scoped,
		StationIDs:    map[uuid.UUID]bool{mine: true},
	}
	svc := NewService(fakeLoader{ps: ps})

	if err := svc.Can(context.Background(), actor(), "station.read", AtStation(mine)); err != nil {
		t.Errorf("station_manager denied own station: %v", err)
	}
	if err := svc.Can(context.Background(), actor(), "station.read", AtStation(other)); !errors.Is(err, ErrForbidden) {
		t.Errorf("station_manager allowed other station: %v", err)
	}
	if err := svc.Can(context.Background(), actor(), "reports.export", Resource{}); err != nil {
		t.Errorf("station_manager denied tenant-wide reports.export: %v", err)
	}
}

func TestHasPermissionIgnoresScope(t *testing.T) {
	t.Parallel()
	ps := PermissionSet{
		Permissions:   map[string]bool{"station.read": true},
		StationScoped: map[string]bool{"station.read": true},
	}
	if !ps.HasPermission("station.read") {
		t.Fatal("HasPermission should be true")
	}
	if ps.HasPermission("nonexistent") {
		t.Fatal("HasPermission should be false for unknown code")
	}
}
