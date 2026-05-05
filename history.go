package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"text/tabwriter"
	"time"
)

// runHistory parses the `hiero-pay history` subcommand args, queries the
// payment store, and writes formatted output to stdout. Pure read — no
// signing path is constructed in this code path.
func runHistory(args []string, store PaymentStore, stdout io.Writer) error {
	fs := flag.NewFlagSet("history", flag.ContinueOnError)
	fs.SetOutput(stdout)

	var (
		since     = fs.String("since", "", "RFC3339 lower bound on submitted_at (inclusive)")
		until     = fs.String("until", "", "RFC3339 upper bound on submitted_at (exclusive)")
		asset     = fs.String("asset", "", "filter by asset symbol (e.g. USDC, HBAR)")
		recipient = fs.String("recipient", "", "filter by recipient (account ID or contact name)")
		status    = fs.String("status", "", "filter by transfer status (only SUCCESS recorded in v1)")
		limit     = fs.Int("limit", defaultQueryLimit, "maximum number of rows to return")
		format    = fs.String("format", "json", "output format: json or table")
	)

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}

	filter := QueryFilter{
		Asset:     *asset,
		Recipient: *recipient,
		Status:    *status,
		Limit:     *limit,
	}
	if *since != "" {
		t, err := time.Parse(time.RFC3339, *since)
		if err != nil {
			return fmt.Errorf("invalid --since %q: %w", *since, err)
		}
		filter.Since = t
	}
	if *until != "" {
		t, err := time.Parse(time.RFC3339, *until)
		if err != nil {
			return fmt.Errorf("invalid --until %q: %w", *until, err)
		}
		filter.Until = t
	}

	rows, err := store.Query(context.Background(), filter)
	if err != nil {
		return fmt.Errorf("query history: %w", err)
	}

	switch *format {
	case "json":
		return writeHistoryJSON(stdout, rows)
	case "table":
		return writeHistoryTable(stdout, rows)
	default:
		return fmt.Errorf("invalid --format %q (must be json or table)", *format)
	}
}

func writeHistoryJSON(w io.Writer, rows []PaymentRow) error {
	// Default to an empty array rather than `null` when there are no rows
	// so consumers can iterate without a nil check.
	if rows == nil {
		rows = []PaymentRow{}
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(rows)
}

func writeHistoryTable(w io.Writer, rows []PaymentRow) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	if _, err := fmt.Fprintln(tw, "TIMESTAMP\tRECIPIENT\tAMOUNT\tASSET\tSTATUS\tAUDIT"); err != nil {
		return err
	}
	for _, r := range rows {
		recipient := r.ToAccount
		if r.ToName != "" {
			recipient = r.ToName + " (" + r.ToAccount + ")"
		}
		if _, err := fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n",
			r.SubmittedAt, recipient, r.AmountDecimal, r.Asset, r.Status, r.AuditStatus,
		); err != nil {
			return err
		}
	}
	return tw.Flush()
}
