package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
)

type Contact struct {
	Name      string `json:"name"`
	AccountID string `json:"accountId"`
}

// contactNamePattern is the strict format expected for contact names: alphanum,
// underscore, hyphen. Keeps shell-typed names safe and avoids leading/trailing
// whitespace surprises.
var contactNamePattern = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

// maxContactNameLen is the per-name length cap. Long enough for realistic
// contact handles, short enough to keep error messages legible.
const maxContactNameLen = 64

// ContactBook is a name → contact map populated from a JSON file. Names are
// lowercased on load so Resolve is case-insensitive; Aliases are supported by
// allowing multiple distinct names to map to the same account.
type ContactBook struct {
	byName map[string]Contact
}

// ErrContactNotFound is returned by Resolve when the requested name has no
// entry in the book. The wrapping error message lists up to 5 known names so
// callers (including LLM agents) can surface a "did you mean" hint instead
// of guessing a fallback account ID.
var ErrContactNotFound = errors.New("contact not found")

// LoadContactBook reads the contacts file from $HIERO_PAY_CONTACTS or, if
// unset, from contacts.json in the current working directory. A missing file
// returns an empty book without error — by-account-ID requests must keep
// working with no contacts configured.
func LoadContactBook() (*ContactBook, error) {
	path := os.Getenv("HIERO_PAY_CONTACTS")
	if path == "" {
		path = "contacts.json"
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &ContactBook{byName: map[string]Contact{}}, nil
		}
		return nil, fmt.Errorf("read contacts file %q: %w", path, err)
	}

	var entries []Contact
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, fmt.Errorf("decode contacts file %q: %w", path, err)
	}

	book := &ContactBook{byName: make(map[string]Contact, len(entries))}
	for i, e := range entries {
		name := strings.TrimSpace(e.Name)
		if name == "" {
			return nil, fmt.Errorf("contacts file %q entry %d: name is empty", path, i)
		}
		if len(name) > maxContactNameLen {
			return nil, fmt.Errorf("contacts file %q entry %d: name %q exceeds %d-character limit", path, i, name, maxContactNameLen)
		}
		if !contactNamePattern.MatchString(name) {
			return nil, fmt.Errorf("contacts file %q entry %d: name %q has invalid characters (allowed: a-z, A-Z, 0-9, _, -)", path, i, name)
		}
		if !accountIDPattern.MatchString(e.AccountID) {
			return nil, fmt.Errorf("contacts file %q entry %d (%q): accountId %q is not a valid Hedera account ID", path, i, name, e.AccountID)
		}

		key := strings.ToLower(name)
		if existing, ok := book.byName[key]; ok {
			return nil, fmt.Errorf("contacts file %q has duplicate name %q (also %q) — names are matched case-insensitively", path, name, existing.Name)
		}
		book.byName[key] = Contact{Name: name, AccountID: e.AccountID}
	}

	return book, nil
}

// Resolve returns the account ID associated with a contact name. Lookup is
// case-insensitive and trimmed of surrounding whitespace, matching the
// normalization applied at load time.
func (b *ContactBook) Resolve(name string) (string, error) {
	key := strings.ToLower(strings.TrimSpace(name))
	if c, ok := b.byName[key]; ok {
		return c.AccountID, nil
	}
	return "", fmt.Errorf("%w: %q (known: %s)", ErrContactNotFound, name, b.suggestionString())
}

// suggestionString returns up to 5 known names, alphabetically sorted, so a
// CONTACT_NOT_FOUND error can carry a "did you mean" hint without dumping the
// whole address book into a CLI error message.
func (b *ContactBook) suggestionString() string {
	if len(b.byName) == 0 {
		return "address book is empty"
	}
	names := make([]string, 0, len(b.byName))
	for _, c := range b.byName {
		names = append(names, c.Name)
	}
	sort.Strings(names)
	if len(names) > 5 {
		return strings.Join(names[:5], ", ") + ", …"
	}
	return strings.Join(names, ", ")
}

