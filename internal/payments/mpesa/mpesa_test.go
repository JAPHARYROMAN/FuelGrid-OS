package mpesa

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestNormalizePhone(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{"0708374149", "254708374149", false},
		{"+254708374149", "254708374149", false},
		{"254708374149", "254708374149", false},
		{"708374149", "254708374149", false},
		{"  0712 345 678 ", "254712345678", false},
		{"0112345678", "254112345678", false}, // Safaricom 011x prefix
		{"", "", true},
		{"12345", "", true},
		{"07O8374149", "", true}, // letter O, not zero
	}
	for _, c := range cases {
		got, err := NormalizePhone(c.in)
		if c.wantErr {
			if err == nil {
				t.Errorf("NormalizePhone(%q): expected error, got %q", c.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("NormalizePhone(%q): unexpected error %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("NormalizePhone(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestWholeShillings(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in      string
		want    int64
		wantErr bool
	}{
		{"150", 150, false},
		{"150.00", 150, false},
		{"150.99", 150, false}, // floors fractional shillings
		{" 1 ", 1, false},
		{"0", 0, true},
		{"-5", 0, true},
		{"", 0, true},
		{"abc", 0, true},
		{"10.5x", 0, true},
	}
	for _, c := range cases {
		got, err := WholeShillings(c.in)
		if c.wantErr {
			if err == nil {
				t.Errorf("WholeShillings(%q): expected error, got %d", c.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("WholeShillings(%q): unexpected error %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("WholeShillings(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestParseCallbackSuccess(t *testing.T) {
	t.Parallel()
	raw := []byte(`{
	  "Body": {
	    "stkCallback": {
	      "MerchantRequestID": "29115-34620561-1",
	      "CheckoutRequestID": "ws_CO_191220191020363925",
	      "ResultCode": 0,
	      "ResultDesc": "The service request is processed successfully.",
	      "CallbackMetadata": {
	        "Item": [
	          {"Name": "Amount", "Value": 150.00},
	          {"Name": "MpesaReceiptNumber", "Value": "NLJ7RT61SV"},
	          {"Name": "TransactionDate", "Value": 20191219102115},
	          {"Name": "PhoneNumber", "Value": 254708374149}
	        ]
	      }
	    }
	  }
	}`)
	res, err := ParseCallback(raw)
	if err != nil {
		t.Fatalf("ParseCallback: %v", err)
	}
	if !res.Success {
		t.Fatalf("expected Success=true for ResultCode 0")
	}
	if res.CheckoutRequestID != "ws_CO_191220191020363925" {
		t.Errorf("CheckoutRequestID = %q", res.CheckoutRequestID)
	}
	if res.MerchantRequestID != "29115-34620561-1" {
		t.Errorf("MerchantRequestID = %q", res.MerchantRequestID)
	}
	if res.MpesaReceipt != "NLJ7RT61SV" {
		t.Errorf("MpesaReceipt = %q", res.MpesaReceipt)
	}
	if res.Amount != "150.00" {
		t.Errorf("Amount = %q, want decimal-string 150.00", res.Amount)
	}
	if res.Phone != "254708374149" {
		t.Errorf("Phone = %q", res.Phone)
	}
}

func TestParseCallbackFailure(t *testing.T) {
	t.Parallel()
	raw := []byte(`{
	  "Body": {
	    "stkCallback": {
	      "MerchantRequestID": "29115-34620561-1",
	      "CheckoutRequestID": "ws_CO_FAIL",
	      "ResultCode": 1032,
	      "ResultDesc": "Request cancelled by user"
	    }
	  }
	}`)
	res, err := ParseCallback(raw)
	if err != nil {
		t.Fatalf("ParseCallback: %v", err)
	}
	if res.Success {
		t.Fatalf("expected Success=false for non-zero ResultCode")
	}
	if res.ResultCode != 1032 {
		t.Errorf("ResultCode = %d, want 1032", res.ResultCode)
	}
	if res.MpesaReceipt != "" || res.Amount != "" {
		t.Errorf("failed callback should carry no receipt/amount, got %q/%q", res.MpesaReceipt, res.Amount)
	}
}

func TestParseCallbackInvalid(t *testing.T) {
	t.Parallel()
	if _, err := ParseCallback([]byte(`not json`)); err == nil {
		t.Error("expected error for malformed JSON")
	}
	if _, err := ParseCallback([]byte(`{"Body":{"stkCallback":{"ResultCode":0}}}`)); err == nil {
		t.Error("expected error for missing CheckoutRequestID")
	}
}

func TestDisabledClientIsNoOp(t *testing.T) {
	t.Parallel()
	c := New(Config{}, nil) // no credentials
	if c.Enabled() {
		t.Fatal("client with empty credentials must be disabled")
	}
	if _, err := c.STKPush(context.Background(), STKPushInput{Phone: "0708374149", Amount: "100"}); !errors.Is(err, ErrDisabled) {
		t.Errorf("disabled STKPush err = %v, want ErrDisabled", err)
	}
	if _, err := c.accessToken(context.Background()); !errors.Is(err, ErrDisabled) {
		t.Errorf("disabled token err = %v, want ErrDisabled", err)
	}
}

func TestEnabledMissingConfigYieldsErrConfig(t *testing.T) {
	t.Parallel()
	// Key+secret present (enabled) but shortcode/passkey/callback missing.
	c := New(Config{ConsumerKey: "k", ConsumerSecret: "s"}, nil)
	if !c.Enabled() {
		t.Fatal("client with credentials must be enabled")
	}
	if _, err := c.STKPush(context.Background(), STKPushInput{Phone: "0708374149", Amount: "100"}); !errors.Is(err, ErrConfig) {
		t.Errorf("STKPush err = %v, want ErrConfig", err)
	}
}

// TestSTKPushAgainstStub drives a full STK push against an httptest stub that
// emulates Daraja's token + processrequest endpoints — no live network.
func TestSTKPushAgainstStub(t *testing.T) {
	t.Parallel()

	var gotPath, gotAuth string
	var gotBody stkPushBody
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/oauth/v1/generate"):
			if !strings.HasPrefix(r.Header.Get("Authorization"), "Basic ") {
				t.Errorf("token request missing Basic auth: %q", r.Header.Get("Authorization"))
			}
			_ = json.NewEncoder(w).Encode(tokenResponse{AccessToken: "tok-123", ExpiresIn: "3599"})
		case strings.HasPrefix(r.URL.Path, "/mpesa/stkpush"):
			gotPath = r.URL.Path
			gotAuth = r.Header.Get("Authorization")
			b, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(b, &gotBody)
			_ = json.NewEncoder(w).Encode(STKPushResult{
				MerchantRequestID:   "mr-1",
				CheckoutRequestID:   "co-1",
				ResponseCode:        "0",
				ResponseDescription: "Success. Request accepted for processing",
				CustomerMessage:     "Enter your PIN",
			})
		default:
			http.Error(w, "unexpected path "+r.URL.Path, http.StatusNotFound)
		}
	}))
	defer srv.Close()

	c := New(Config{
		ConsumerKey:    "k",
		ConsumerSecret: "s",
		Shortcode:      "174379",
		Passkey:        "passkey",
		CallbackURL:    "https://example.test/cb",
	}, nil)
	c.baseForTest = srv.URL
	c.nowFunc = func() time.Time { return time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC) }

	res, err := c.STKPush(context.Background(), STKPushInput{
		Phone:            "0708374149",
		Amount:           "150.00",
		AccountReference: "FuelGridLongRef123",
		Description:      "Fuel",
	})
	if err != nil {
		t.Fatalf("STKPush: %v", err)
	}
	if res.CheckoutRequestID != "co-1" {
		t.Errorf("CheckoutRequestID = %q", res.CheckoutRequestID)
	}
	if gotPath != "/mpesa/stkpush/v1/processrequest" {
		t.Errorf("posted to %q", gotPath)
	}
	if gotAuth != "Bearer tok-123" {
		t.Errorf("Authorization = %q, want bearer token", gotAuth)
	}
	if gotBody.Amount != "150" {
		t.Errorf("wire Amount = %q, want whole shillings 150", gotBody.Amount)
	}
	if gotBody.PhoneNumber != "254708374149" {
		t.Errorf("wire PhoneNumber = %q", gotBody.PhoneNumber)
	}
	if gotBody.BusinessShortCode != "174379" {
		t.Errorf("wire BusinessShortCode = %q", gotBody.BusinessShortCode)
	}
	if len(gotBody.AccountReference) > 12 {
		t.Errorf("AccountReference %q exceeds 12 chars", gotBody.AccountReference)
	}
	if gotBody.Timestamp != "20240102030405" {
		t.Errorf("Timestamp = %q", gotBody.Timestamp)
	}
}

// TestTokenCaching verifies the token is fetched once and reused until near
// expiry.
func TestTokenCaching(t *testing.T) {
	t.Parallel()
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		_ = json.NewEncoder(w).Encode(tokenResponse{AccessToken: "tok", ExpiresIn: "3599"})
	}))
	defer srv.Close()

	c := New(Config{ConsumerKey: "k", ConsumerSecret: "s"}, nil)
	c.baseForTest = srv.URL
	now := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	c.nowFunc = func() time.Time { return now }

	for i := 0; i < 3; i++ {
		if _, err := c.accessToken(context.Background()); err != nil {
			t.Fatalf("token: %v", err)
		}
	}
	if hits != 1 {
		t.Errorf("token fetched %d times, want 1 (cached)", hits)
	}
	// Advance past expiry; a new fetch should occur.
	now = now.Add(2 * time.Hour)
	if _, err := c.accessToken(context.Background()); err != nil {
		t.Fatalf("token after expiry: %v", err)
	}
	if hits != 2 {
		t.Errorf("token fetched %d times after expiry, want 2", hits)
	}
}
