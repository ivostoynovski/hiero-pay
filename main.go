package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math"
	"os"

	hiero "github.com/hiero-ledger/hiero-sdk-go/v2/sdk"
)

// usdcDecimals is the standard precision for Circle USDC. Hardcoded for v1;
// could be fetched from the token info at runtime later.
const usdcDecimals = 6

// Result is the JSON written to stdout on success.
type Result struct {
	TransactionID string `json:"transactionId"`
	Status        string `json:"status"`
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

	client, err := buildClient(cfg)
	if err != nil {
		return fail("AUTH_ERROR", err)
	}
	defer func() { _ = client.Close() }()

	result, err := executeTransfer(client, cfg, &req)
	if err != nil {
		return fail("TRANSFER_FAILED", err)
	}

	encoded, _ := json.Marshal(result)
	fmt.Println(string(encoded))
	return nil
}

type config struct {
	operatorID  hiero.AccountID
	operatorKey hiero.PrivateKey
	network     string
	tokenID     hiero.TokenID
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
	privKey, err := hiero.PrivateKeyFromString(opKey)
	if err != nil {
		return nil, fmt.Errorf("invalid OPERATOR_KEY: %w", err)
	}
	tID, err := hiero.TokenIDFromString(tokenID)
	if err != nil {
		return nil, fmt.Errorf("invalid USDC_TOKEN_ID %q: %w", tokenID, err)
	}

	return &config{
		operatorID:  accountID,
		operatorKey: privKey,
		network:     network,
		tokenID:     tID,
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

func executeTransfer(client *hiero.Client, cfg *config, req *PaymentRequest) (*Result, error) {
	recipientID, err := hiero.AccountIDFromString(req.RecipientAccountID)
	if err != nil {
		return nil, fmt.Errorf("invalid recipientAccountId: %w", err)
	}

	rawUnits := int64(math.Round(req.Amount * math.Pow10(usdcDecimals)))
	if rawUnits <= 0 {
		return nil, fmt.Errorf("amount %v rounds to zero raw units at %d decimals", req.Amount, usdcDecimals)
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

// fail writes a structured JSON error to stderr and returns the original error.
// The caller propagates the error so main() exits with code 1.
func fail(code string, err error) error {
	out := ErrorOut{Code: code, Error: err.Error()}
	encoded, _ := json.Marshal(out)
	fmt.Fprintln(os.Stderr, string(encoded))
	return err
}
