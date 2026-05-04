package main

import (
	"encoding/json"
	"fmt"

	hiero "github.com/hiero-ledger/hiero-sdk-go/v2/sdk"
)

// auditMessageVersion identifies the schema of the JSON written to the audit
// topic. Bump it if the shape ever changes — readers can branch on the version.
const auditMessageVersion = 2

// AuditMessage is the JSON payload appended to the HCS audit topic for every
// successful payment.
type AuditMessage struct {
	Version       int    `json:"v"`
	TransactionID string `json:"txId"`
	From          string `json:"from"`
	To            string `json:"to"`
	TokenID       string `json:"tokenId"`
	Amount        string `json:"amount"` // canonical decimal text, e.g. "1.5"
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
