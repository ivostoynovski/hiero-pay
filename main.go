package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"strings"

	hiero "github.com/hiero-ledger/hiero-sdk-go/v2/sdk"
	"github.com/shopspring/decimal"
)

// version is the binary's reported identifier. The Claude Code skill calls
// --version before constructing a payment so a stale binary surfaces
// loudly rather than via a cryptic INVALID_INPUT decode error.
const version = "v1.6.0"

const usdcDecimals int32 = 6

// defaultMaxAmount is the safety cap applied when MAX_PAYMENT_AMOUNT is unset.
var defaultMaxAmount = decimal.NewFromInt(10_000)

// Result is the JSON written to stdout on success.
type Result struct {
	TransactionID string       `json:"transactionId"`
	Status        string       `json:"status"`
	DBStatus      string       `json:"dbStatus,omitempty"`     // SUCCESS | SKIPPED | FAILED
	DBError       string       `json:"dbError,omitempty"`      // present when DBStatus = FAILED
	AuditStatus   string       `json:"auditStatus,omitempty"`  // SUCCESS | SKIPPED | FAILED
	AuditMessage  *AuditResult `json:"auditMessage,omitempty"` // present when AuditStatus = SUCCESS
	AuditError    string       `json:"auditError,omitempty"`   // present when AuditStatus = FAILED
}

// ErrorOut is the JSON written to stderr on failure. Code is a stable machine-
// readable label; Error is the human-readable message.
type ErrorOut struct {
	Code  string `json:"code"`
	Error string `json:"error"`
}

func main() {
	flag.Usage = func() {
		out := flag.CommandLine.Output()
		_, _ = fmt.Fprint(out, `Usage:
  hiero-pay [--file PATH]      Submit a payment from JSON (stdin if --file omitted)
  hiero-pay history [flags]    Query the local payment history

Pay flags:
`)
		flag.PrintDefaults()
		_, _ = fmt.Fprintln(out, "\nRun 'hiero-pay history --help' to see history flags.")
	}

	if len(os.Args) > 1 && os.Args[1] == "history" {
		store, err := OpenSQLitePaymentStore("")
		if err != nil {
			_ = fail("CONFIG_MISSING", err)
			os.Exit(1)
		}
		defer func() { _ = store.Close() }()
		if err := runHistory(os.Args[2:], store, os.Stdout); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}

	versionFlag := flag.Bool("version", false, "print version and exit")
	filePath := flag.String("file", "", "JSON file with payment request (default: stdin)")
	flag.Parse()

	if *versionFlag {
		fmt.Println(version)
		return
	}

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

	// Default missing asset to USDC for back-compat with pre-Slice-3 requests.
	if req.Asset == "" {
		req.Asset = "USDC"
	}

	registry, err := LoadTokenRegistry()
	if err != nil {
		return fail("CONFIG_MISSING", err)
	}
	asset, err := registry.Lookup(req.Asset)
	if err != nil {
		return fail("INVALID_INPUT", err)
	}

	rawUnits, err := toRawUnits(req.Amount, asset.Decimals)
	if err != nil {
		return fail("INVALID_INPUT", err)
	}

	// Resolve the recipient: either the literal account ID from the request
	// or, when only a name was provided, a lookup against the local contacts
	// book. The book is loaded lazily — the by-ID path keeps working with
	// no contacts file present.
	recipientAccountID := req.RecipientAccountID
	if req.Recipient != "" {
		book, bookErr := LoadContactBook()
		if bookErr != nil {
			return fail("INVALID_INPUT", bookErr)
		}
		resolved, resolveErr := book.Resolve(req.Recipient)
		if resolveErr != nil {
			if errors.Is(resolveErr, ErrContactNotFound) {
				return fail("CONTACT_NOT_FOUND", resolveErr)
			}
			return fail("INVALID_INPUT", resolveErr)
		}
		recipientAccountID = resolved
	}

	// Pre-sign audit-message size check: if audit logging is enabled and the
	// resulting JSON would exceed HCS's effective per-message limit, surface
	// the configuration problem here rather than as a post-transfer audit
	// failure.
	if cfg.auditTopicID != nil {
		tokenIDForAudit := ""
		if asset.Kind == AssetKindHTS {
			tokenIDForAudit = asset.TokenID
		}
		if size := auditMessageSizeUpperBound(asset.Symbol, tokenIDForAudit, req.Memo); size > maxAuditMessageBytes {
			return fail("INVALID_INPUT", fmt.Errorf("audit message would exceed %d-byte HCS limit (estimated %d bytes)", maxAuditMessageBytes, size))
		}
	}

	client, err := buildClient(cfg)
	if err != nil {
		return fail("AUTH_ERROR", err)
	}
	defer func() { _ = client.Close() }()

	store, err := OpenSQLitePaymentStore("")
	if err != nil {
		return fail("CONFIG_MISSING", err)
	}
	defer func() { _ = store.Close() }()

	recipientID, err := hiero.AccountIDFromString(recipientAccountID)
	if err != nil {
		return fail("TRANSFER_FAILED", fmt.Errorf("invalid recipientAccountId: %w", err))
	}

	deps := Deps{
		Cfg:    cfg,
		Signer: &HieroSigner{Client: client},
		Store:  store,
		Client: client,
	}
	transfer := Transfer{
		AssetKind:   asset.Kind,
		AssetSymbol: asset.Symbol,
		Decimals:    asset.Decimals,
		From:        cfg.operatorID,
		To:          recipientID,
		ToName:      req.Recipient,
		RawUnits:    rawUnits,
		Memo:        req.Memo,
	}
	if asset.Kind == AssetKindHTS {
		tokenID, parseErr := hiero.TokenIDFromString(asset.TokenID)
		if parseErr != nil {
			return fail("CONFIG_MISSING", fmt.Errorf("registry tokenId %q: %w", asset.TokenID, parseErr))
		}
		transfer.TokenID = tokenID
	}

	result, err := Pay(context.Background(), deps, req, transfer)
	if err != nil {
		return fail("TRANSFER_FAILED", err)
	}

	encoded, _ := json.Marshal(result)
	fmt.Println(string(encoded))
	return nil
}

type config struct {
	operatorID   hiero.AccountID
	operatorKey  hiero.PrivateKey
	network      string
	auditTopicID *hiero.TopicID  // nil when audit logging is disabled
	maxAmount    decimal.Decimal // upper cap on a single payment
}

func loadConfig() (*config, error) {
	opID := os.Getenv("OPERATOR_ID")
	opKey := os.Getenv("OPERATOR_KEY")
	network := os.Getenv("HEDERA_NETWORK")

	missing := []string{}
	for k, v := range map[string]string{
		"OPERATOR_ID":    opID,
		"OPERATOR_KEY":   opKey,
		"HEDERA_NETWORK": network,
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
