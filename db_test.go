package main

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// openTempStore opens a SQLitePaymentStore against a fresh temp DB file.
// HIERO_PAY_DB is set so the store reads from that path; the file is
// cleaned up automatically when the test ends.
func openTempStore(t *testing.T) *SQLitePaymentStore {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "history.db")
	t.Setenv("HIERO_PAY_DB", path)
	store, err := OpenSQLitePaymentStore("")
	if err != nil {
		t.Fatalf("OpenSQLitePaymentStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func TestSQLitePaymentStore_OpenRunsMigrations(t *testing.T) {
	store := openTempStore(t)

	// _meta should record the current schema version.
	var v string
	if err := store.db.QueryRow(`SELECT value FROM _meta WHERE key='schema_version'`).Scan(&v); err != nil {
		t.Fatalf("read schema_version: %v", err)
	}
	if v != "1" {
		t.Errorf("schema_version = %q, want %q", v, "1")
	}

	// payments table must exist with the expected columns. Querying any
	// column on an empty table is enough to verify the schema was applied.
	if _, err := store.db.Query(`SELECT tx_id, asset, audit_status FROM payments`); err != nil {
		t.Errorf("query payments table: %v", err)
	}

	// Indexes must exist — query sqlite_master for the expected names.
	for _, name := range []string{
		"idx_payments_submitted_at",
		"idx_payments_to_account",
		"idx_payments_asset",
		"idx_payments_to_name",
	} {
		var found string
		err := store.db.QueryRow(`SELECT name FROM sqlite_master WHERE type='index' AND name=?`, name).Scan(&found)
		if err != nil {
			t.Errorf("index %q missing: %v", name, err)
		}
	}
}

func TestSQLitePaymentStore_Record_PersistsRow(t *testing.T) {
	store := openTempStore(t)
	row := PaymentRow{
		TxID:           "0.0.111@1700000000.0",
		SchemaVersion:  schemaVersion,
		Status:         "SUCCESS",
		Network:        "testnet",
		FromAccount:    "0.0.111",
		ToAccount:      "0.0.222",
		ToName:         "alice",
		Asset:          "USDC",
		TokenID:        "0.0.429274",
		Decimals:       6,
		AmountDecimal:  "1.5",
		AmountRawUnits: 1_500_000,
		Memo:           "test",
		SubmittedAt:    "2026-05-05T12:00:00Z",
		AuditStatus:    "PENDING",
	}
	if err := store.Record(context.Background(), row); err != nil {
		t.Fatalf("Record: %v", err)
	}

	var got PaymentRow
	var toName, tokenID, memo, consensus, topicID, auditError sql.NullString
	var seqNum sql.NullInt64
	err := store.db.QueryRow(`
		SELECT tx_id, schema_version, status, network,
		       from_account, to_account, to_name,
		       asset, token_id, decimals,
		       amount_decimal, amount_raw_units, memo,
		       submitted_at, consensus_timestamp,
		       audit_status, audit_topic_id, audit_seq_number, audit_error
		FROM payments WHERE tx_id = ?`, row.TxID).Scan(
		&got.TxID, &got.SchemaVersion, &got.Status, &got.Network,
		&got.FromAccount, &got.ToAccount, &toName,
		&got.Asset, &tokenID, &got.Decimals,
		&got.AmountDecimal, &got.AmountRawUnits, &memo,
		&got.SubmittedAt, &consensus,
		&got.AuditStatus, &topicID, &seqNum, &auditError,
	)
	if err != nil {
		t.Fatalf("read back row: %v", err)
	}

	if got.TxID != row.TxID {
		t.Errorf("TxID = %q, want %q", got.TxID, row.TxID)
	}
	if toName.String != "alice" {
		t.Errorf("to_name = %q, want %q", toName.String, "alice")
	}
	if got.Asset != "USDC" {
		t.Errorf("asset = %q, want %q", got.Asset, "USDC")
	}
	if tokenID.String != "0.0.429274" {
		t.Errorf("token_id = %q, want %q", tokenID.String, "0.0.429274")
	}
	if got.AmountRawUnits != 1_500_000 {
		t.Errorf("amount_raw_units = %d, want %d", got.AmountRawUnits, 1_500_000)
	}
	if got.AuditStatus != "PENDING" {
		t.Errorf("audit_status = %q, want %q", got.AuditStatus, "PENDING")
	}
}

func TestSQLitePaymentStore_Record_HBAR_NoTokenID(t *testing.T) {
	store := openTempStore(t)
	row := PaymentRow{
		TxID:           "0.0.111@1700000001.0",
		SchemaVersion:  schemaVersion,
		Status:         "SUCCESS",
		Network:        "testnet",
		FromAccount:    "0.0.111",
		ToAccount:      "0.0.222",
		Asset:          "HBAR",
		Decimals:       8,
		AmountDecimal:  "1.5",
		AmountRawUnits: 150_000_000,
		SubmittedAt:    "2026-05-05T12:00:00Z",
		AuditStatus:    "PENDING",
	}
	if err := store.Record(context.Background(), row); err != nil {
		t.Fatalf("Record HBAR: %v", err)
	}

	// HBAR rows must store token_id as SQL NULL, not empty string.
	var tokenID sql.NullString
	err := store.db.QueryRow(`SELECT token_id FROM payments WHERE tx_id = ?`, row.TxID).Scan(&tokenID)
	if err != nil {
		t.Fatalf("read token_id: %v", err)
	}
	if tokenID.Valid {
		t.Errorf("HBAR row stored token_id = %q, want NULL", tokenID.String)
	}
}

func TestSQLitePaymentStore_Record_RejectsDuplicateTxID(t *testing.T) {
	store := openTempStore(t)
	row := PaymentRow{
		TxID:           "0.0.111@1700000002.0",
		SchemaVersion:  schemaVersion,
		Status:         "SUCCESS",
		Network:        "testnet",
		FromAccount:    "0.0.111",
		ToAccount:      "0.0.222",
		Asset:          "USDC",
		TokenID:        "0.0.429274",
		Decimals:       6,
		AmountDecimal:  "1.0",
		AmountRawUnits: 1_000_000,
		SubmittedAt:    "2026-05-05T12:00:00Z",
		AuditStatus:    "PENDING",
	}
	if err := store.Record(context.Background(), row); err != nil {
		t.Fatalf("first Record: %v", err)
	}
	if err := store.Record(context.Background(), row); err == nil {
		t.Error("second Record returned nil error, want PK conflict")
	}
}

func TestSQLitePaymentStore_UpdateAudit_FillsFields(t *testing.T) {
	store := openTempStore(t)
	row := PaymentRow{
		TxID:           "0.0.111@1700000003.0",
		SchemaVersion:  schemaVersion,
		Status:         "SUCCESS",
		Network:        "testnet",
		FromAccount:    "0.0.111",
		ToAccount:      "0.0.222",
		Asset:          "USDC",
		TokenID:        "0.0.429274",
		Decimals:       6,
		AmountDecimal:  "1.0",
		AmountRawUnits: 1_000_000,
		SubmittedAt:    "2026-05-05T12:00:00Z",
		AuditStatus:    "PENDING",
	}
	if err := store.Record(context.Background(), row); err != nil {
		t.Fatalf("Record: %v", err)
	}

	outcome := AuditOutcome{
		Status:    "SUCCESS",
		TopicID:   "0.0.5555",
		SeqNumber: 42,
	}
	if err := store.UpdateAudit(context.Background(), row.TxID, outcome); err != nil {
		t.Fatalf("UpdateAudit: %v", err)
	}

	var status string
	var topicID sql.NullString
	var seq sql.NullInt64
	err := store.db.QueryRow(
		`SELECT audit_status, audit_topic_id, audit_seq_number FROM payments WHERE tx_id = ?`,
		row.TxID,
	).Scan(&status, &topicID, &seq)
	if err != nil {
		t.Fatalf("read audit fields: %v", err)
	}
	if status != "SUCCESS" {
		t.Errorf("audit_status = %q, want SUCCESS", status)
	}
	if topicID.String != "0.0.5555" {
		t.Errorf("audit_topic_id = %q, want 0.0.5555", topicID.String)
	}
	if seq.Int64 != 42 {
		t.Errorf("audit_seq_number = %d, want 42", seq.Int64)
	}
}

func TestSQLitePaymentStore_UpdateAudit_UnknownTxIDErrors(t *testing.T) {
	store := openTempStore(t)
	err := store.UpdateAudit(context.Background(), "no-such-tx", AuditOutcome{Status: "SUCCESS"})
	if err == nil {
		t.Fatal("UpdateAudit on unknown tx returned nil error")
	}
	if !strings.Contains(err.Error(), "no row") {
		t.Errorf("error %q should mention that no row matches", err.Error())
	}
}

// seedRow is a tiny helper for the Query tests. Inserts one row and
// returns the row's TxID for later assertions.
func seedRow(t *testing.T, store *SQLitePaymentStore, p PaymentRow) {
	t.Helper()
	if err := store.Record(context.Background(), p); err != nil {
		t.Fatalf("seed Record: %v", err)
	}
}

func makeRow(txID, asset, toAccount, toName, submittedAt string) PaymentRow {
	return PaymentRow{
		TxID:           txID,
		SchemaVersion:  schemaVersion,
		Status:         "SUCCESS",
		Network:        "testnet",
		FromAccount:    "0.0.111",
		ToAccount:      toAccount,
		ToName:         toName,
		Asset:          asset,
		Decimals:       6,
		AmountDecimal:  "1.0",
		AmountRawUnits: 1_000_000,
		SubmittedAt:    submittedAt,
		AuditStatus:    "SUCCESS",
	}
}

func TestSQLitePaymentStore_Query_FilterByAsset(t *testing.T) {
	store := openTempStore(t)
	seedRow(t, store, makeRow("tx1", "USDC", "0.0.222", "alice", "2026-05-05T10:00:00Z"))
	seedRow(t, store, makeRow("tx2", "HBAR", "0.0.222", "alice", "2026-05-05T11:00:00Z"))
	seedRow(t, store, makeRow("tx3", "USDC", "0.0.333", "bob", "2026-05-05T12:00:00Z"))

	rows, err := store.Query(context.Background(), QueryFilter{Asset: "USDC"})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("len(rows) = %d, want 2 USDC rows", len(rows))
	}
	for _, r := range rows {
		if r.Asset != "USDC" {
			t.Errorf("row asset=%q, want USDC", r.Asset)
		}
	}
}

func TestSQLitePaymentStore_Query_FilterByRecipient_AccountID(t *testing.T) {
	store := openTempStore(t)
	seedRow(t, store, makeRow("tx1", "USDC", "0.0.222", "alice", "2026-05-05T10:00:00Z"))
	seedRow(t, store, makeRow("tx2", "USDC", "0.0.333", "bob", "2026-05-05T11:00:00Z"))

	rows, err := store.Query(context.Background(), QueryFilter{Recipient: "0.0.222"})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(rows) != 1 || rows[0].TxID != "tx1" {
		t.Errorf("rows = %+v, want [tx1]", rows)
	}
}

func TestSQLitePaymentStore_Query_FilterByRecipient_Name_CaseInsensitive(t *testing.T) {
	store := openTempStore(t)
	seedRow(t, store, makeRow("tx1", "USDC", "0.0.222", "Alice", "2026-05-05T10:00:00Z"))

	cases := []string{"alice", "ALICE", "Alice"}
	for _, q := range cases {
		t.Run(q, func(t *testing.T) {
			rows, err := store.Query(context.Background(), QueryFilter{Recipient: q})
			if err != nil {
				t.Fatalf("Query(%q): %v", q, err)
			}
			if len(rows) != 1 {
				t.Errorf("Query(%q) returned %d rows, want 1 (case-insensitive on to_name)", q, len(rows))
			}
		})
	}
}

func TestSQLitePaymentStore_Query_FilterByDateRange(t *testing.T) {
	store := openTempStore(t)
	seedRow(t, store, makeRow("tx1", "USDC", "0.0.222", "", "2026-05-04T12:00:00Z"))
	seedRow(t, store, makeRow("tx2", "USDC", "0.0.222", "", "2026-05-05T12:00:00Z"))
	seedRow(t, store, makeRow("tx3", "USDC", "0.0.222", "", "2026-05-06T12:00:00Z"))

	since, _ := time.Parse(time.RFC3339, "2026-05-05T00:00:00Z")
	until, _ := time.Parse(time.RFC3339, "2026-05-06T00:00:00Z")

	rows, err := store.Query(context.Background(), QueryFilter{Since: since, Until: until})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(rows) != 1 || rows[0].TxID != "tx2" {
		t.Errorf("rows = %+v, want only tx2", rows)
	}
}

func TestSQLitePaymentStore_Query_OrdersNewestFirst(t *testing.T) {
	store := openTempStore(t)
	seedRow(t, store, makeRow("tx-old", "USDC", "0.0.222", "", "2026-05-01T12:00:00Z"))
	seedRow(t, store, makeRow("tx-new", "USDC", "0.0.222", "", "2026-05-05T12:00:00Z"))
	seedRow(t, store, makeRow("tx-mid", "USDC", "0.0.222", "", "2026-05-03T12:00:00Z"))

	rows, err := store.Query(context.Background(), QueryFilter{})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("len(rows) = %d, want 3", len(rows))
	}
	if rows[0].TxID != "tx-new" || rows[1].TxID != "tx-mid" || rows[2].TxID != "tx-old" {
		t.Errorf("order = [%s, %s, %s], want [tx-new, tx-mid, tx-old]", rows[0].TxID, rows[1].TxID, rows[2].TxID)
	}
}

func TestSQLitePaymentStore_Query_LimitTruncates(t *testing.T) {
	store := openTempStore(t)
	for i := 0; i < 5; i++ {
		seedRow(t, store, makeRow(fmt.Sprintf("tx%d", i), "USDC", "0.0.222", "", fmt.Sprintf("2026-05-0%dT12:00:00Z", i+1)))
	}
	rows, err := store.Query(context.Background(), QueryFilter{Limit: 2})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(rows) != 2 {
		t.Errorf("len(rows) = %d, want 2 (--limit cap)", len(rows))
	}
}

func TestSQLitePaymentStore_Query_CombinedFilters(t *testing.T) {
	store := openTempStore(t)
	seedRow(t, store, makeRow("tx1", "USDC", "0.0.222", "alice", "2026-05-04T12:00:00Z"))
	seedRow(t, store, makeRow("tx2", "HBAR", "0.0.222", "alice", "2026-05-05T12:00:00Z"))
	seedRow(t, store, makeRow("tx3", "USDC", "0.0.333", "bob", "2026-05-05T12:00:00Z"))
	seedRow(t, store, makeRow("tx4", "USDC", "0.0.222", "alice", "2026-05-05T13:00:00Z"))

	since, _ := time.Parse(time.RFC3339, "2026-05-05T00:00:00Z")

	rows, err := store.Query(context.Background(), QueryFilter{
		Asset:     "USDC",
		Recipient: "alice",
		Since:     since,
	})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	// Only tx4 matches all three: USDC + alice + on/after 2026-05-05.
	if len(rows) != 1 || rows[0].TxID != "tx4" {
		t.Errorf("rows = %+v, want only tx4", rows)
	}
}
