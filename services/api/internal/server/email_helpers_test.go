package server

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/japharyroman/fuelgrid-os/internal/email"
	"github.com/japharyroman/fuelgrid-os/services/api/internal/config"
)

type captureEmailSender struct {
	messages []email.Message
	err      error
}

func (s *captureEmailSender) Send(_ context.Context, msg email.Message) error {
	s.messages = append(s.messages, msg)
	return s.err
}

func (s *captureEmailSender) Driver() string { return "test" }

func newEmailHelperTestServer(sender email.Sender) *Server {
	return &Server{
		cfg:    config.Config{AppBaseURL: "https://fuelgrid.example.test/"},
		email:  sender,
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
}

func TestSendPasswordResetEmailBuildsPublicResetLink(t *testing.T) {
	sender := &captureEmailSender{}
	server := newEmailHelperTestServer(sender)

	server.sendPasswordResetEmail(context.Background(), "operator@example.com", "token/with+symbols")

	if len(sender.messages) != 1 {
		t.Fatalf("sent messages = %d, want 1", len(sender.messages))
	}
	msg := sender.messages[0]
	if msg.To != "operator@example.com" {
		t.Fatalf("recipient = %q", msg.To)
	}
	if !strings.Contains(msg.Body, "https://fuelgrid.example.test/reset-password?token=token%2Fwith%2Bsymbols") {
		t.Fatalf("reset email does not contain an escaped public link: %q", msg.Body)
	}
}

func TestSendInviteEmailPrefillsTenantAndRecipient(t *testing.T) {
	sender := &captureEmailSender{}
	server := newEmailHelperTestServer(sender)

	server.sendInviteEmail(context.Background(), "team+one@example.com", "Team One", "itemba")

	if len(sender.messages) != 1 {
		t.Fatalf("sent messages = %d, want 1", len(sender.messages))
	}
	body := sender.messages[0].Body
	if !strings.Contains(body, "https://fuelgrid.example.test/forgot-password?tenant=itemba&email=team%2Bone%40example.com") {
		t.Fatalf("invite email does not contain the prefilled reset link: %q", body)
	}
}

func TestEmailHelperDeliveryFailureDoesNotPanic(t *testing.T) {
	sender := &captureEmailSender{err: errors.New("provider unavailable")}
	server := newEmailHelperTestServer(sender)

	server.sendPasswordResetEmail(context.Background(), "operator@example.com", "token")
	server.sendInviteEmail(context.Background(), "operator@example.com", "Operator", "itemba")

	if len(sender.messages) != 2 {
		t.Fatalf("send attempts = %d, want 2", len(sender.messages))
	}
}
