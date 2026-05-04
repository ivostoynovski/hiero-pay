package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/shopspring/decimal"
)

func dec(s string) decimal.Decimal {
	d, err := decimal.NewFromString(s)
	if err != nil {
		panic(err)
	}
	return d
}

func TestPaymentRequest_Validate(t *testing.T) {
	cases := []struct {
		name    string
		req     PaymentRequest
		wantErr bool
	}{
		{"valid minimal", PaymentRequest{RecipientAccountID: "0.0.5678", Amount: dec("1")}, false},
		{"valid with memo", PaymentRequest{RecipientAccountID: "0.0.5678", Amount: dec("1.5"), Memo: "PR #1685"}, false},
		{"valid memo at limit", PaymentRequest{RecipientAccountID: "0.0.5678", Amount: dec("1"), Memo: strings.Repeat("a", maxMemoBytes)}, false},
		{"valid 6 decimals", PaymentRequest{RecipientAccountID: "0.0.5678", Amount: dec("0.000001")}, false},

		{"missing recipient", PaymentRequest{Amount: dec("1")}, true},
		{"recipient single number", PaymentRequest{RecipientAccountID: "5678", Amount: dec("1")}, true},
		{"recipient alpha", PaymentRequest{RecipientAccountID: "0.0.abc", Amount: dec("1")}, true},
		{"recipient negative shard", PaymentRequest{RecipientAccountID: "-1.0.0", Amount: dec("1")}, true},
		{"recipient trailing space", PaymentRequest{RecipientAccountID: "0.0.5678 ", Amount: dec("1")}, true},
		{"zero amount", PaymentRequest{RecipientAccountID: "0.0.5678", Amount: dec("0")}, true},
		{"negative amount", PaymentRequest{RecipientAccountID: "0.0.5678", Amount: dec("-1")}, true},
		{"7 decimals rejected", PaymentRequest{RecipientAccountID: "0.0.5678", Amount: dec("0.0000001")}, true},
		{"memo over limit", PaymentRequest{RecipientAccountID: "0.0.5678", Amount: dec("1"), Memo: strings.Repeat("a", maxMemoBytes+1)}, true},
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

// TestPaymentRequest_UnmarshalExactDecimal verifies that values which would
// lose precision through float64 (e.g. 0.1) round-trip exactly when sent as
// JSON strings.
func TestPaymentRequest_UnmarshalExactDecimal(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"simple string", `{"recipientAccountId":"0.0.5","amount":"0.1"}`, "0.1"},
		{"string with trailing zeros normalizes", `{"recipientAccountId":"0.0.5","amount":"1.500000"}`, "1.5"},
		{"large integer", `{"recipientAccountId":"0.0.5","amount":"999999"}`, "999999"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var req PaymentRequest
			if err := json.Unmarshal([]byte(tc.in), &req); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if got := req.Amount.String(); got != tc.want {
				t.Fatalf("Amount = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestPaymentRequest_RejectsNonStringAmount verifies that non-string `amount`
// values are rejected at decode time, before any validation runs.
func TestPaymentRequest_RejectsNonStringAmount(t *testing.T) {
	cases := []struct {
		name string
		in   string
	}{
		{"number", `{"recipientAccountId":"0.0.5","amount":0.1}`},
		{"integer", `{"recipientAccountId":"0.0.5","amount":1}`},
		{"null", `{"recipientAccountId":"0.0.5","amount":null}`},
		{"missing", `{"recipientAccountId":"0.0.5"}`},
		{"bool", `{"recipientAccountId":"0.0.5","amount":true}`},
		{"unparseable string", `{"recipientAccountId":"0.0.5","amount":"abc"}`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var req PaymentRequest
			if err := json.Unmarshal([]byte(tc.in), &req); err == nil {
				t.Fatalf("expected error decoding %s, got nil", tc.in)
			}
		})
	}
}

func TestToRawUnits(t *testing.T) {
	cases := []struct {
		name    string
		amount  string
		want    int64
		wantErr bool
	}{
		{"one usdc", "1", 1_000_000, false},
		{"fractional", "1.5", 1_500_000, false},
		{"smallest unit", "0.000001", 1, false},
		{"sub-unit rounds to zero", "0.0000001", 0, true},
		{"zero", "0", 0, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := toRawUnits(dec(tc.amount), usdcDecimals)
			if (err != nil) != tc.wantErr {
				t.Fatalf("toRawUnits err=%v, wantErr=%v", err, tc.wantErr)
			}
			if !tc.wantErr && got != tc.want {
				t.Fatalf("toRawUnits = %d, want %d", got, tc.want)
			}
		})
	}
}
