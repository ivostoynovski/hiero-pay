package main

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/shopspring/decimal"
)

// PaymentRequest is the JSON schema accepted by hiero-pay on stdin or via --file.
// The token transferred is determined by the USDC_TOKEN_ID env var, not the request.
//
// Either Recipient (a contact-book name) or RecipientAccountID (a literal Hedera
// account ID) must be set — but not both. The contact name path is resolved
// against the local address book (see contacts.go); the account-ID path
// bypasses the book entirely.
//
// Amount must be a JSON string ("1.5"), not a number — string-only input keeps
// callers off the float-imprecision path before the value reaches us.
type PaymentRequest struct {
	Recipient          string          `json:"recipient,omitempty"`
	RecipientAccountID string          `json:"recipientAccountId,omitempty"`
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
	hasName := r.Recipient != ""
	hasID := r.RecipientAccountID != ""

	switch {
	case hasName && hasID:
		return fmt.Errorf("set exactly one of recipient or recipientAccountId, not both")
	case !hasName && !hasID:
		return fmt.Errorf("set one of recipient (contact name) or recipientAccountId (e.g. 0.0.5678)")
	}

	if hasID {
		if !accountIDPattern.MatchString(r.RecipientAccountID) {
			return fmt.Errorf("recipientAccountId %q is not a valid Hedera account ID (expected shard.realm.num, e.g. 0.0.5678)", r.RecipientAccountID)
		}
	} else {
		name := strings.TrimSpace(r.Recipient)
		if name == "" {
			return fmt.Errorf("recipient is empty after trimming whitespace")
		}
		if len(name) > maxContactNameLen {
			return fmt.Errorf("recipient %q exceeds %d-character limit", name, maxContactNameLen)
		}
		if !contactNamePattern.MatchString(name) {
			return fmt.Errorf("recipient %q has invalid characters (allowed: a-z, A-Z, 0-9, _, -)", name)
		}
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
