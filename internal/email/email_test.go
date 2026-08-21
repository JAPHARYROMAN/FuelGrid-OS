package email

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// New returns the console (no-op) driver when SMTP is unconfigured, so dev and
// CI never attempt a real send.
func TestNewFallsBackToConsoleWhenUnconfigured(t *testing.T) {
	t.Parallel()
	s := New(Config{}, nil)
	if got := s.Driver(); got != "console" {
		t.Fatalf("Driver() = %q, want console", got)
	}
	// Console send never errors.
	if err := s.Send(context.Background(), Message{To: "a@b.test", Subject: "hi"}); err != nil {
		t.Fatalf("console Send returned error: %v", err)
	}
}

// New returns the SMTP driver once a host is configured.
func TestNewUsesSMTPWhenHostSet(t *testing.T) {
	t.Parallel()
	s := New(Config{Host: "smtp.example.test", Port: 587}, nil)
	if got := s.Driver(); got != "smtp" {
		t.Fatalf("Driver() = %q, want smtp", got)
	}
}

func TestNewPrefersResendWhenConfigured(t *testing.T) {
	t.Parallel()
	s := New(Config{
		ResendAPIKey: "re_test",
		Host:         "smtp.example.test",
		Port:         587,
	}, nil)
	if got := s.Driver(); got != "resend" {
		t.Fatalf("Driver() = %q, want resend", got)
	}
}

func TestResendSenderDeliversMessageAndAttachment(t *testing.T) {
	t.Parallel()

	var received resendRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/emails" {
			t.Errorf("request = %s %s, want POST /emails", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer re_test" {
			t.Errorf("Authorization = %q", got)
		}
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Errorf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"email_123"}`))
	}))
	defer server.Close()

	sender := New(Config{
		ResendAPIKey:  "re_test",
		ResendBaseURL: server.URL,
		From:          "FuelGrid OS <no-reply@itembagrouptz.com>",
	}, nil)
	err := sender.Send(context.Background(), Message{
		To:      "attendant@gmail.com",
		Subject: "Your FuelGrid login",
		Body:    "Open the invitation link.",
		Attachments: []Attachment{{
			Filename: "schedule.csv",
			Data:     []byte("shift,date"),
		}},
	})
	if err != nil {
		t.Fatalf("Send returned error: %v", err)
	}
	if received.From != "FuelGrid OS <no-reply@itembagrouptz.com>" {
		t.Errorf("From = %q", received.From)
	}
	if len(received.To) != 1 || received.To[0] != "attendant@gmail.com" {
		t.Errorf("To = %#v", received.To)
	}
	if len(received.Attachments) != 1 {
		t.Fatalf("Attachments = %#v", received.Attachments)
	}
	if got, want := received.Attachments[0].Content, base64.StdEncoding.EncodeToString([]byte("shift,date")); got != want {
		t.Errorf("attachment content = %q, want %q", got, want)
	}
}

func TestResendSenderReturnsProviderError(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"message":"sender domain is not verified"}`, http.StatusUnprocessableEntity)
	}))
	defer server.Close()

	sender := New(Config{ResendAPIKey: "re_test", ResendBaseURL: server.URL}, nil)
	if err := sender.Send(context.Background(), Message{To: "attendant@gmail.com"}); err == nil {
		t.Fatal("Send returned nil error, want provider error")
	}
}
