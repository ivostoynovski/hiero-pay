package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestRunHistory_PassesFiltersToStore(t *testing.T) {
	store := &fakePaymentStore{}
	var buf bytes.Buffer

	args := []string{
		"--asset", "USDC",
		"--recipient", "alice",
		"--since", "2026-05-01T00:00:00Z",
		"--until", "2026-05-06T00:00:00Z",
		"--status", "SUCCESS",
		"--limit", "7",
	}
	if err := runHistory(args, store, &buf); err != nil {
		t.Fatalf("runHistory: %v", err)
	}

	if len(store.queryCalls) != 1 {
		t.Fatalf("store.queryCalls = %d, want 1", len(store.queryCalls))
	}
	got := store.queryCalls[0]
	if got.Asset != "USDC" {
		t.Errorf("filter.Asset = %q, want USDC", got.Asset)
	}
	if got.Recipient != "alice" {
		t.Errorf("filter.Recipient = %q, want alice", got.Recipient)
	}
	wantSince, _ := time.Parse(time.RFC3339, "2026-05-01T00:00:00Z")
	if !got.Since.Equal(wantSince) {
		t.Errorf("filter.Since = %v, want %v", got.Since, wantSince)
	}
	wantUntil, _ := time.Parse(time.RFC3339, "2026-05-06T00:00:00Z")
	if !got.Until.Equal(wantUntil) {
		t.Errorf("filter.Until = %v, want %v", got.Until, wantUntil)
	}
	if got.Status != "SUCCESS" {
		t.Errorf("filter.Status = %q, want SUCCESS", got.Status)
	}
	if got.Limit != 7 {
		t.Errorf("filter.Limit = %d, want 7", got.Limit)
	}
}

func TestRunHistory_DefaultFormatIsJSON(t *testing.T) {
	store := &fakePaymentStore{
		cannedQueryRows: []PaymentRow{
			{
				TxID:           "0.0.111@1700000000.0",
				Status:         "SUCCESS",
				Network:        "testnet",
				FromAccount:    "0.0.111",
				ToAccount:      "0.0.222",
				ToName:         "alice",
				Asset:          "USDC",
				TokenID:        "0.0.429274",
				Decimals:       6,
				AmountDecimal:  "1.5",
				AmountRawUnits: 1_500_000,
				SubmittedAt:    "2026-05-05T12:00:00Z",
				AuditStatus:    "SUCCESS",
			},
		},
	}
	var buf bytes.Buffer
	if err := runHistory(nil, store, &buf); err != nil {
		t.Fatalf("runHistory: %v", err)
	}

	// Output should be JSON. Decode it back and assert structure.
	var got []PaymentRow
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("decode JSON output: %v\nraw: %s", err, buf.String())
	}
	if len(got) != 1 || got[0].TxID != "0.0.111@1700000000.0" {
		t.Errorf("decoded rows = %+v, want one with TxID 0.0.111@1700000000.0", got)
	}
	// Asset symbol must round-trip through json tags.
	if got[0].Asset != "USDC" {
		t.Errorf("decoded asset = %q, want USDC", got[0].Asset)
	}
}

func TestRunHistory_TableFormatHumanReadable(t *testing.T) {
	store := &fakePaymentStore{
		cannedQueryRows: []PaymentRow{
			{
				TxID:          "0.0.111@1700000000.0",
				Status:        "SUCCESS",
				Asset:         "USDC",
				ToAccount:     "0.0.222",
				ToName:        "alice",
				AmountDecimal: "1.5",
				SubmittedAt:   "2026-05-05T12:00:00Z",
				AuditStatus:   "SUCCESS",
			},
		},
	}
	var buf bytes.Buffer
	if err := runHistory([]string{"--format", "table"}, store, &buf); err != nil {
		t.Fatalf("runHistory: %v", err)
	}

	out := buf.String()
	// Header must be present.
	if !strings.Contains(out, "TIMESTAMP") || !strings.Contains(out, "RECIPIENT") || !strings.Contains(out, "AMOUNT") || !strings.Contains(out, "ASSET") {
		t.Errorf("table output missing header columns:\n%s", out)
	}
	// Recipient column should show "name (account)" when name is set.
	if !strings.Contains(out, "alice (0.0.222)") {
		t.Errorf("table output missing 'alice (0.0.222)' recipient cell:\n%s", out)
	}
	if !strings.Contains(out, "1.5") || !strings.Contains(out, "USDC") {
		t.Errorf("table output missing amount/asset cells:\n%s", out)
	}
}

func TestRunHistory_TableFormat_AccountIDOnly_WhenNoName(t *testing.T) {
	store := &fakePaymentStore{
		cannedQueryRows: []PaymentRow{
			{
				ToAccount:     "0.0.333",
				Asset:         "HBAR",
				AmountDecimal: "0.5",
				Status:        "SUCCESS",
				SubmittedAt:   "2026-05-05T12:00:00Z",
				AuditStatus:   "SUCCESS",
			},
		},
	}
	var buf bytes.Buffer
	if err := runHistory([]string{"--format", "table"}, store, &buf); err != nil {
		t.Fatalf("runHistory: %v", err)
	}
	out := buf.String()
	// When ToName is empty, the recipient column shows just the account ID
	// without parentheses around an empty name.
	if !strings.Contains(out, "0.0.333") {
		t.Errorf("expected account ID in output:\n%s", out)
	}
	if strings.Contains(out, "(") {
		t.Errorf("expected no parentheses when ToName is empty:\n%s", out)
	}
}

func TestRunHistory_InvalidFormat_Errors(t *testing.T) {
	store := &fakePaymentStore{}
	var buf bytes.Buffer
	err := runHistory([]string{"--format", "xml"}, store, &buf)
	if err == nil {
		t.Fatal("runHistory returned nil for --format=xml, want error")
	}
	if !strings.Contains(err.Error(), "format") {
		t.Errorf("error %q should mention the format flag", err.Error())
	}
}

func TestRunHistory_InvalidSinceTimestamp_Errors(t *testing.T) {
	store := &fakePaymentStore{}
	var buf bytes.Buffer
	err := runHistory([]string{"--since", "not-a-time"}, store, &buf)
	if err == nil {
		t.Fatal("runHistory returned nil for malformed --since, want error")
	}
}

func TestRunHistory_DefaultLimitIsApplied(t *testing.T) {
	store := &fakePaymentStore{}
	var buf bytes.Buffer
	if err := runHistory(nil, store, &buf); err != nil {
		t.Fatalf("runHistory: %v", err)
	}
	if len(store.queryCalls) != 1 {
		t.Fatalf("queryCalls = %d, want 1", len(store.queryCalls))
	}
	if store.queryCalls[0].Limit != defaultQueryLimit {
		t.Errorf("default Limit = %d, want %d", store.queryCalls[0].Limit, defaultQueryLimit)
	}
}

func TestRunHistory_EmptyResults_JSONIsEmptyArray(t *testing.T) {
	store := &fakePaymentStore{cannedQueryRows: nil}
	var buf bytes.Buffer
	if err := runHistory(nil, store, &buf); err != nil {
		t.Fatalf("runHistory: %v", err)
	}
	out := strings.TrimSpace(buf.String())
	// Must be a valid empty array, NOT JSON null. Consumers iterate
	// without a nil check.
	if out != "[]" {
		t.Errorf("empty-result JSON = %q, want %q", out, "[]")
	}
}
