package email

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const defaultResendBaseURL = "https://api.resend.com"

// ResendSender delivers transactional email through Resend's HTTPS API. This
// transport works on infrastructure where outbound SMTP ports are blocked.
type ResendSender struct {
	apiKey  string
	baseURL string
	from    string
	client  *http.Client
}

type resendAttachment struct {
	Filename string `json:"filename"`
	Content  string `json:"content"`
}

type resendRequest struct {
	From        string             `json:"from"`
	To          []string           `json:"to"`
	Subject     string             `json:"subject"`
	Text        string             `json:"text"`
	Attachments []resendAttachment `json:"attachments,omitempty"`
}

func newResendSender(apiKey, baseURL, from string) *ResendSender {
	return &ResendSender{
		apiKey:  apiKey,
		baseURL: strings.TrimRight(baseURL, "/"),
		from:    from,
		client:  &http.Client{Timeout: 10 * time.Second},
	}
}

// Send delivers one message through the Resend API.
func (s *ResendSender) Send(ctx context.Context, msg Message) error {
	payload := resendRequest{
		From:    s.from,
		To:      []string{msg.To},
		Subject: msg.Subject,
		Text:    msg.Body,
	}
	for _, attachment := range msg.Attachments {
		payload.Attachments = append(payload.Attachments, resendAttachment{
			Filename: attachment.Filename,
			Content:  base64.StdEncoding.EncodeToString(attachment.Data),
		})
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("email: encode Resend request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.baseURL+"/emails", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("email: create Resend request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+s.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "FuelGrid-OS/1.0")

	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("email: Resend request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		responseBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("email: Resend returned %s: %s", resp.Status, strings.TrimSpace(string(responseBody)))
	}
	return nil
}

// Driver returns "resend".
func (s *ResendSender) Driver() string { return "resend" }
