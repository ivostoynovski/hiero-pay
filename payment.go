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
	AssetKind   AssetKind
	AssetSymbol string        // canonical symbol, e.g. "USDC" or "HBAR"
	TokenID     hiero.TokenID // unset (zero value) for HBAR
	Decimals    int32         // snapshotted from the resolved asset
	From, To    hiero.AccountID
	ToName      string        // empty when the request used recipientAccountId
	RawUnits    int64
	Memo        string
}

// TxResult is what Signer reports back after a successful submission.
type TxResult struct {
	TransactionID string
	Status        string
}

// Deps is the dependency bundle Pay needs.
type Deps struct {
	Cfg    *config
	Signer Signer
	Store  PaymentStore
	Client *hiero.Client
}

// Pay submits the transfer via the signer, records a row to the payment
// store, then writes a best-effort audit message and updates the row's
// audit fields.
//
// Hard invariant: a Record / UpdateAudit / writeAudit failure must never
// escalate a successful transfer to a failure. The Result reports per-step
// outcomes (DBStatus, AuditStatus) alongside the transfer's SUCCESS so the
// caller knows what landed and what didn't.
func Pay(ctx context.Context, deps Deps, req PaymentRequest, t Transfer) (*Result, error) {
	txResult, err := deps.Signer.Submit(ctx, t)
	if err != nil {
		return nil, fmt.Errorf("execute transfer: %w", err)
	}

	result := &Result{
		TransactionID: txResult.TransactionID,
		Status:        txResult.Status,
	}

	// Record-then-audit-then-update sequence. Each step is fail-soft.
	row := PaymentRow{
		TxID:           result.TransactionID,
		SchemaVersion:  schemaVersion,
		Status:         result.Status,
		Network:        deps.Cfg.network,
		FromAccount:    deps.Cfg.operatorID.String(),
		ToAccount:      t.To.String(),
		ToName:         t.ToName,
		Asset:          t.AssetSymbol,
		Decimals:       int(t.Decimals),
		AmountDecimal:  req.Amount.String(),
		AmountRawUnits: t.RawUnits,
		Memo:           req.Memo,
		SubmittedAt:    time.Now().UTC().Format(time.RFC3339),
		AuditStatus:    "PENDING",
	}
	if t.AssetKind == AssetKindHTS {
		row.TokenID = t.TokenID.String()
	}

	if recordErr := deps.Store.Record(ctx, row); recordErr != nil {
		// Record failed before audit was attempted. The transfer itself
		// landed; surface that with dbStatus=FAILED so the operator can
		// reconcile manually.
		result.DBStatus = "FAILED"
		result.DBError = recordErr.Error()
		result.AuditStatus = "SKIPPED"
		return result, nil
	}
	result.DBStatus = "SUCCESS"

	if deps.Cfg.auditTopicID == nil {
		result.AuditStatus = "SKIPPED"
		_ = deps.Store.UpdateAudit(ctx, result.TransactionID, AuditOutcome{Status: "SKIPPED"})
		return result, nil
	}

	msg := &AuditMessage{
		Version:       auditMessageVersion,
		TransactionID: result.TransactionID,
		From:          deps.Cfg.operatorID.String(),
		To:            t.To.String(),
		Asset:         t.AssetSymbol,
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
		_ = deps.Store.UpdateAudit(ctx, result.TransactionID, AuditOutcome{
			Status:     "FAILED",
			ErrMessage: auditErr.Error(),
		})
		return result, nil
	}

	result.AuditStatus = "SUCCESS"
	result.AuditMessage = audit
	if updErr := deps.Store.UpdateAudit(ctx, result.TransactionID, AuditOutcome{
		Status:    "SUCCESS",
		TopicID:   audit.TopicID,
		SeqNumber: int64(audit.SequenceNumber),
	}); updErr != nil {
		// Audit succeeded but its outcome couldn't be persisted to the row;
		// the payment is still SUCCESS / audit is still SUCCESS, but the
		// row is stuck at PENDING. Surface so the operator knows to
		// reconcile.
		result.DBError = fmt.Sprintf("update audit on row: %v", updErr)
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
