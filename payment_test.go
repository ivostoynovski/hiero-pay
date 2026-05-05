package main

import (
	"context"
	"errors"
	"testing"

	hiero "github.com/hiero-ledger/hiero-sdk-go/v2/sdk"
	"github.com/shopspring/decimal"
)

// fakeSigner records every Transfer it sees and returns canned outputs.
// Tests assert on the recorded calls so we verify what the orchestrator
// passed to the signer, not just that "the signer was called somehow."
type fakeSigner struct {
	calls        []Transfer
	cannedResult TxResult
	cannedErr    error
}

func (f *fakeSigner) Submit(_ context.Context, t Transfer) (TxResult, error) {
	f.calls = append(f.calls, t)
	return f.cannedResult, f.cannedErr
}

func mustAccountID(t *testing.T, s string) hiero.AccountID {
	t.Helper()
	id, err := hiero.AccountIDFromString(s)
	if err != nil {
		t.Fatalf("parse account id %q: %v", s, err)
	}
	return id
}

func mustTokenID(t *testing.T, s string) hiero.TokenID {
	t.Helper()
	id, err := hiero.TokenIDFromString(s)
	if err != nil {
		t.Fatalf("parse token id %q: %v", s, err)
	}
	return id
}

// newPayFixture returns a config, store, request, and Transfer that the
// tests pass to Pay() without needing a live Hedera client. auditTopicID
// is nil so Pay short-circuits the audit step and never touches Client.
func newPayFixture(t *testing.T) (*config, *fakePaymentStore, PaymentRequest, Transfer) {
	t.Helper()
	cfg := &config{
		operatorID:   mustAccountID(t, "0.0.111"),
		network:      "testnet",
		auditTopicID: nil,
		maxAmount:    decimal.NewFromInt(10_000),
	}
	req := PaymentRequest{
		RecipientAccountID: "0.0.333",
		Asset:              "USDC",
		Amount:             dec("1.5"),
		Memo:               "unit test",
	}
	transfer := Transfer{
		AssetKind:   AssetKindHTS,
		AssetSymbol: "USDC",
		TokenID:     mustTokenID(t, "0.0.222"),
		Decimals:    6,
		From:        cfg.operatorID,
		To:          mustAccountID(t, req.RecipientAccountID),
		RawUnits:    1_500_000,
		Memo:        req.Memo,
	}
	return cfg, &fakePaymentStore{}, req, transfer
}

// newHbarPayFixture is the HBAR-denominated counterpart of newPayFixture.
func newHbarPayFixture(t *testing.T) (*config, *fakePaymentStore, PaymentRequest, Transfer) {
	t.Helper()
	cfg := &config{
		operatorID:   mustAccountID(t, "0.0.111"),
		network:      "testnet",
		auditTopicID: nil,
		maxAmount:    decimal.NewFromInt(10_000),
	}
	req := PaymentRequest{
		RecipientAccountID: "0.0.333",
		Asset:              "HBAR",
		Amount:             dec("1.5"),
		Memo:               "unit test hbar",
	}
	transfer := Transfer{
		AssetKind:   AssetKindHBAR,
		AssetSymbol: "HBAR",
		Decimals:    8,
		From:        cfg.operatorID,
		To:          mustAccountID(t, req.RecipientAccountID),
		RawUnits:    150_000_000, // 1.5 HBAR in tinybars
		Memo:        req.Memo,
	}
	return cfg, &fakePaymentStore{}, req, transfer
}

func TestPay_HappyPath_PassesTransferToSignerAndRecordsRow(t *testing.T) {
	cfg, store, req, transfer := newPayFixture(t)

	signer := &fakeSigner{
		cannedResult: TxResult{TransactionID: "0.0.111@1700000000.0", Status: "SUCCESS"},
	}
	deps := Deps{Cfg: cfg, Signer: signer, Store: store}

	result, err := Pay(context.Background(), deps, req, transfer)
	if err != nil {
		t.Fatalf("Pay returned err=%v, want nil", err)
	}

	if result.TransactionID != "0.0.111@1700000000.0" {
		t.Errorf("Result.TransactionID = %q, want %q", result.TransactionID, "0.0.111@1700000000.0")
	}
	if result.Status != "SUCCESS" {
		t.Errorf("Result.Status = %q, want %q", result.Status, "SUCCESS")
	}
	if result.DBStatus != "SUCCESS" {
		t.Errorf("Result.DBStatus = %q, want %q", result.DBStatus, "SUCCESS")
	}
	if result.AuditStatus != "SKIPPED" {
		t.Errorf("Result.AuditStatus = %q, want %q", result.AuditStatus, "SKIPPED")
	}

	// Signer assertions.
	if len(signer.calls) != 1 {
		t.Fatalf("signer.calls = %d, want 1", len(signer.calls))
	}
	got := signer.calls[0]
	if got.AssetKind != transfer.AssetKind {
		t.Errorf("Submit got AssetKind=%v, want %v", got.AssetKind, transfer.AssetKind)
	}
	if got.TokenID != transfer.TokenID {
		t.Errorf("Submit got TokenID=%v, want %v", got.TokenID, transfer.TokenID)
	}
	if got.RawUnits != transfer.RawUnits {
		t.Errorf("Submit got RawUnits=%d, want %d", got.RawUnits, transfer.RawUnits)
	}

	// Store assertions: a row was recorded with the orchestrator-derived
	// fields (asset, decimals, raw units, audit_status=PENDING).
	if len(store.recordCalls) != 1 {
		t.Fatalf("store.recordCalls = %d, want 1", len(store.recordCalls))
	}
	row := store.recordCalls[0]
	if row.TxID != "0.0.111@1700000000.0" {
		t.Errorf("recorded TxID=%q, want %q", row.TxID, "0.0.111@1700000000.0")
	}
	if row.Asset != "USDC" {
		t.Errorf("recorded asset=%q, want %q", row.Asset, "USDC")
	}
	if row.Decimals != 6 {
		t.Errorf("recorded decimals=%d, want 6", row.Decimals)
	}
	if row.AmountRawUnits != 1_500_000 {
		t.Errorf("recorded raw units=%d, want 1500000", row.AmountRawUnits)
	}
	if row.AuditStatus != "PENDING" {
		t.Errorf("recorded audit_status=%q, want PENDING (audit hasn't run yet at Record time)", row.AuditStatus)
	}

	// UpdateAudit must have run once, finalizing the row to SKIPPED.
	if len(store.updateAuditCalls) != 1 {
		t.Fatalf("store.updateAuditCalls = %d, want 1", len(store.updateAuditCalls))
	}
	upd := store.updateAuditCalls[0]
	if upd.TxID != "0.0.111@1700000000.0" {
		t.Errorf("UpdateAudit TxID=%q, want %q", upd.TxID, "0.0.111@1700000000.0")
	}
	if upd.Outcome.Status != "SKIPPED" {
		t.Errorf("UpdateAudit status=%q, want SKIPPED", upd.Outcome.Status)
	}
}

func TestPay_HappyPath_HBAR_RecordsHBARRow(t *testing.T) {
	cfg, store, req, transfer := newHbarPayFixture(t)

	signer := &fakeSigner{
		cannedResult: TxResult{TransactionID: "0.0.111@1700000001.0", Status: "SUCCESS"},
	}
	deps := Deps{Cfg: cfg, Signer: signer, Store: store}

	result, err := Pay(context.Background(), deps, req, transfer)
	if err != nil {
		t.Fatalf("Pay returned err=%v, want nil", err)
	}
	if result.Status != "SUCCESS" {
		t.Errorf("Result.Status = %q, want SUCCESS", result.Status)
	}

	if len(signer.calls) != 1 || signer.calls[0].AssetKind != AssetKindHBAR {
		t.Errorf("signer call kind=%v, want HBAR", signer.calls[0].AssetKind)
	}
	if (signer.calls[0].TokenID != hiero.TokenID{}) {
		t.Errorf("HBAR signer.TokenID=%v, want zero", signer.calls[0].TokenID)
	}

	if len(store.recordCalls) != 1 {
		t.Fatalf("store.recordCalls = %d, want 1", len(store.recordCalls))
	}
	row := store.recordCalls[0]
	if row.Asset != "HBAR" {
		t.Errorf("recorded asset=%q, want HBAR", row.Asset)
	}
	if row.Decimals != 8 {
		t.Errorf("recorded decimals=%d, want 8 for HBAR", row.Decimals)
	}
	if row.TokenID != "" {
		t.Errorf("recorded token_id=%q, want empty for HBAR", row.TokenID)
	}
	if row.AmountRawUnits != 150_000_000 {
		t.Errorf("recorded raw units=%d (tinybars), want 150000000 for 1.5 HBAR", row.AmountRawUnits)
	}
}

func TestPay_SignerFailure_NoStoreCalls(t *testing.T) {
	cfg, store, req, transfer := newPayFixture(t)

	cannedErr := errors.New("network unreachable")
	signer := &fakeSigner{cannedErr: cannedErr}
	deps := Deps{Cfg: cfg, Signer: signer, Store: store}

	result, err := Pay(context.Background(), deps, req, transfer)
	if err == nil {
		t.Fatal("Pay returned nil error, want error")
	}
	if !errors.Is(err, cannedErr) {
		t.Errorf("Pay returned err=%v, want it to wrap %v", err, cannedErr)
	}
	if result != nil {
		t.Errorf("Pay returned result=%+v on signer failure, want nil", result)
	}

	// The DB is the canonical record of "what the operator paid"; if the
	// transfer never landed, no row should exist.
	if len(store.recordCalls) != 0 {
		t.Errorf("store.recordCalls = %d on signer failure, want 0", len(store.recordCalls))
	}
	if len(store.updateAuditCalls) != 0 {
		t.Errorf("store.updateAuditCalls = %d on signer failure, want 0", len(store.updateAuditCalls))
	}
}

func TestPay_StoreRecordFailure_KeepsTransferSuccess(t *testing.T) {
	cfg, _, req, transfer := newPayFixture(t)
	cannedRecordErr := errors.New("disk full")
	store := &fakePaymentStore{cannedRecordErr: cannedRecordErr}
	signer := &fakeSigner{
		cannedResult: TxResult{TransactionID: "0.0.111@1700000002.0", Status: "SUCCESS"},
	}
	deps := Deps{Cfg: cfg, Signer: signer, Store: store}

	result, err := Pay(context.Background(), deps, req, transfer)
	if err != nil {
		t.Fatalf("Pay returned err=%v on store-Record failure, want nil — payment integrity > log completeness", err)
	}

	// The two-assertion rule from .claude/agents/test-verifier.md: assert
	// BOTH that the transfer is reported SUCCESS AND that dbStatus=FAILED.
	// One assertion alone would let a regression pass that flipped the
	// invariant.
	if result.Status != "SUCCESS" {
		t.Errorf("Result.Status = %q, want SUCCESS — DB failure must NOT escalate the transfer to a failure", result.Status)
	}
	if result.DBStatus != "FAILED" {
		t.Errorf("Result.DBStatus = %q, want FAILED", result.DBStatus)
	}
	if result.DBError == "" {
		t.Errorf("Result.DBError is empty, want a message describing the disk-full failure")
	}
	// AuditStatus should be SKIPPED — there was no row to update against.
	if result.AuditStatus != "SKIPPED" {
		t.Errorf("Result.AuditStatus = %q, want SKIPPED when Record fails", result.AuditStatus)
	}
	// UpdateAudit must NOT have been attempted — the row was never written.
	if len(store.updateAuditCalls) != 0 {
		t.Errorf("store.updateAuditCalls = %d, want 0 (no row exists to update)", len(store.updateAuditCalls))
	}
}
