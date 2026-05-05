package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeContacts creates a contacts file in a tempdir and points
// HIERO_PAY_CONTACTS at it for the duration of the test. The path is returned
// so callers can use it for negative paths (e.g. "missing file" by deleting
// or pointing elsewhere).
func writeContacts(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "contacts.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write contacts file: %v", err)
	}
	t.Setenv("HIERO_PAY_CONTACTS", path)
	return path
}

func TestLoadContactBook_MissingFile_ReturnsEmptyBook(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HIERO_PAY_CONTACTS", filepath.Join(dir, "does-not-exist.json"))

	book, err := LoadContactBook()
	if err != nil {
		t.Fatalf("LoadContactBook err=%v, want nil for missing file", err)
	}
	if got := len(book.byName); got != 0 {
		t.Errorf("missing-file book size = %d, want 0", got)
	}
}

func TestLoadContactBook_MalformedJSON_Errors(t *testing.T) {
	writeContacts(t, "not json at all")

	if _, err := LoadContactBook(); err == nil {
		t.Fatal("LoadContactBook returned nil error for malformed JSON")
	}
}

func TestLoadContactBook_TolerateUnknownFields(t *testing.T) {
	// Future-proof fields (memo, tags, network) must be silently accepted so
	// the schema can evolve without breaking older binaries.
	writeContacts(t, `[{"name":"alice","accountId":"0.0.1234","memo":"old","tags":["x"],"network":"testnet"}]`)

	book, err := LoadContactBook()
	if err != nil {
		t.Fatalf("LoadContactBook err=%v, want nil with unknown fields", err)
	}
	if got, _ := book.Resolve("alice"); got != "0.0.1234" {
		t.Errorf("Resolve(\"alice\") = %q, want %q", got, "0.0.1234")
	}
}

func TestLoadContactBook_DuplicateNamesCaseInsensitive_Errors(t *testing.T) {
	writeContacts(t, `[{"name":"Alice","accountId":"0.0.1"},{"name":"alice","accountId":"0.0.2"}]`)

	_, err := LoadContactBook()
	if err == nil {
		t.Fatal("LoadContactBook returned nil error for case-insensitive duplicate")
	}
	if !strings.Contains(err.Error(), "duplicate") {
		t.Errorf("error %q does not mention duplicate", err.Error())
	}
}

func TestLoadContactBook_InvalidNameCharacters_Errors(t *testing.T) {
	writeContacts(t, `[{"name":"alice@vendor","accountId":"0.0.1"}]`)

	_, err := LoadContactBook()
	if err == nil {
		t.Fatal("LoadContactBook returned nil error for invalid name chars")
	}
}

func TestLoadContactBook_InvalidAccountID_Errors(t *testing.T) {
	writeContacts(t, `[{"name":"alice","accountId":"not-an-id"}]`)

	_, err := LoadContactBook()
	if err == nil {
		t.Fatal("LoadContactBook returned nil error for malformed accountId")
	}
}

func TestLoadContactBook_NameTooLong_Errors(t *testing.T) {
	long := strings.Repeat("a", maxContactNameLen+1)
	writeContacts(t, `[{"name":"`+long+`","accountId":"0.0.1"}]`)

	_, err := LoadContactBook()
	if err == nil {
		t.Fatal("LoadContactBook returned nil error for over-long name")
	}
}

func TestLoadContactBook_EmptyName_Errors(t *testing.T) {
	writeContacts(t, `[{"name":"","accountId":"0.0.1"}]`)

	_, err := LoadContactBook()
	if err == nil {
		t.Fatal("LoadContactBook returned nil error for empty name")
	}
}

func TestLoadContactBook_AliasesAllowed(t *testing.T) {
	// Two distinct names pointing at the same account is supported on purpose:
	// the operator's "boss" can also be "alice".
	writeContacts(t, `[{"name":"alice","accountId":"0.0.1"},{"name":"boss","accountId":"0.0.1"}]`)

	book, err := LoadContactBook()
	if err != nil {
		t.Fatalf("LoadContactBook err=%v, want nil for aliases", err)
	}
	if got, _ := book.Resolve("alice"); got != "0.0.1" {
		t.Errorf("Resolve(\"alice\") = %q, want %q", got, "0.0.1")
	}
	if got, _ := book.Resolve("boss"); got != "0.0.1" {
		t.Errorf("Resolve(\"boss\") = %q, want %q", got, "0.0.1")
	}
}

func TestContactBook_Resolve_KnownName_ReturnsAccountID(t *testing.T) {
	writeContacts(t, `[{"name":"alice","accountId":"0.0.1234"}]`)
	book, err := LoadContactBook()
	if err != nil {
		t.Fatalf("LoadContactBook err=%v", err)
	}

	got, err := book.Resolve("alice")
	if err != nil {
		t.Fatalf("Resolve err=%v, want nil", err)
	}
	if got != "0.0.1234" {
		t.Errorf("Resolve = %q, want %q", got, "0.0.1234")
	}
}

func TestContactBook_Resolve_CaseInsensitive(t *testing.T) {
	writeContacts(t, `[{"name":"Alice","accountId":"0.0.1234"}]`)
	book, _ := LoadContactBook()

	cases := []string{"alice", "ALICE", "AlIcE", "Alice"}
	for _, q := range cases {
		t.Run(q, func(t *testing.T) {
			got, err := book.Resolve(q)
			if err != nil {
				t.Fatalf("Resolve(%q) err=%v", q, err)
			}
			if got != "0.0.1234" {
				t.Errorf("Resolve(%q) = %q, want %q", q, got, "0.0.1234")
			}
		})
	}
}

func TestContactBook_Resolve_TrimsWhitespace(t *testing.T) {
	writeContacts(t, `[{"name":"alice","accountId":"0.0.1234"}]`)
	book, _ := LoadContactBook()

	got, err := book.Resolve("  alice  ")
	if err != nil {
		t.Fatalf("Resolve err=%v, want nil after trim", err)
	}
	if got != "0.0.1234" {
		t.Errorf("Resolve(\"  alice  \") = %q, want %q", got, "0.0.1234")
	}
}

func TestContactBook_Resolve_UnknownName_ReturnsContactNotFound(t *testing.T) {
	writeContacts(t, `[{"name":"alice","accountId":"0.0.1"}]`)
	book, _ := LoadContactBook()

	_, err := book.Resolve("zelda")
	if err == nil {
		t.Fatal("Resolve returned nil error for unknown name")
	}
	// errors.Is must catch the sentinel — substring matching the message would
	// be a false-positive trap if the wrapping format ever changes.
	if !errors.Is(err, ErrContactNotFound) {
		t.Errorf("Resolve err=%v, want it to wrap ErrContactNotFound", err)
	}
	if !strings.Contains(err.Error(), "alice") {
		t.Errorf("not-found message %q does not include the known name as a hint", err.Error())
	}
}

func TestContactBook_Resolve_EmptyBookSuggestionMentionsEmpty(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HIERO_PAY_CONTACTS", filepath.Join(dir, "missing.json"))
	book, _ := LoadContactBook()

	_, err := book.Resolve("alice")
	if err == nil {
		t.Fatal("Resolve returned nil error against an empty book")
	}
	if !strings.Contains(err.Error(), "empty") {
		t.Errorf("empty-book message %q should signal that the book is empty", err.Error())
	}
}

func TestContactBook_Resolve_SuggestionsCappedAtFive(t *testing.T) {
	body := `[
		{"name":"a","accountId":"0.0.1"},
		{"name":"b","accountId":"0.0.2"},
		{"name":"c","accountId":"0.0.3"},
		{"name":"d","accountId":"0.0.4"},
		{"name":"e","accountId":"0.0.5"},
		{"name":"f","accountId":"0.0.6"},
		{"name":"g","accountId":"0.0.7"}
	]`
	writeContacts(t, body)
	book, _ := LoadContactBook()

	_, err := book.Resolve("zzz")
	if err == nil {
		t.Fatal("expected error for unknown name")
	}
	msg := err.Error()
	// At most five names should appear; an ellipsis indicates truncation.
	if !strings.Contains(msg, "…") {
		t.Errorf("over-limit suggestion list %q should include the truncation marker", msg)
	}
	// "f" and "g" sort after "e"; they must not appear.
	if strings.Contains(msg, "f,") || strings.Contains(msg, "g") {
		t.Errorf("suggestion list %q should be capped at the first 5 alphabetically", msg)
	}
}
