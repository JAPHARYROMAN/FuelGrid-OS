// Package mpesa is the Safaricom Daraja (M-Pesa) client: OAuth token
// acquisition, STK Push (Lipa na M-Pesa Online), C2B simulation, and an
// optional B2C payout. It is deliberately ENV-GATED so the rest of the system
// can wire it unconditionally: when the consumer key/secret are unset the
// constructor returns a disabled no-op client whose calls fail with
// ErrDisabled instead of dialing Safaricom — exactly like the Sentry/SMTP
// boundaries, so dev and CI never hit the network without credentials.
//
// Money is carried as decimal STRINGS at this boundary (never float); Daraja's
// wire amounts are whole-shilling integers, so the client formats decimal
// strings down to the integer Daraja expects and never does float arithmetic
// on money.
package mpesa

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ErrDisabled is returned by every call on a no-op client (credentials unset).
// Callers treat it as "M-Pesa not configured" — a 503-style condition, never a
// 500 — so an un-provisioned deployment degrades cleanly.
var ErrDisabled = errors.New("mpesa: client disabled — set MPESA_CONSUMER_KEY/SECRET to enable")

// ErrConfig is returned when the client is enabled (key+secret present) but a
// field required for a specific call (shortcode, passkey, callback URL) is
// missing. Surfaced as a 503 so the operator knows to finish provisioning.
var ErrConfig = errors.New("mpesa: incomplete configuration for this operation")

// API host per environment.
const (
	sandboxBaseURL    = "https://sandbox.safaricom.co.ke"
	productionBaseURL = "https://api.safaricom.co.ke"

	// Daraja access tokens live ~3600s; refresh a little early to avoid using
	// one that expires mid-flight.
	tokenSkew = 60 * time.Second
)

// Config carries the MPESA_* settings. Key+Secret unset => disabled no-op.
type Config struct {
	ConsumerKey    string
	ConsumerSecret string
	Shortcode      string // business shortcode / paybill / till (BusinessShortCode)
	Passkey        string // Lipa na M-Pesa online passkey (for the STK password)
	Env            string // "sandbox" | "production"
	CallbackURL    string // public HTTPS URL Daraja POSTs results to
}

// Enabled reports whether the credentials needed to authenticate are present.
func (c Config) Enabled() bool {
	return strings.TrimSpace(c.ConsumerKey) != "" && strings.TrimSpace(c.ConsumerSecret) != ""
}

// Client is the Daraja boundary. Use New to construct it; the returned value is
// safe for concurrent use (the token cache is mutex-guarded).
type Client struct {
	cfg     Config
	baseURL string
	http    *http.Client
	logger  *slog.Logger

	// enabled is false for the no-op client; every call short-circuits to
	// ErrDisabled and never touches the network.
	enabled bool

	mu          sync.Mutex
	token       string
	tokenExp    time.Time
	nowFunc     func() time.Time // injectable clock for tests
	baseForTest string           // set by tests to point at a stub server
}

// New returns an enabled client when Config.Enabled() (key+secret present),
// otherwise a disabled no-op whose calls return ErrDisabled without dialing.
// This makes "M-Pesa unconfigured" the safe default for dev/CI.
func New(cfg Config, logger *slog.Logger) *Client {
	if logger == nil {
		logger = slog.Default()
	}
	base := sandboxBaseURL
	if strings.EqualFold(strings.TrimSpace(cfg.Env), "production") {
		base = productionBaseURL
	}
	c := &Client{
		cfg:     cfg,
		baseURL: base,
		logger:  logger,
		nowFunc: time.Now,
		http: &http.Client{
			Timeout: 20 * time.Second,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12},
			},
		},
	}
	if !cfg.Enabled() {
		logger.Info("mpesa: credentials unset — using disabled (no-op) client")
		return c
	}
	c.enabled = true
	logger.Info("mpesa: Daraja client wired", "env", base, "shortcode", cfg.Shortcode)
	return c
}

// Enabled reports whether this client will actually call Daraja.
func (c *Client) Enabled() bool { return c.enabled }

// base returns the effective API base (the test override wins).
func (c *Client) base() string {
	if c.baseForTest != "" {
		return c.baseForTest
	}
	return c.baseURL
}

func (c *Client) now() time.Time { return c.nowFunc() }

// ---------------------------------------------------------------------------
// OAuth token
// ---------------------------------------------------------------------------

type tokenResponse struct {
	AccessToken string `json:"access_token"`
	// Daraja returns expires_in as a string of seconds ("3599").
	ExpiresIn string `json:"expires_in"`
}

// accessToken returns a cached, non-expired access token, fetching a fresh one
// when the cache is empty or stale.
func (c *Client) accessToken(ctx context.Context) (string, error) {
	if !c.enabled {
		return "", ErrDisabled
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.token != "" && c.now().Before(c.tokenExp.Add(-tokenSkew)) {
		return c.token, nil
	}

	url := c.base() + "/oauth/v1/generate?grant_type=client_credentials"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("mpesa: build token request: %w", err)
	}
	cred := base64.StdEncoding.EncodeToString(
		[]byte(c.cfg.ConsumerKey + ":" + c.cfg.ConsumerSecret))
	req.Header.Set("Authorization", "Basic "+cred)

	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("mpesa: token request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("mpesa: token http %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var tr tokenResponse
	if err := json.Unmarshal(body, &tr); err != nil {
		return "", fmt.Errorf("mpesa: decode token: %w", err)
	}
	if tr.AccessToken == "" {
		return "", errors.New("mpesa: empty access token from Daraja")
	}
	ttl := 3599
	if secs, err := strconv.Atoi(strings.TrimSpace(tr.ExpiresIn)); err == nil && secs > 0 {
		ttl = secs
	}
	c.token = tr.AccessToken
	c.tokenExp = c.now().Add(time.Duration(ttl) * time.Second)
	return c.token, nil
}

// ---------------------------------------------------------------------------
// STK Push (Lipa na M-Pesa Online)
// ---------------------------------------------------------------------------

// STKPushInput is the caller-facing request for an STK push.
type STKPushInput struct {
	// Phone is the payer MSISDN; it is normalised to 2547######## form.
	Phone string
	// Amount is a decimal string (e.g. "150.00"); Daraja takes whole shillings,
	// so it is floored to an integer at the wire boundary.
	Amount string
	// AccountReference (<=12 chars on the wire) and Description label the prompt.
	AccountReference string
	Description      string
}

// STKPushResult is Daraja's synchronous acknowledgement. The terminal result
// (paid / failed) arrives later on the callback URL.
type STKPushResult struct {
	MerchantRequestID   string `json:"MerchantRequestID"`
	CheckoutRequestID   string `json:"CheckoutRequestID"`
	ResponseCode        string `json:"ResponseCode"`
	ResponseDescription string `json:"ResponseDescription"`
	CustomerMessage     string `json:"CustomerMessage"`
}

type stkPushBody struct {
	BusinessShortCode string `json:"BusinessShortCode"`
	Password          string `json:"Password"`
	Timestamp         string `json:"Timestamp"`
	TransactionType   string `json:"TransactionType"`
	Amount            string `json:"Amount"`
	PartyA            string `json:"PartyA"`
	PartyB            string `json:"PartyB"`
	PhoneNumber       string `json:"PhoneNumber"`
	CallBackURL       string `json:"CallBackURL"`
	AccountReference  string `json:"AccountReference"`
	TransactionDesc   string `json:"TransactionDesc"`
}

// STKPush initiates a customer payment prompt. Returns ErrDisabled on the no-op
// client and ErrConfig when the shortcode/passkey/callback are not provisioned.
func (c *Client) STKPush(ctx context.Context, in STKPushInput) (*STKPushResult, error) {
	if !c.enabled {
		return nil, ErrDisabled
	}
	if c.cfg.Shortcode == "" || c.cfg.Passkey == "" || c.cfg.CallbackURL == "" {
		return nil, ErrConfig
	}
	phone, err := NormalizePhone(in.Phone)
	if err != nil {
		return nil, err
	}
	amount, err := WholeShillings(in.Amount)
	if err != nil {
		return nil, err
	}
	ts := c.now().Format("20060102150405")
	password := base64.StdEncoding.EncodeToString(
		[]byte(c.cfg.Shortcode + c.cfg.Passkey + ts))

	acctRef := in.AccountReference
	if acctRef == "" {
		acctRef = "FuelGrid"
	}
	if len(acctRef) > 12 {
		acctRef = acctRef[:12]
	}
	desc := in.Description
	if desc == "" {
		desc = "Fuel payment"
	}

	body := stkPushBody{
		BusinessShortCode: c.cfg.Shortcode,
		Password:          password,
		Timestamp:         ts,
		TransactionType:   "CustomerPayBillOnline",
		Amount:            strconv.FormatInt(amount, 10),
		PartyA:            phone,
		PartyB:            c.cfg.Shortcode,
		PhoneNumber:       phone,
		CallBackURL:       c.cfg.CallbackURL,
		AccountReference:  acctRef,
		TransactionDesc:   desc,
	}

	var out STKPushResult
	if err := c.post(ctx, "/mpesa/stkpush/v1/processrequest", body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// post performs an authenticated JSON POST and decodes the response. A non-2xx
// status returns an error carrying the body so callers can log Daraja's reason.
func (c *Client) post(ctx context.Context, path string, reqBody, out any) error {
	tok, err := c.accessToken(ctx)
	if err != nil {
		return err
	}
	buf, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("mpesa: marshal request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base()+path, bytes.NewReader(buf))
	if err != nil {
		return fmt.Errorf("mpesa: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("mpesa: %s: %w", path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("mpesa: %s http %d: %s", path, resp.StatusCode, strings.TrimSpace(string(respBody)))
	}
	if out != nil {
		if err := json.Unmarshal(respBody, out); err != nil {
			return fmt.Errorf("mpesa: decode %s response: %w", path, err)
		}
	}
	return nil
}
