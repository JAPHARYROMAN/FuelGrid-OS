package mpesa

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// CallbackPayload is the shape Daraja POSTs to the STK callback URL once the
// customer accepts or rejects the prompt. The interesting fields are nested
// under Body.stkCallback; CallbackMetadata.Item is a heterogeneous list of
// {Name, Value} pairs that is only present on success (ResultCode == 0).
//
//	{
//	  "Body": { "stkCallback": {
//	     "MerchantRequestID": "...",
//	     "CheckoutRequestID": "...",
//	     "ResultCode": 0,
//	     "ResultDesc": "The service request is processed successfully.",
//	     "CallbackMetadata": { "Item": [
//	        {"Name":"Amount","Value":150.00},
//	        {"Name":"MpesaReceiptNumber","Value":"NLJ7RT61SV"},
//	        {"Name":"PhoneNumber","Value":254708374149}
//	     ]}
//	  }}
//	}
type CallbackPayload struct {
	Body struct {
		STKCallback struct {
			MerchantRequestID string `json:"MerchantRequestID"`
			CheckoutRequestID string `json:"CheckoutRequestID"`
			ResultCode        int    `json:"ResultCode"`
			ResultDesc        string `json:"ResultDesc"`
			CallbackMetadata  struct {
				Item []CallbackItem `json:"Item"`
			} `json:"CallbackMetadata"`
		} `json:"stkCallback"`
	} `json:"Body"`
}

// CallbackItem is one {Name, Value} metadata pair. Value is left as RawMessage
// because Daraja mixes types (number, string) across items.
type CallbackItem struct {
	Name  string          `json:"Name"`
	Value json.RawMessage `json:"Value"`
}

// CallbackResult is the flattened, type-safe view a handler persists: which
// request it belongs to, whether it succeeded, and (on success) the receipt,
// settled amount (as a decimal string), and payer phone.
type CallbackResult struct {
	MerchantRequestID string
	CheckoutRequestID string
	ResultCode        int
	ResultDesc        string
	Success           bool
	// The following are only populated when Success is true.
	MpesaReceipt string
	Amount       string // decimal string, e.g. "150.00"
	Phone        string // normalised 2547######## when present
}

// ParseCallback decodes and validates a Daraja STK callback body. It returns
// an error only when the envelope is structurally invalid (not when the
// payment merely failed — a non-zero ResultCode is a valid, fully-parsed
// CallbackResult with Success=false). This keeps the webhook handler simple:
// parse, persist, ack 200.
func ParseCallback(raw []byte) (*CallbackResult, error) {
	var p CallbackPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, fmt.Errorf("mpesa: decode callback: %w", err)
	}
	cb := p.Body.STKCallback
	if cb.CheckoutRequestID == "" {
		return nil, errors.New("mpesa: callback missing CheckoutRequestID")
	}

	res := &CallbackResult{
		MerchantRequestID: cb.MerchantRequestID,
		CheckoutRequestID: cb.CheckoutRequestID,
		ResultCode:        cb.ResultCode,
		ResultDesc:        cb.ResultDesc,
		Success:           cb.ResultCode == 0,
	}
	if !res.Success {
		return res, nil
	}

	for _, item := range cb.CallbackMetadata.Item {
		switch item.Name {
		case "MpesaReceiptNumber":
			res.MpesaReceipt = trimJSONString(item.Value)
		case "Amount":
			res.Amount = decimalFromJSON(item.Value)
		case "PhoneNumber":
			if ph, err := NormalizePhone(trimJSONNumberOrString(item.Value)); err == nil {
				res.Phone = ph
			} else {
				res.Phone = trimJSONNumberOrString(item.Value)
			}
		}
	}
	return res, nil
}

// trimJSONString unwraps a JSON string value ("NLJ7RT61SV") to its contents;
// if it is not a JSON string it returns the raw bytes trimmed.
func trimJSONString(raw json.RawMessage) string {
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	return strings.TrimSpace(string(raw))
}

// trimJSONNumberOrString renders a JSON number or string as plain text without
// scientific notation (Daraja sends PhoneNumber as a bare integer like
// 254708374149).
func trimJSONNumberOrString(raw json.RawMessage) string {
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	var n json.Number
	if err := json.Unmarshal(raw, &n); err == nil {
		return n.String()
	}
	return strings.TrimSpace(string(raw))
}

// decimalFromJSON renders a JSON number/string amount as a decimal string. We
// avoid float formatting: json.Number preserves the exact lexical form Daraja
// sent (e.g. "150" or "150.00"), which is exactly the decimal-string contract.
func decimalFromJSON(raw json.RawMessage) string {
	var n json.Number
	if err := json.Unmarshal(raw, &n); err == nil {
		return n.String()
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return strings.TrimSpace(s)
	}
	return strings.TrimSpace(string(raw))
}

// NormalizePhone coerces a Kenyan MSISDN into Daraja's 2547######## /
// 2541######## form. It accepts 07XXXXXXXX, 7XXXXXXXX, +2547XXXXXXXX, and the
// already-normalised 2547XXXXXXXX. An unrecognisable value yields an error.
func NormalizePhone(raw string) (string, error) {
	s := strings.TrimSpace(raw)
	s = strings.ReplaceAll(s, " ", "")
	s = strings.TrimPrefix(s, "+")
	if s == "" {
		return "", errors.New("mpesa: empty phone number")
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return "", fmt.Errorf("mpesa: phone %q has non-digit characters", raw)
		}
	}
	switch {
	case strings.HasPrefix(s, "254") && len(s) == 12:
		return s, nil
	case strings.HasPrefix(s, "0") && len(s) == 10:
		return "254" + s[1:], nil
	case len(s) == 9 && (strings.HasPrefix(s, "7") || strings.HasPrefix(s, "1")):
		return "254" + s, nil
	default:
		return "", fmt.Errorf("mpesa: unrecognised phone format %q", raw)
	}
}

// WholeShillings converts a decimal-string amount to the whole-shilling integer
// Daraja expects on the wire. It parses without float arithmetic on the money
// (it splits on the decimal point) and floors any fractional part, rejecting
// non-positive or malformed amounts.
func WholeShillings(amount string) (int64, error) {
	s := strings.TrimSpace(amount)
	if s == "" {
		return 0, errors.New("mpesa: empty amount")
	}
	intPart := s
	if dot := strings.IndexByte(s, '.'); dot >= 0 {
		intPart = s[:dot]
		frac := s[dot+1:]
		for _, r := range frac {
			if r < '0' || r > '9' {
				return 0, fmt.Errorf("mpesa: malformed amount %q", amount)
			}
		}
	}
	if intPart == "" {
		intPart = "0"
	}
	v, err := strconv.ParseInt(intPart, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("mpesa: malformed amount %q", amount)
	}
	if v <= 0 {
		return 0, fmt.Errorf("mpesa: amount must be a positive number of shillings, got %q", amount)
	}
	return v, nil
}
