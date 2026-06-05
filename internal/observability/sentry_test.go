package observability

import (
	"strings"
	"testing"

	"github.com/getsentry/sentry-go"
)

// TestScrubString covers SR-I2: the scrubber redacts the high-signal PII/secret
// shapes (email, bearer token, password/secret assignments, phone numbers) from
// free-form strings while leaving ordinary text intact.
func TestScrubString(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		mustNot []string // substrings that must NOT survive
		must    []string // substrings that MUST survive
	}{
		{
			name:    "email",
			in:      "login failed for alice@example.com",
			mustNot: []string{"alice@example.com"},
			must:    []string{"login failed", piiRedacted},
		},
		{
			name:    "bearer token",
			in:      "upstream call with Bearer abc123DEF456ghi789 returned 500",
			mustNot: []string{"abc123DEF456ghi789"},
			must:    []string{"returned 500", piiRedacted},
		},
		{
			name:    "password assignment",
			in:      `parsed body {"email":"x","password":"hunter2secret"}`,
			mustNot: []string{"hunter2secret"},
			must:    []string{piiRedacted},
		},
		{
			name:    "phone",
			in:      "sms to +254712345678 queued",
			mustNot: []string{"254712345678"},
			must:    []string{"sms to", "queued", piiRedacted},
		},
		{
			name: "no false positive on plain text",
			in:   "shift 7 closed with 3 nozzles at station MIK-01",
			must: []string{"shift 7 closed with 3 nozzles at station MIK-01"},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := scrubString(c.in)
			for _, m := range c.mustNot {
				if strings.Contains(got, m) {
					t.Fatalf("scrubString(%q) = %q still contains %q", c.in, got, m)
				}
			}
			for _, m := range c.must {
				if !strings.Contains(got, m) {
					t.Fatalf("scrubString(%q) = %q is missing expected %q", c.in, got, m)
				}
			}
		})
	}
}

// TestScrubEvent covers SR-I2 end-to-end on a sentry.Event: the message,
// exception value, request data, tags and attached breadcrumbs are all scrubbed,
// and the event is never dropped.
func TestScrubEvent(t *testing.T) {
	evt := &sentry.Event{
		Message: "auth error for bob@example.com",
		Exception: []sentry.Exception{
			{Value: "token Bearer s3cretToken1234567890 rejected"},
		},
		Request: &sentry.Request{
			Data:        `{"password":"p@ssw0rdValue"}`,
			QueryString: "email=carol@example.com",
		},
		Tags: map[string]string{"contact": "+254700111222"},
		Breadcrumbs: []*sentry.Breadcrumb{
			{Message: "sent reset to dave@example.com"},
		},
	}

	out := scrubEvent(evt, nil)
	if out == nil {
		t.Fatal("scrubEvent must never drop the event")
	}

	checks := []struct {
		field string
		value string
	}{
		{"message", out.Message},
		{"exception value", out.Exception[0].Value},
		{"request data", out.Request.Data},
		{"request query", out.Request.QueryString},
		{"tag", out.Tags["contact"]},
		{"breadcrumb", out.Breadcrumbs[0].Message},
	}
	leaks := []string{
		"bob@example.com", "s3cretToken1234567890", "p@ssw0rdValue",
		"carol@example.com", "254700111222", "dave@example.com",
	}
	for _, ch := range checks {
		for _, leak := range leaks {
			if strings.Contains(ch.value, leak) {
				t.Fatalf("%s still leaks %q: %q", ch.field, leak, ch.value)
			}
		}
		if !strings.Contains(ch.value, piiRedacted) {
			t.Fatalf("%s was not redacted: %q", ch.field, ch.value)
		}
	}
}

// TestScrubBreadcrumb covers the BeforeBreadcrumb hook directly: a breadcrumb's
// message and string data values are redacted.
func TestScrubBreadcrumb(t *testing.T) {
	b := &sentry.Breadcrumb{
		Message: "verifying code for erin@example.com",
		Data: map[string]any{
			"phone": "+254733000111",
			"count": 3, // non-string values are left untouched
		},
	}
	out := scrubBreadcrumb(b, nil)
	if strings.Contains(out.Message, "erin@example.com") {
		t.Fatalf("breadcrumb message still leaks email: %q", out.Message)
	}
	if pv, _ := out.Data["phone"].(string); strings.Contains(pv, "254733000111") {
		t.Fatalf("breadcrumb data still leaks phone: %q", pv)
	}
	if out.Data["count"] != 3 {
		t.Fatalf("non-string breadcrumb data must be untouched, got %v", out.Data["count"])
	}
}
