package main

import (
	"context"
	"fmt"
	"time"

	hiero "github.com/hiero-ledger/hiero-sdk-go/v2/sdk"
)

// Signer submits a token transfer and returns the resulting transaction
// reference. It exists so unit tests can drive the orchestrator without a
// real Hedera client; HieroSigner below is the production adapter and is the
// only place the transfer-and-receipt SDK types are used.
type Signer interface {
	Submit(ctx context.Context, t Transfer) (TxResult, error)
}

type Transfer struct {
	AssetKind AssetKind       // HBAR or HTS
	TokenID   hiero.TokenID   // unset (zero value) for HBAR
	From, To  hiero.AccountID
	RawUnits  int64
	Memo      string
}

// TxResult is what Signer reports back after a successful submission.
type TxResult struct {
	TransactionID string
	Status        string
}

// Deps is the dependency bundle Pay needs. Future slices will add fields
// (PaymentStore in Slice 4); for now Signer is the only injected dependency,
// and the audit submission still uses the concrete client because there is
// no second audit-sink implementation to motivate an interface.
type Deps struct {
	Cfg    *config
	Signer Signer
	Client *hiero.Client
}

// Pay submits the transfer via the signer, then writes a best-effort audit
// message. A failure inside the audit step never escalates a successful
// transfer to a failure — auditStatus reports the outcome alongside the
// SUCCESS payment, preserving the "payment integrity > audit completeness"
// invariant.
func Pay(ctx context.Context, deps Deps, req PaymentRequest, t Transfer) (*Result, error) {
	txResult, err := deps.Signer.Submit(ctx, t)
	if err != nil {
		return nil, fmt.Errorf("execute transfer: %w", err)
	}

	result := &Result{
		TransactionID: txResult.TransactionID,
		Status:        txResult.Status,
	}

	if deps.Cfg.auditTopicID == nil {
		result.AuditStatus = "SKIPPED"
		return result, nil
	}

	msg := &AuditMessage{
		Version:       auditMessageVersion,
		TransactionID: result.TransactionID,
		From:          deps.Cfg.operatorID.String(),
		To:            t.To.String(),
		Asset:         req.Asset,
		Amount:        req.Amount.String(),
		Memo:          req.Memo,
		Timestamp:     time.Now().UTC().Format(time.RFC3339),
	}
	if t.AssetKind == AssetKindHTS {
		msg.TokenID = t.TokenID.String()
	}

	audit, auditErr := writeAudit(deps.Client, deps.Cfg, msg)
	if auditErr != nil {
		result.AuditStatus = "FAILED"
		result.AuditError = auditErr.Error()
	} else {
		result.AuditStatus = "SUCCESS"
		result.AuditMessage = audit
	}
	return result, nil
}

// HieroSigner is the production Signer adapter. It owns the SDK Client and
// is the only place hiero-sdk-go's TransferTransaction / TransactionReceipt
// types are referenced.
type HieroSigner struct {
	Client *hiero.Client
}

func (s *HieroSigner) Submit(_ context.Context, t Transfer) (TxResult, error) {
	tx := hiero.NewTransferTransaction().SetTransactionMemo(t.Memo)

	switch t.AssetKind {
	case AssetKindHBAR:
		tx = tx.
			AddHbarTransfer(t.From, hiero.HbarFromTinybar(-t.RawUnits)).
			AddHbarTransfer(t.To, hiero.HbarFromTinybar(t.RawUnits))
	case AssetKindHTS:
		tx = tx.
			AddTokenTransfer(t.TokenID, t.From, -t.RawUnits).
			AddTokenTransfer(t.TokenID, t.To, t.RawUnits)
	default:
		return TxResult{}, fmt.Errorf("unknown asset kind %q", t.AssetKind)
	}

	resp, err := tx.Execute(s.Client)
	if err != nil {
		return TxResult{}, err
	}

	receipt, err := resp.GetReceipt(s.Client)
	if err != nil {
		return TxResult{}, fmt.Errorf("get receipt: %w", err)
	}

	return TxResult{
		TransactionID: resp.TransactionID.String(),
		Status:        receipt.Status.String(),
	}, nil
}
