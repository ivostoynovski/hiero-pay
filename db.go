package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// PaymentStore is the persistence boundary for payment history. Record /
// UpdateAudit are write-side; Query is the read path used by the history
// subcommand.
type PaymentStore interface {
	Record(ctx context.Context, p PaymentRow) error
	UpdateAudit(ctx context.Context, txID string, a AuditOutcome) error
	Query(ctx context.Context, filter QueryFilter) ([]PaymentRow, error)
}

// PaymentRow mirrors the payments table schema. JSON tags drive the
// machine-readable output of `hiero-pay history --format=json`.
type PaymentRow struct {
	TxID               string `json:"txId"`
	SchemaVersion      int    `json:"-"` // internal; not part of the history output
	Status             string `json:"status"`
	Network            string `json:"network"`
	FromAccount        string `json:"from"`
	ToAccount          string `json:"to"`
	ToName             string `json:"toName,omitempty"`
	Asset              string `json:"asset"`
	TokenID            string `json:"tokenId,omitempty"`
	Decimals           int    `json:"decimals"`
	AmountDecimal      string `json:"amount"`
	AmountRawUnits     int64  `json:"amountRawUnits"`
	Memo               string `json:"memo,omitempty"`
	SubmittedAt        string `json:"submittedAt"`
	ConsensusTimestamp string `json:"consensusTimestamp,omitempty"`
	AuditStatus        string `json:"auditStatus"`
	AuditTopicID       string `json:"auditTopicId,omitempty"`
	AuditSeqNumber     int64  `json:"auditSeqNumber,omitempty"`
	AuditError         string `json:"auditError,omitempty"`
}

// QueryFilter narrows a Query call. Empty / zero-value fields are not
// applied — Limit ≤ 0 falls back to defaultQueryLimit. Recipient matches
// either to_account (exact) or to_name (case-insensitive) so the operator
// can pass either form.
type QueryFilter struct {
	Since     time.Time
	Until     time.Time
	Asset     string
	Recipient string
	Status    string
	Limit     int
}

const defaultQueryLimit = 50

// AuditOutcome carries the audit submission's outcome back into the row
// after writeAudit has run.
type AuditOutcome struct {
	Status     string // SUCCESS | SKIPPED | FAILED
	TopicID    string
	SeqNumber  int64
	ErrMessage string
}

// SQLitePaymentStore is the production PaymentStore adapter. It owns the
// *sql.DB handle and runs schema migrations on Open so callers don't have
// to.
type SQLitePaymentStore struct {
	db *sql.DB
}

// OpenSQLitePaymentStore opens the SQLite database at path (or
// $HIERO_PAY_DB / ./history.db if path is empty), runs forward-only schema
// migrations to bring it up to current, and returns a ready-to-use store.
func OpenSQLitePaymentStore(path string) (*SQLitePaymentStore, error) {
	if path == "" {
		path = os.Getenv("HIERO_PAY_DB")
	}
	if path == "" {
		path = "history.db"
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite at %q: %w", path, err)
	}
	if err := migrateSchema(db); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migrate schema at %q: %w", path, err)
	}
	return &SQLitePaymentStore{db: db}, nil
}

func (s *SQLitePaymentStore) Close() error {
	return s.db.Close()
}

// Record inserts a new payment row. tx_id is the primary key, so a re-record
// of the same transaction returns an error rather than silently overwriting.
func (s *SQLitePaymentStore) Record(ctx context.Context, p PaymentRow) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO payments (
			tx_id, schema_version, status, network,
			from_account, to_account, to_name,
			asset, token_id, decimals,
			amount_decimal, amount_raw_units, memo,
			submitted_at, consensus_timestamp,
			audit_status, audit_topic_id, audit_seq_number, audit_error
		) VALUES (
			?, ?, ?, ?,
			?, ?, ?,
			?, ?, ?,
			?, ?, ?,
			?, ?,
			?, ?, ?, ?
		)`,
		p.TxID, p.SchemaVersion, p.Status, p.Network,
		p.FromAccount, p.ToAccount, nullableString(p.ToName),
		p.Asset, nullableString(p.TokenID), p.Decimals,
		p.AmountDecimal, p.AmountRawUnits, nullableString(p.Memo),
		p.SubmittedAt, nullableString(p.ConsensusTimestamp),
		p.AuditStatus, nullableString(p.AuditTopicID), nullableInt64(p.AuditSeqNumber), nullableString(p.AuditError),
	)
	if err != nil {
		return fmt.Errorf("insert payment row %s: %w", p.TxID, err)
	}
	return nil
}

// Query reads payment rows matching filter, ordered newest-first. It
// returns rows even when audit_status is PENDING / FAILED — the operator
// asked for "what did I pay," not "what audited cleanly."
func (s *SQLitePaymentStore) Query(ctx context.Context, f QueryFilter) ([]PaymentRow, error) {
	where := []string{}
	args := []any{}
	if !f.Since.IsZero() {
		where = append(where, "submitted_at >= ?")
		args = append(args, f.Since.UTC().Format(time.RFC3339))
	}
	if !f.Until.IsZero() {
		where = append(where, "submitted_at < ?")
		args = append(args, f.Until.UTC().Format(time.RFC3339))
	}
	if f.Asset != "" {
		where = append(where, "asset = ?")
		args = append(args, f.Asset)
	}
	if f.Recipient != "" {
		where = append(where, "(to_account = ? OR to_name COLLATE NOCASE = ?)")
		args = append(args, f.Recipient, f.Recipient)
	}
	if f.Status != "" {
		where = append(where, "status = ?")
		args = append(args, f.Status)
	}

	limit := f.Limit
	if limit <= 0 {
		limit = defaultQueryLimit
	}

	q := `SELECT
			tx_id, schema_version, status, network,
			from_account, to_account, to_name,
			asset, token_id, decimals,
			amount_decimal, amount_raw_units, memo,
			submitted_at, consensus_timestamp,
			audit_status, audit_topic_id, audit_seq_number, audit_error
		FROM payments`
	if len(where) > 0 {
		q += " WHERE " + strings.Join(where, " AND ")
	}
	q += " ORDER BY submitted_at DESC LIMIT ?"
	args = append(args, limit)

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("query payments: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []PaymentRow
	for rows.Next() {
		var p PaymentRow
		var toName, tokenID, memo, consensus, topicID, auditError sql.NullString
		var seq sql.NullInt64
		if err := rows.Scan(
			&p.TxID, &p.SchemaVersion, &p.Status, &p.Network,
			&p.FromAccount, &p.ToAccount, &toName,
			&p.Asset, &tokenID, &p.Decimals,
			&p.AmountDecimal, &p.AmountRawUnits, &memo,
			&p.SubmittedAt, &consensus,
			&p.AuditStatus, &topicID, &seq, &auditError,
		); err != nil {
			return nil, fmt.Errorf("scan payment row: %w", err)
		}
		p.ToName = toName.String
		p.TokenID = tokenID.String
		p.Memo = memo.String
		p.ConsensusTimestamp = consensus.String
		p.AuditTopicID = topicID.String
		p.AuditSeqNumber = seq.Int64
		p.AuditError = auditError.String
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate payment rows: %w", err)
	}
	return out, nil
}

// UpdateAudit fills in the audit_* columns of an existing row identified by
// tx_id. Returns an error if no row matches — in practice that means
// Record was never called or the tx_id is wrong.
func (s *SQLitePaymentStore) UpdateAudit(ctx context.Context, txID string, a AuditOutcome) error {
	res, err := s.db.ExecContext(ctx, `
		UPDATE payments
		SET audit_status = ?,
		    audit_topic_id = ?,
		    audit_seq_number = ?,
		    audit_error = ?
		WHERE tx_id = ?`,
		a.Status,
		nullableString(a.TopicID),
		nullableInt64(a.SeqNumber),
		nullableString(a.ErrMessage),
		txID,
	)
	if err != nil {
		return fmt.Errorf("update audit for %s: %w", txID, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected for %s: %w", txID, err)
	}
	if n == 0 {
		return fmt.Errorf("update audit: no row with tx_id %s", txID)
	}
	return nil
}

// nullableString returns sql.NullString so empty strings persist as SQL NULL
// rather than empty-string. The schema treats NULL as "field not applicable
// to this row" (e.g. token_id is NULL for HBAR).
func nullableString(s string) any {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}

func nullableInt64(n int64) any {
	if n == 0 {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: n, Valid: true}
}

// schemaVersion is the migration target. Bump this and add a corresponding
// case in applyMigration when the schema changes.
const schemaVersion = 1

// migrateSchema creates the _meta table if missing, reads the current
// schema_version, and applies forward-only migrations until the DB is at
// schemaVersion.
func migrateSchema(db *sql.DB) error {
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS _meta (
			key   TEXT PRIMARY KEY,
			value TEXT NOT NULL
		)`); err != nil {
		return fmt.Errorf("create _meta table: %w", err)
	}

	current, err := readSchemaVersion(db)
	if err != nil {
		return err
	}

	for current < schemaVersion {
		next := current + 1
		if err := applyMigration(db, next); err != nil {
			return fmt.Errorf("apply migration v%d: %w", next, err)
		}
		if err := writeSchemaVersion(db, next); err != nil {
			return fmt.Errorf("write schema_version=%d: %w", next, err)
		}
		current = next
	}
	return nil
}

func readSchemaVersion(db *sql.DB) (int, error) {
	var raw string
	err := db.QueryRow(`SELECT value FROM _meta WHERE key = 'schema_version'`).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("read schema_version: %w", err)
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("parse schema_version %q: %w", raw, err)
	}
	return v, nil
}

func writeSchemaVersion(db *sql.DB, v int) error {
	_, err := db.Exec(`
		INSERT INTO _meta(key, value) VALUES ('schema_version', ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value`, strconv.Itoa(v))
	return err
}

func applyMigration(db *sql.DB, v int) error {
	switch v {
	case 1:
		return applyMigrationV1(db)
	default:
		return fmt.Errorf("unknown schema version %d", v)
	}
}

func applyMigrationV1(db *sql.DB) error {
	stmts := []string{
		`CREATE TABLE payments (
			tx_id              TEXT PRIMARY KEY,
			schema_version     INTEGER NOT NULL,
			status             TEXT NOT NULL,
			network            TEXT NOT NULL,
			from_account       TEXT NOT NULL,
			to_account         TEXT NOT NULL,
			to_name            TEXT,
			asset              TEXT NOT NULL,
			token_id           TEXT,
			decimals           INTEGER NOT NULL,
			amount_decimal     TEXT NOT NULL,
			amount_raw_units   INTEGER NOT NULL,
			memo               TEXT,
			submitted_at       TEXT NOT NULL,
			consensus_timestamp TEXT,
			audit_status       TEXT NOT NULL,
			audit_topic_id     TEXT,
			audit_seq_number   INTEGER,
			audit_error        TEXT
		)`,
		`CREATE INDEX idx_payments_submitted_at ON payments(submitted_at DESC)`,
		`CREATE INDEX idx_payments_to_account   ON payments(to_account)`,
		`CREATE INDEX idx_payments_asset        ON payments(asset)`,
		`CREATE INDEX idx_payments_to_name      ON payments(to_name) WHERE to_name IS NOT NULL`,
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			return err
		}
	}
	return nil
}
