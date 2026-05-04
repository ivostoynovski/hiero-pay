package main

import (
	"encoding/json"
	"fmt"
	"regexp"

	"github.com/shopspring/decimal"
)

// PaymentRequest is the JSON schema accepted by hiero-pay on stdin or via --file.
// The token transferred is determined by the USDC_TOKEN_ID env var, not the request.
//
// Amount must be a JSON string ("1.5"), not a number — string-only input keeps
// callers off the float-imprecision path before the value reaches us.
//
// Example:
//
//	{
//	  "recipientAccountId": "0.0.5678",
//	  "amount": "1.5",
//	  "memo": "PR #1685"
//	}
type PaymentRequest struct {
	RecipientAccountID string          `json:"recipientAccountId"`
	Amount             decimal.Decimal `json:"amount"`
	Memo               string          `json:"memo,omitempty"`
}

// UnmarshalJSON enforces that `amount` arrives as a JSON string. Without this,
// callers could send `"amount": 0.1`, which their JSON encoder may have already
// rendered through a float (yielding 0.1000000000000000055…). Quoted strings
// bypass that risk on the caller's side.
func (r *PaymentRequest) UnmarshalJSON(data []byte) error {
	type alias PaymentRequest
	aux := struct {
		Amount json.RawMessage `json:"amount"`
		*alias
	}{alias: (*alias)(r)}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}

	if len(aux.Amount) == 0 || string(aux.Amount) == "null" {
		return fmt.Errorf("amount is required")
	}

	var s string
	if err := json.Unmarshal(aux.Amount, &s); err != nil {
		return fmt.Errorf("decode amount: %w", err)
	}
	parsed, err := decimal.NewFromString(s)
	if err != nil {
		return fmt.Errorf("amount %q is not a valid decimal: %w", s, err)
	}
	r.Amount = parsed
	return nil
}

// accountIDPattern matches Hedera shard.realm.num — non-negative integers only.
var accountIDPattern = regexp.MustCompile(`^\d+\.\d+\.\d+$`)

// maxMemoBytes is Hedera's per-transaction memo cap (bytes, not characters).
const maxMemoBytes = 100

// Validate returns the first error found in the request, or nil if valid.
// The cap check (vs MAX_PAYMENT_AMOUNT) is applied later, in run(), since it
// depends on env config.
func (r *PaymentRequest) Validate() error {
	if r.RecipientAccountID == "" {
		return fmt.Errorf("recipientAccountId is required")
	}
	if !accountIDPattern.MatchString(r.RecipientAccountID) {
		return fmt.Errorf("recipientAccountId %q is not a valid Hedera account ID (expected shard.realm.num, e.g. 0.0.5678)", r.RecipientAccountID)
	}
	if !r.Amount.IsPositive() {
		return fmt.Errorf("amount must be positive, got %s", r.Amount.String())
	}
	if -r.Amount.Exponent() > usdcDecimals {
		return fmt.Errorf("amount %s exceeds USDC's %d-decimal precision", r.Amount.String(), usdcDecimals)
	}
	if len(r.Memo) > maxMemoBytes {
		return fmt.Errorf("memo exceeds %d-byte Hedera transaction memo limit (got %d bytes)", maxMemoBytes, len(r.Memo))
	}
	return nil
}
