package main

import (
	"strings"
	"testing"
)

func TestPaymentRequest_Validate(t *testing.T) {
	cases := []struct {
		name    string
		req     PaymentRequest
		wantErr bool
	}{
		{"valid minimal", PaymentRequest{RecipientAccountID: "0.0.5678", Amount: 1}, false},
		{"valid with memo", PaymentRequest{RecipientAccountID: "0.0.5678", Amount: 1.5, Memo: "PR #1685"}, false},
		{"valid memo at limit", PaymentRequest{RecipientAccountID: "0.0.5678", Amount: 1, Memo: strings.Repeat("a", maxMemoBytes)}, false},

		{"missing recipient", PaymentRequest{Amount: 1}, true},
		{"recipient single number", PaymentRequest{RecipientAccountID: "5678", Amount: 1}, true},
		{"recipient alpha", PaymentRequest{RecipientAccountID: "0.0.abc", Amount: 1}, true},
		{"recipient negative shard", PaymentRequest{RecipientAccountID: "-1.0.0", Amount: 1}, true},
		{"recipient trailing space", PaymentRequest{RecipientAccountID: "0.0.5678 ", Amount: 1}, true},
		{"zero amount", PaymentRequest{RecipientAccountID: "0.0.5678", Amount: 0}, true},
		{"negative amount", PaymentRequest{RecipientAccountID: "0.0.5678", Amount: -1}, true},
		{"memo over limit", PaymentRequest{RecipientAccountID: "0.0.5678", Amount: 1, Memo: strings.Repeat("a", maxMemoBytes+1)}, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.req.Validate()
			if (err != nil) != tc.wantErr {
				t.Fatalf("Validate() returned err=%v, wantErr=%v", err, tc.wantErr)
			}
		})
	}
}
