package server

import (
	"context"
	"fmt"
	"strings"

	"github.com/japharyroman/fuelgrid-os/internal/email"
)

// sendPasswordResetEmail delivers the one-time reset link. It is best-effort:
// when no Sender is wired it is a no-op, and any delivery failure is logged but
// never surfaced to the request (a failed email must not change the 202 the
// caller already gets, nor leak whether the address exists).
func (s *Server) sendPasswordResetEmail(ctx context.Context, to, token string) {
	if s.email == nil || to == "" {
		return
	}
	base := strings.TrimRight(s.cfg.AppBaseURL, "/")
	link := fmt.Sprintf("%s/reset-password?token=%s", base, token)
	body := strings.Join([]string{
		"We received a request to reset your FuelGrid OS password.",
		"",
		"Use the link below to choose a new password. It expires shortly and can be used once:",
		"",
		link,
		"",
		"If you did not request this, you can safely ignore this email — your password will not change.",
	}, "\n")

	if err := s.email.Send(ctx, email.Message{
		To:      to,
		Subject: "Reset your FuelGrid OS password",
		Body:    body,
	}); err != nil {
		s.logger.Warn("password reset email not delivered", "error", err)
	}
}

// sendInviteEmail welcomes a newly invited user and points them at the reset
// flow to set their first password (invited accounts have no password yet).
// Best-effort, exactly like the reset email.
func (s *Server) sendInviteEmail(ctx context.Context, to, fullName string) {
	if s.email == nil || to == "" {
		return
	}
	base := strings.TrimRight(s.cfg.AppBaseURL, "/")
	greeting := "Hello"
	if fullName != "" {
		greeting = "Hello " + fullName
	}
	body := strings.Join([]string{
		greeting + ",",
		"",
		"You have been invited to FuelGrid OS.",
		"",
		"To get started, set your password using the password-reset flow:",
		"",
		base + "/forgot-password",
		"",
		"Then sign in at " + base + "/login.",
	}, "\n")

	if err := s.email.Send(ctx, email.Message{
		To:      to,
		Subject: "You've been invited to FuelGrid OS",
		Body:    body,
	}); err != nil {
		s.logger.Warn("invite email not delivered", "error", err)
	}
}
