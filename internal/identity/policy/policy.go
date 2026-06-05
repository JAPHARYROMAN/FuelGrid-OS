// Package policy is the authorization evaluator for FuelGrid OS.
//
// The package is split into two halves:
//
//   - PermissionSet is a pure value type. Can(...) is a deterministic
//     function with no IO — easy to test in isolation and easy to cache
//     across requests in a future revision.
//
//   - Loader is the IO boundary. The DB-backed implementation pulls a
//     user's permissions and station scope from Postgres; a fake Loader
//     drives the unit tests.
//
// Middlewares and handlers call Service.Can(ctx, actor, perm, resource)
// which loads the PermissionSet for the actor and asks it the question.
package policy

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/japharyroman/fuelgrid-os/internal/identity"
)

// ErrForbidden is returned when an actor is missing a permission or scope.
// Distinct from identity.ErrUnauthenticated which means "no actor at all".
var ErrForbidden = errors.New("policy: forbidden")

// Resource locates the thing being acted on. Today only StationID matters;
// future revisions will add CompanyID, CustomerID, etc.
type Resource struct {
	StationID *uuid.UUID
}

// AtStation is a small constructor for the common case.
func AtStation(id uuid.UUID) Resource { return Resource{StationID: &id} }

// PermissionSet is the snapshot of what a user can do. Permissions is the
// set of granted action codes. StationIDs is the explicit station scope;
// TenantWide reports whether the user has tenant-wide reach (no explicit
// station rows). For tenant-wide users, StationIDs is empty.
type PermissionSet struct {
	UserID        uuid.UUID
	TenantID      uuid.UUID
	Permissions   map[string]bool
	StationIDs    map[uuid.UUID]bool
	TenantWide    bool
	IsSystemAdmin bool
	StationScoped map[string]bool // which permission codes require a station
}

// HasPermission reports whether the actor has the permission, ignoring
// scope. Useful for UI hints. The authoritative check is Can().
func (ps PermissionSet) HasPermission(code string) bool {
	return ps.Permissions[code]
}

// Can returns nil if the actor may perform the action against the resource,
// or ErrForbidden otherwise.
//
// Rules:
//  1. Actor must hold the named permission.
//  2. If the permission is station_scoped:
//     - resource.StationID is required;
//     - tenant-wide actors are allowed unconditionally;
//     - otherwise the station must be in the actor's station scope.
//  3. If the permission is tenant-wide, no resource is required.
func (ps PermissionSet) Can(permission string, resource Resource) error {
	if !ps.Permissions[permission] {
		return ErrForbidden
	}

	if !ps.StationScoped[permission] {
		// Tenant-wide permission: scope doesn't matter.
		return nil
	}

	if resource.StationID == nil {
		// Permission is station-scoped but caller didn't supply a station.
		return ErrForbidden
	}

	if ps.TenantWide {
		return nil
	}

	if ps.StationIDs[*resource.StationID] {
		return nil
	}

	return ErrForbidden
}

// Loader fetches a PermissionSet for the supplied actor. Implementations
// may cache; the DBLoader does not yet — a Redis-backed cache lands in a
// later stage when traffic justifies it.
type Loader interface {
	Load(ctx context.Context, actor identity.Actor) (PermissionSet, error)
}

// Service is the high-level authorization API consumed by HTTP middleware
// and (eventually) gRPC interceptors and CLIs.
type Service struct {
	loader Loader
}

// NewService wires the policy service against a Loader.
func NewService(loader Loader) *Service {
	return &Service{loader: loader}
}

// Can loads the actor's PermissionSet and evaluates the request.
func (s *Service) Can(ctx context.Context, actor identity.Actor, permission string, resource Resource) error {
	if !actor.IsAuthenticated() {
		return identity.ErrUnauthenticated
	}
	ps, err := s.loader.Load(ctx, actor)
	if err != nil {
		return err
	}
	return ps.Can(permission, resource)
}

// LoadFor exposes the underlying loader. Used by /me/permissions to render
// a UI-friendly summary without paying for an authoritative Can() roundtrip.
func (s *Service) LoadFor(ctx context.Context, actor identity.Actor) (PermissionSet, error) {
	if !actor.IsAuthenticated() {
		return PermissionSet{}, identity.ErrUnauthenticated
	}
	return s.loader.Load(ctx, actor)
}
