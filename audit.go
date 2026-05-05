package main

import (
	"encoding/json"
	"fmt"

	hiero "github.com/hiero-ledger/hiero-sdk-go/v2/sdk"
)

// auditMessageVersion identifies the schema of the JSON written to the audit
// topic. Bump it if the shape ever changes — readers can branch on the version.
const auditMessageVersion = 3

// maxAuditMessageBytes is the effective HCS topic-message limit. Audit
// payloads bigger than this can't be submitted; we surface that as
// INVALID_INPUT before signing rather than as a post-transfer audit failure.
const maxAuditMessageBytes = 1024

// AuditMessage is the JSON payload appended to the HCS audit topic for every
// successful payment.
type AuditMessage struct {
	Version       int    `json:"v"`
	TransactionID string `json:"txId"`
	From          string `json:"from"`
	To            string `json:"to"`
	Asset         string `json:"asset"`             // e.g. "USDC", "HBAR"
	TokenID       string `json:"tokenId,omitempty"` // omitted for HBAR
	Amount        string `json:"amount"`            // canonical decimal text, e.g. "1.5"
	Memo          string `json:"memo,omitempty"`
	Timestamp     string `json:"timestamp"` // RFC3339
}

// AuditResult is the output of a successful audit submission, surfaced into
// the CLI's stdout JSON so callers (humans and LLMs) can verify the entry.
type AuditResult struct {
	TopicID        string `json:"topicId"`
	TransactionID  string `json:"transactionId"`
	SequenceNumber uint64 `json:"sequenceNumber"`
}

// auditMessageSizeUpperBound returns a serialized-size upper bound for an
// audit message that hasn't been built yet, using worst-case placeholders
// for the fields that aren't known until after signing (transaction ID and
// timestamps). Used to reject pre-sign requests that would produce an
// oversized HCS message.
func auditMessageSizeUpperBound(asset, tokenID, memo string) int {
	worst := AuditMessage{
		Version:       auditMessageVersion,
		TransactionID: "0.0.99999999999@99999999999.999999999",
		From:          "0.0.99999999999",
		To:            "0.0.99999999999",
		Asset:         asset,
		TokenID:       tokenID,
		Amount:        "999999999999999999.999999999999999999",
		Memo:          memo,
		Timestamp:     "9999-12-31T23:59:59Z",
	}
	payload, err := json.Marshal(worst)
	if err != nil {
		// Marshalling a struct with only string/int fields cannot fail in
		// practice; if it ever does, treat the size as "very large" so the
		// caller rejects the request rather than letting it through.
		return maxAuditMessageBytes + 1
	}
	return len(payload)
}

// writeAudit submits one JSON-encoded AuditMessage to the configured audit
// topic. Caller is responsible for ensuring cfg.auditTopicID is non-nil.
func writeAudit(client *hiero.Client, cfg *config, msg *AuditMessage) (*AuditResult, error) {
	payload, err := json.Marshal(msg)
	if err != nil {
		return nil, fmt.Errorf("marshal audit message: %w", err)
	}

	resp, err := hiero.NewTopicMessageSubmitTransaction().
		SetTopicID(*cfg.auditTopicID).
		SetMessage(payload).
		Execute(client)
	if err != nil {
		return nil, fmt.Errorf("submit topic message: %w", err)
	}

	receipt, err := resp.GetReceipt(client)
	if err != nil {
		return nil, fmt.Errorf("get audit receipt: %w", err)
	}

	return &AuditResult{
		TopicID:        cfg.auditTopicID.String(),
		TransactionID:  resp.TransactionID.String(),
		SequenceNumber: receipt.TopicSequenceNumber,
	}, nil
}
