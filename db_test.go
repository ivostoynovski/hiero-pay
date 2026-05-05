package main

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
)

// fakePaymentStore is the test double for PaymentStore. It records every
// Record/UpdateAudit call so orchestrator tests assert on the actual values
// the orchestrator passed (per .claude/agents/test-verifier.md — fakes that
// ignore inputs are a false-positive trap).
type fakePaymentStore struct {
	recordCalls       []PaymentRow
	updateAuditCalls  []fakePaymentUpdate
	cannedRecordErr   error
	cannedUpdateError error
}

type fakePaymentUpdate struct {
	TxID    string
	Outcome AuditOutcome
}

func (f *fakePaymentStore) Record(_ context.Context, p PaymentRow) error {
	f.recordCalls = append(f.recordCalls, p)
	return f.cannedRecordErr
}

func (f *fakePaymentStore) UpdateAudit(_ context.Context, txID string, a AuditOutcome) error {
	f.updateAuditCalls = append(f.updateAuditCalls, fakePaymentUpdate{TxID: txID, Outcome: a})
	return f.cannedUpdateError
}

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
