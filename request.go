package main

import (
	"fmt"
	"regexp"
)

// PaymentRequest is the JSON schema accepted by hiero-pay on stdin or via --file.
// The token transferred is determined by the USDC_TOKEN_ID env var, not the request.
//
// Example:
//
//	{
//	  "recipientAccountId": "0.0.5678",
//	  "amount": 1.5,
//	  "memo": "PR #1685"
//	}
type PaymentRequest struct {
	RecipientAccountID string  `json:"recipientAccountId"`
	Amount             float64 `json:"amount"`
	Memo               string  `json:"memo,omitempty"`
}

// accountIDPattern matches Hedera shard.realm.num — non-negative integers only.
var accountIDPattern = regexp.MustCompile(`^\d+\.\d+\.\d+$`)

// maxMemoBytes is Hedera's per-transaction memo cap (bytes, not characters).
const maxMemoBytes = 100

// Validate returns the first error found in the request, or nil if valid.
func (r *PaymentRequest) Validate() error {
	if r.RecipientAccountID == "" {
		return fmt.Errorf("recipientAccountId is required")
	}
	if !accountIDPattern.MatchString(r.RecipientAccountID) {
		return fmt.Errorf("recipientAccountId %q is not a valid Hedera account ID (expected shard.realm.num, e.g. 0.0.5678)", r.RecipientAccountID)
	}
	if r.Amount <= 0 {
		return fmt.Errorf("amount must be positive, got %v", r.Amount)
	}
	if len(r.Memo) > maxMemoBytes {
		return fmt.Errorf("memo exceeds %d-byte Hedera transaction memo limit (got %d bytes)", maxMemoBytes, len(r.Memo))
	}
	return nil
}
