// Package identity is the domain layer for authentication and authorization:
// users, sessions, passwords, MFA, and the Actor that flows through every
// request via context.
//
// HTTP-specific code (handlers, middleware) lives under services/api;
// this package is transport-agnostic so the same logic can power gRPC,
// background jobs, and future CLIs.
package identity

import (
	"context"
	"errors"

	"github.com/google/uuid"
)

// Actor is the authenticated principal for a request. It is set by the
// auth middleware and consumed by handlers and the policy evaluator.
//
// Zero value means "anonymous". Callers must check IsAuthenticated()
// before relying on UserID / TenantID.
type Actor struct {
	UserID    uuid.UUID
	TenantID  uuid.UUID
	SessionID uuid.UUID
	Email     string
	// MfaSatisfied reports whether MFA was completed for this session.
	// Some sensitive actions can be gated on this.
	MfaSatisfied bool
}

// IsAuthenticated returns true when an actor has been attached to the context.
func (a Actor) IsAuthenticated() bool { return a.UserID != uuid.Nil }

// ErrUnauthenticated is returned when a handler requires an actor but
// the request context does not carry one.
var ErrUnauthenticated = errors.New("identity: request is not authenticated")

type actorKey struct{}

// WithActor returns a child context carrying the actor.
func WithActor(ctx context.Context, a Actor) context.Context {
	return context.WithValue(ctx, actorKey{}, a)
}

// ActorFrom returns the actor on the context, or the zero Actor if none.
// Use Require if you need the missing-actor case to be an error.
func ActorFrom(ctx context.Context) Actor {
	a, _ := ctx.Value(actorKey{}).(Actor)
	return a
}

// Require returns the actor on the context or ErrUnauthenticated when
// nothing is attached. Used by handlers and services that need an actor.
func Require(ctx context.Context) (Actor, error) {
	a := ActorFrom(ctx)
	if !a.IsAuthenticated() {
		return Actor{}, ErrUnauthenticated
	}
	return a, nil
}
