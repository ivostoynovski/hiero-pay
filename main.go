package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"strings"
	"time"

	hiero "github.com/hiero-ledger/hiero-sdk-go/v2/sdk"
	"github.com/shopspring/decimal"
)

// usdcDecimals is the standard precision for Circle USDC. Hardcoded for v1;
// could be fetched from the token info at runtime later.
const usdcDecimals int32 = 6

// defaultMaxAmount is the safety cap applied when MAX_PAYMENT_AMOUNT is unset.
var defaultMaxAmount = decimal.NewFromInt(10_000)

// Result is the JSON written to stdout on success.
type Result struct {
	TransactionID string       `json:"transactionId"`
	Status        string       `json:"status"`
	AuditStatus   string       `json:"auditStatus,omitempty"`   // SUCCESS | SKIPPED | FAILED
	AuditMessage  *AuditResult `json:"auditMessage,omitempty"`  // present when AuditStatus = SUCCESS
	AuditError    string       `json:"auditError,omitempty"`    // present when AuditStatus = FAILED
}

// ErrorOut is the JSON written to stderr on failure. Code is a stable machine-
// readable label; Error is the human-readable message.
type ErrorOut struct {
	Code  string `json:"code"`
	Error string `json:"error"`
}

func main() {
	filePath := flag.String("file", "", "JSON file with payment request (default: stdin)")
	flag.Parse()

	if err := run(*filePath); err != nil {
		os.Exit(1)
	}
}

func run(filePath string) error {
	raw, err := readInput(filePath)
	if err != nil {
		return fail("INVALID_INPUT", fmt.Errorf("read input: %w", err))
	}

	var req PaymentRequest
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		return fail("INVALID_INPUT", fmt.Errorf("decode JSON: %w", err))
	}
	if err := req.Validate(); err != nil {
		return fail("INVALID_INPUT", err)
	}

	cfg, err := loadConfig()
	if err != nil {
		return fail("CONFIG_MISSING", err)
	}

	if req.Amount.GreaterThan(cfg.maxAmount) {
		return fail("INVALID_INPUT", fmt.Errorf("amount %s exceeds MAX_PAYMENT_AMOUNT cap of %s", req.Amount.String(), cfg.maxAmount.String()))
	}

	rawUnits, err := toRawUnits(req.Amount, usdcDecimals)
	if err != nil {
		return fail("INVALID_INPUT", err)
	}

	client, err := buildClient(cfg)
	if err != nil {
		return fail("AUTH_ERROR", err)
	}
	defer func() { _ = client.Close() }()

	result, err := executeTransfer(client, cfg, &req, rawUnits)
	if err != nil {
		return fail("TRANSFER_FAILED", err)
	}

	// Audit logging — degrade gracefully. Payment integrity always trumps
	// audit completeness: if the audit submission fails, we still report the
	// transfer as SUCCESS but mark the audit as FAILED so the caller knows.
	if cfg.auditTopicID == nil {
		result.AuditStatus = "SKIPPED"
	} else {
		audit, auditErr := writeAudit(client, cfg, &AuditMessage{
			Version:       auditMessageVersion,
			TransactionID: result.TransactionID,
			From:          cfg.operatorID.String(),
			To:            req.RecipientAccountID,
			TokenID:       cfg.tokenID.String(),
			Amount:        req.Amount.String(),
			Memo:          req.Memo,
			Timestamp:     time.Now().UTC().Format(time.RFC3339),
		})
		if auditErr != nil {
			result.AuditStatus = "FAILED"
			result.AuditError = auditErr.Error()
		} else {
			result.AuditStatus = "SUCCESS"
			result.AuditMessage = audit
		}
	}

	encoded, _ := json.Marshal(result)
	fmt.Println(string(encoded))
	return nil
}

type config struct {
	operatorID   hiero.AccountID
	operatorKey  hiero.PrivateKey
	network      string
	tokenID      hiero.TokenID
	auditTopicID *hiero.TopicID  // nil when audit logging is disabled
	maxAmount    decimal.Decimal // upper cap on a single payment
}

func loadConfig() (*config, error) {
	opID := os.Getenv("OPERATOR_ID")
	opKey := os.Getenv("OPERATOR_KEY")
	network := os.Getenv("HEDERA_NETWORK")
	tokenID := os.Getenv("USDC_TOKEN_ID")

	missing := []string{}
	for k, v := range map[string]string{
		"OPERATOR_ID":    opID,
		"OPERATOR_KEY":   opKey,
		"HEDERA_NETWORK": network,
		"USDC_TOKEN_ID":  tokenID,
	} {
		if v == "" {
			missing = append(missing, k)
		}
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("missing required env vars: %v", missing)
	}

	accountID, err := hiero.AccountIDFromString(opID)
	if err != nil {
		return nil, fmt.Errorf("invalid OPERATOR_ID %q: %w", opID, err)
	}
	privKey, err := parseOperatorKey(opKey)
	if err != nil {
		return nil, fmt.Errorf("invalid OPERATOR_KEY: %w", err)
	}
	tID, err := hiero.TokenIDFromString(tokenID)
	if err != nil {
		return nil, fmt.Errorf("invalid USDC_TOKEN_ID %q: %w", tokenID, err)
	}

	// AUDIT_TOPIC_ID is optional — when empty, audit logging is skipped.
	var auditTopic *hiero.TopicID
	if raw := os.Getenv("AUDIT_TOPIC_ID"); raw != "" {
		parsed, err := hiero.TopicIDFromString(raw)
		if err != nil {
			return nil, fmt.Errorf("invalid AUDIT_TOPIC_ID %q: %w", raw, err)
		}
		auditTopic = &parsed
	}

	// MAX_PAYMENT_AMOUNT is optional — falls back to defaultMaxAmount.
	maxAmount := defaultMaxAmount
	if raw := os.Getenv("MAX_PAYMENT_AMOUNT"); raw != "" {
		parsed, err := decimal.NewFromString(raw)
		if err != nil {
			return nil, fmt.Errorf("invalid MAX_PAYMENT_AMOUNT %q: %w", raw, err)
		}
		if !parsed.IsPositive() {
			return nil, fmt.Errorf("MAX_PAYMENT_AMOUNT must be positive, got %s", raw)
		}
		maxAmount = parsed
	}

	return &config{
		operatorID:   accountID,
		operatorKey:  privKey,
		network:      network,
		tokenID:      tID,
		auditTopicID: auditTopic,
		maxAmount:    maxAmount,
	}, nil
}

func buildClient(cfg *config) (*hiero.Client, error) {
	client, err := hiero.ClientForName(cfg.network)
	if err != nil {
		return nil, fmt.Errorf("create client for network %q: %w", cfg.network, err)
	}
	client.SetOperator(cfg.operatorID, cfg.operatorKey)
	return client, nil
}

// toRawUnits converts a decimal token amount to the integer raw units used by
// the SDK. The conversion is exact: callers are expected to have already
// validated that amount has at most `decimals` fractional digits.
func toRawUnits(amount decimal.Decimal, decimals int32) (int64, error) {
	raw := amount.Shift(decimals)
	if !raw.IsInteger() {
		return 0, fmt.Errorf("amount %s exceeds %d-decimal precision", amount.String(), decimals)
	}
	if !raw.IsPositive() {
		return 0, fmt.Errorf("amount %s rounds to zero raw units at %d decimals", amount.String(), decimals)
	}
	if raw.GreaterThan(decimal.NewFromInt(math.MaxInt64)) {
		return 0, fmt.Errorf("amount %s overflows int64 raw units at %d decimals", amount.String(), decimals)
	}
	return raw.IntPart(), nil
}

func executeTransfer(client *hiero.Client, cfg *config, req *PaymentRequest, rawUnits int64) (*Result, error) {
	recipientID, err := hiero.AccountIDFromString(req.RecipientAccountID)
	if err != nil {
		return nil, fmt.Errorf("invalid recipientAccountId: %w", err)
	}

	tx := hiero.NewTransferTransaction().
		AddTokenTransfer(cfg.tokenID, cfg.operatorID, -rawUnits).
		AddTokenTransfer(cfg.tokenID, recipientID, rawUnits).
		SetTransactionMemo(req.Memo)

	resp, err := tx.Execute(client)
	if err != nil {
		return nil, fmt.Errorf("execute transfer: %w", err)
	}

	receipt, err := resp.GetReceipt(client)
	if err != nil {
		return nil, fmt.Errorf("get receipt: %w", err)
	}

	return &Result{
		TransactionID: resp.TransactionID.String(),
		Status:        receipt.Status.String(),
	}, nil
}

func readInput(filePath string) ([]byte, error) {
	if filePath == "" {
		return io.ReadAll(os.Stdin)
	}
	return os.ReadFile(filePath)
}

// parseOperatorKey accepts either an ECDSA secp256k1 or an ED25519 private key
// in hex (with or without `0x` prefix), DER, or PEM. Hedera's auto-detect parser
// defaults raw hex to ED25519, which silently mis-parses ECDSA keys — so we try
// ECDSA first.
func parseOperatorKey(s string) (hiero.PrivateKey, error) {
	s = strings.TrimPrefix(s, "0x")
	s = strings.TrimPrefix(s, "0X")
	if k, err := hiero.PrivateKeyFromStringECDSA(s); err == nil {
		return k, nil
	}
	if k, err := hiero.PrivateKeyFromStringEd25519(s); err == nil {
		return k, nil
	}
	return hiero.PrivateKeyFromString(s)
}

// fail writes a structured JSON error to stderr and returns the original error.
// The caller propagates the error so main() exits with code 1.
func fail(code string, err error) error {
	out := ErrorOut{Code: code, Error: err.Error()}
	encoded, _ := json.Marshal(out)
	fmt.Fprintln(os.Stderr, string(encoded))
	return err
}
