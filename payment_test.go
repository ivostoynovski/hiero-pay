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

// newPayFixture returns a config + request + transfer triple that the tests
// can pass to Pay() without needing a live Hedera client. auditTopicID is
// left nil so Pay short-circuits the audit step and never touches Client.
func newPayFixture(t *testing.T) (*config, PaymentRequest, Transfer) {
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
		AssetKind: AssetKindHTS,
		TokenID:   mustTokenID(t, "0.0.222"),
		From:      cfg.operatorID,
		To:        mustAccountID(t, req.RecipientAccountID),
		RawUnits:  1_500_000,
		Memo:      req.Memo,
	}
	return cfg, req, transfer
}

// newHbarPayFixture is the HBAR-denominated counterpart of newPayFixture.
// AssetKind is HBAR; TokenID is left zero. RawUnits represents tinybars.
func newHbarPayFixture(t *testing.T) (*config, PaymentRequest, Transfer) {
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
		AssetKind: AssetKindHBAR,
		From:      cfg.operatorID,
		To:        mustAccountID(t, req.RecipientAccountID),
		RawUnits:  150_000_000, // 1.5 HBAR in tinybars
		Memo:      req.Memo,
	}
	return cfg, req, transfer
}

func TestPay_HappyPath_PassesTransferToSigner(t *testing.T) {
	cfg, req, transfer := newPayFixture(t)

	signer := &fakeSigner{
		cannedResult: TxResult{TransactionID: "0.0.111@1700000000.0", Status: "SUCCESS"},
	}
	deps := Deps{Cfg: cfg, Signer: signer}

	result, err := Pay(context.Background(), deps, req, transfer)
	if err != nil {
		t.Fatalf("Pay returned err=%v, want nil", err)
	}

	// Verify the canned TxResult propagated to the caller-visible Result.
	if result.TransactionID != "0.0.111@1700000000.0" {
		t.Errorf("Result.TransactionID = %q, want %q", result.TransactionID, "0.0.111@1700000000.0")
	}
	if result.Status != "SUCCESS" {
		t.Errorf("Result.Status = %q, want %q", result.Status, "SUCCESS")
	}
	// auditTopicID is nil → audit must be reported SKIPPED.
	if result.AuditStatus != "SKIPPED" {
		t.Errorf("Result.AuditStatus = %q, want %q", result.AuditStatus, "SKIPPED")
	}
	if result.AuditMessage != nil {
		t.Errorf("Result.AuditMessage = %+v, want nil", result.AuditMessage)
	}

	// Verify the orchestrator passed the EXACT Transfer we constructed,
	// not some other shape it built internally. This is the assertion that
	// would catch a regression where Pay starts mutating the transfer.
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
	if got.From != transfer.From {
		t.Errorf("Submit got From=%v, want %v", got.From, transfer.From)
	}
	if got.To != transfer.To {
		t.Errorf("Submit got To=%v, want %v", got.To, transfer.To)
	}
	if got.RawUnits != transfer.RawUnits {
		t.Errorf("Submit got RawUnits=%d, want %d", got.RawUnits, transfer.RawUnits)
	}
	if got.Memo != transfer.Memo {
		t.Errorf("Submit got Memo=%q, want %q", got.Memo, transfer.Memo)
	}
}

func TestPay_HappyPath_HBAR_PassesTransferToSigner(t *testing.T) {
	cfg, req, transfer := newHbarPayFixture(t)

	signer := &fakeSigner{
		cannedResult: TxResult{TransactionID: "0.0.111@1700000001.0", Status: "SUCCESS"},
	}
	deps := Deps{Cfg: cfg, Signer: signer}

	result, err := Pay(context.Background(), deps, req, transfer)
	if err != nil {
		t.Fatalf("Pay returned err=%v, want nil", err)
	}
	if result.Status != "SUCCESS" {
		t.Errorf("Result.Status = %q, want %q", result.Status, "SUCCESS")
	}
	if result.AuditStatus != "SKIPPED" {
		t.Errorf("Result.AuditStatus = %q, want %q", result.AuditStatus, "SKIPPED")
	}

	if len(signer.calls) != 1 {
		t.Fatalf("signer.calls = %d, want 1", len(signer.calls))
	}
	got := signer.calls[0]
	if got.AssetKind != AssetKindHBAR {
		t.Errorf("Submit got AssetKind=%v, want HBAR", got.AssetKind)
	}
	// HBAR transfers must NOT carry a TokenID — leaving the signer-side
	// branch to use AddHbarTransfer instead of AddTokenTransfer.
	if (got.TokenID != hiero.TokenID{}) {
		t.Errorf("Submit got TokenID=%v, want zero value (HBAR)", got.TokenID)
	}
	if got.RawUnits != 150_000_000 {
		t.Errorf("Submit got RawUnits=%d (tinybars), want 150000000 for 1.5 HBAR", got.RawUnits)
	}
}

func TestPay_SignerFailure_ReturnsErrorAndNoResult(t *testing.T) {
	cfg, req, transfer := newPayFixture(t)

	cannedErr := errors.New("network unreachable")
	signer := &fakeSigner{cannedErr: cannedErr}
	deps := Deps{Cfg: cfg, Signer: signer}

	result, err := Pay(context.Background(), deps, req, transfer)
	if err == nil {
		t.Fatal("Pay returned nil error, want error")
	}
	// Pay must wrap the underlying signer error so errors.Is can identify the
	// cause; asserting only "some error" would be tautological.
	if !errors.Is(err, cannedErr) {
		t.Errorf("Pay returned err=%v, want it to wrap %v", err, cannedErr)
	}
	if result != nil {
		t.Errorf("Pay returned result=%+v on signer failure, want nil", result)
	}

	// The orchestrator must have actually invoked the signer once. If the
	// signer wasn't called, the test would still pass with a misclassified
	// error path — assert the side effect happened.
	if len(signer.calls) != 1 {
		t.Errorf("signer.calls = %d on failure path, want 1", len(signer.calls))
	}
}
