// Package email is the transactional-email boundary: a small Sender interface
// with an SMTP driver for production and a console/no-op driver used when SMTP
// is unconfigured so local development is a safe no-op.
//
// Callers (password reset, user invite, critical notifications) treat sending
// as BEST-EFFORT: a send failure is logged, never returned to the request, and
// must never block the user-facing operation.
package email

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"log/slog"
	"mime"
	"net"
	"net/smtp"
	"strconv"
	"strings"
	"time"
)

// Attachment is one file attached to a Message. ContentType defaults to
// application/octet-stream when blank. Used by the per-tenant Scheduled Reports
// email channel to deliver the rendered CSV/PDF/XLSX file (Reports Center
// Phase 12).
type Attachment struct {
	Filename    string
	ContentType string
	Data        []byte
}

// Message is one outbound email. Body is plain text; HTML is intentionally out
// of scope for this boundary (the templates are short transactional notices).
// Attachments, when present, are encoded as a MIME multipart/mixed message.
type Message struct {
	To          string
	Subject     string
	Body        string
	Attachments []Attachment
}

// Sender delivers transactional email. Implementations must be safe for
// concurrent use. Send returns an error so a caller that wants to log delivery
// failures can, but callers treat email as best-effort and never surface the
// error to the request.
type Sender interface {
	Send(ctx context.Context, msg Message) error
	// Driver names the active transport ("smtp" / "console") for startup logs.
	Driver() string
}

// Config carries the SMTP_* settings. When Host is empty the constructor
// returns the console driver, keeping dev a no-op.
type Config struct {
	Host     string
	Port     int
	Username string
	Password string
	From     string
}

// New returns the SMTP sender when Config.Host is set, otherwise a console
// (log-only) sender. This makes "SMTP unconfigured" the safe default: dev and
// CI never attempt a real send.
func New(cfg Config, logger *slog.Logger) Sender {
	if logger == nil {
		logger = slog.Default()
	}
	if strings.TrimSpace(cfg.Host) == "" {
		logger.Info("email: SMTP unconfigured — using console (no-op) sender")
		return &ConsoleSender{logger: logger}
	}
	from := cfg.From
	if from == "" {
		from = "no-reply@fuelgrid.local"
	}
	logger.Info("email: SMTP sender wired", "host", cfg.Host, "port", cfg.Port, "from", from)
	return &SMTPSender{cfg: cfg, from: from, logger: logger}
}

// ConsoleSender logs the message instead of sending it. Used in development and
// CI so no real mail leaves the box; the log line is enough to follow the flow.
type ConsoleSender struct{ logger *slog.Logger }

// Send logs the message. It never fails.
func (c *ConsoleSender) Send(_ context.Context, msg Message) error {
	c.logger.Info("email (console driver — not sent)",
		"to", msg.To, "subject", msg.Subject)
	return nil
}

// Driver returns "console".
func (c *ConsoleSender) Driver() string { return "console" }

// SMTPSender delivers over SMTP using the standard library. It dials per send
// (transactional volume is low) and applies a short timeout so a flaky relay
// can never wedge a caller.
type SMTPSender struct {
	cfg    Config
	from   string
	logger *slog.Logger
}

// Send delivers one message over SMTP. PLAIN auth is used only when a username
// is configured (so an open internal relay still works). A short dial timeout
// bounds the call.
func (s *SMTPSender) Send(ctx context.Context, msg Message) error {
	addr := net.JoinHostPort(s.cfg.Host, strconv.Itoa(s.cfg.Port))

	dialCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	var d net.Dialer
	conn, err := d.DialContext(dialCtx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("email: dial %s: %w", addr, err)
	}

	client, err := smtp.NewClient(conn, s.cfg.Host)
	if err != nil {
		_ = conn.Close()
		return fmt.Errorf("email: smtp client: %w", err)
	}
	defer func() { _ = client.Close() }()

	// STARTTLS opportunistically if the server offers it.
	if ok, _ := client.Extension("STARTTLS"); ok {
		if err := client.StartTLS(&tls.Config{ServerName: s.cfg.Host, MinVersion: tls.VersionTLS12}); err != nil {
			return fmt.Errorf("email: starttls: %w", err)
		}
	}

	if s.cfg.Username != "" {
		auth := smtp.PlainAuth("", s.cfg.Username, s.cfg.Password, s.cfg.Host)
		if err := client.Auth(auth); err != nil {
			return fmt.Errorf("email: auth: %w", err)
		}
	}

	if err := client.Mail(s.from); err != nil {
		return fmt.Errorf("email: MAIL FROM: %w", err)
	}
	if err := client.Rcpt(msg.To); err != nil {
		return fmt.Errorf("email: RCPT TO: %w", err)
	}
	wc, err := client.Data()
	if err != nil {
		return fmt.Errorf("email: DATA: %w", err)
	}
	if _, err := wc.Write(s.render(msg)); err != nil {
		_ = wc.Close()
		return fmt.Errorf("email: write body: %w", err)
	}
	if err := wc.Close(); err != nil {
		return fmt.Errorf("email: close body: %w", err)
	}
	return client.Quit()
}

// Driver returns "smtp".
func (s *SMTPSender) Driver() string { return "smtp" }

// render builds the RFC 5322 message bytes. With no attachments it is a plain-text
// message; with attachments it is a multipart/mixed message (a text/plain body
// part followed by a base64-encoded application part per attachment).
func (s *SMTPSender) render(msg Message) []byte {
	var b strings.Builder
	b.WriteString("From: " + s.from + "\r\n")
	// Strip CR/LF from the recipient before it goes into the header. Callers should
	// already validate addresses, but this is defense-in-depth against SMTP header
	// injection (a `To` value carrying `\r\nBcc: ...` would otherwise smuggle headers
	// into the message). Subject is neutralised separately by Q-encoding below.
	b.WriteString("To: " + sanitizeHeaderValue(msg.To) + "\r\n")
	b.WriteString("Subject: " + mime.QEncoding.Encode("utf-8", msg.Subject) + "\r\n")
	b.WriteString("MIME-Version: 1.0\r\n")

	if len(msg.Attachments) == 0 {
		b.WriteString("Content-Type: text/plain; charset=\"utf-8\"\r\n")
		b.WriteString("\r\n")
		b.WriteString(msg.Body)
		b.WriteString("\r\n")
		return []byte(b.String())
	}

	// A fixed, syntactically-valid boundary. The body parts never contain it (the
	// text body is operator-authored prose and the attachments are base64), so a
	// constant boundary is safe and keeps the message deterministic.
	const boundary = "----=_FuelGridReportBoundary_7f3b9c"
	b.WriteString("Content-Type: multipart/mixed; boundary=\"" + boundary + "\"\r\n")
	b.WriteString("\r\n")

	// Text body part.
	b.WriteString("--" + boundary + "\r\n")
	b.WriteString("Content-Type: text/plain; charset=\"utf-8\"\r\n")
	b.WriteString("\r\n")
	b.WriteString(msg.Body)
	b.WriteString("\r\n")

	for _, att := range msg.Attachments {
		ct := att.ContentType
		if ct == "" {
			ct = "application/octet-stream"
		}
		b.WriteString("--" + boundary + "\r\n")
		b.WriteString("Content-Type: " + ct + "\r\n")
		b.WriteString("Content-Transfer-Encoding: base64\r\n")
		b.WriteString("Content-Disposition: attachment; filename=\"" + sanitizeFilename(att.Filename) + "\"\r\n")
		b.WriteString("\r\n")
		b.WriteString(chunk76(base64.StdEncoding.EncodeToString(att.Data)))
		b.WriteString("\r\n")
	}

	b.WriteString("--" + boundary + "--\r\n")
	return []byte(b.String())
}

// sanitizeFilename strips characters that would break the Content-Disposition
// header (quotes / CR / LF), so a filename can never inject a header.
func sanitizeFilename(name string) string {
	r := strings.NewReplacer("\"", "", "\r", "", "\n", "")
	return r.Replace(name)
}

// sanitizeHeaderValue strips CR/LF so an attacker-influenced value (e.g. a
// recipient address) cannot inject additional message headers (header injection).
func sanitizeHeaderValue(v string) string {
	r := strings.NewReplacer("\r", "", "\n", "")
	return r.Replace(v)
}

// chunk76 inserts CRLF every 76 characters, as RFC 2045 requires for base64
// transfer encoding.
func chunk76(s string) string {
	const width = 76
	var b strings.Builder
	for len(s) > width {
		b.WriteString(s[:width])
		b.WriteString("\r\n")
		s = s[width:]
	}
	b.WriteString(s)
	return b.String()
}
