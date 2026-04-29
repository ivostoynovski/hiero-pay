// One-time setup: creates an HCS topic for the hiero-pay audit log on the
// configured Hedera network and prints the resulting topic ID.
//
// The topic's submit key is set to the operator's public key, so only this
// operator can write audit entries. The topic is publicly readable (HCS topics
// always are) — that's the point: an immutable, ordered, queryable record of
// every payment hiero-pay made.
//
// Usage (from repo root):
//
//	source .env
//	go run ./scripts/create-audit-topic
//
// Cost: ~$0.01 of HBAR (testnet HBAR is free).
package main

import (
	"fmt"
	"os"
	"strings"

	hiero "github.com/hiero-ledger/hiero-sdk-go/v2/sdk"
)

const topicMemo = "hiero-pay audit log"

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run() error {
	opID := os.Getenv("OPERATOR_ID")
	opKey := os.Getenv("OPERATOR_KEY")
	network := os.Getenv("HEDERA_NETWORK")
	if opID == "" || opKey == "" || network == "" {
		return fmt.Errorf("missing required env vars: OPERATOR_ID, OPERATOR_KEY, HEDERA_NETWORK (run `source .env` first)")
	}

	accountID, err := hiero.AccountIDFromString(opID)
	if err != nil {
		return fmt.Errorf("invalid OPERATOR_ID: %w", err)
	}
	privKey, err := parseOperatorKey(opKey)
	if err != nil {
		return fmt.Errorf("invalid OPERATOR_KEY: %w", err)
	}

	client, err := hiero.ClientForName(network)
	if err != nil {
		return fmt.Errorf("create client for network %q: %w", network, err)
	}
	defer func() { _ = client.Close() }()
	client.SetOperator(accountID, privKey)

	resp, err := hiero.NewTopicCreateTransaction().
		SetTopicMemo(topicMemo).
		SetSubmitKey(privKey.PublicKey()).
		SetAdminKey(privKey.PublicKey()).
		Execute(client)
	if err != nil {
		return fmt.Errorf("execute TopicCreate: %w", err)
	}

	receipt, err := resp.GetReceipt(client)
	if err != nil {
		return fmt.Errorf("get receipt: %w", err)
	}
	if receipt.TopicID == nil {
		return fmt.Errorf("topic ID missing from receipt (status=%s)", receipt.Status.String())
	}
	topicID := receipt.TopicID

	fmt.Println("Audit topic created.")
	fmt.Println()
	fmt.Printf("  Topic ID:    %s\n", topicID.String())
	fmt.Printf("  Memo:        %s\n", topicMemo)
	fmt.Printf("  Submit key:  operator (only %s can write)\n", accountID.String())
	fmt.Printf("  HashScan:    https://hashscan.io/%s/topic/%s\n", network, topicID.String())
	fmt.Println()
	fmt.Println("Update your .env:")
	fmt.Printf("  export AUDIT_TOPIC_ID=\"%s\"\n", topicID.String())
	return nil
}

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
