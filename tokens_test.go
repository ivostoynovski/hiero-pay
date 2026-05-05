package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeTokens creates a tokens registry file in a tempdir and points
// HIERO_PAY_TOKENS at it for the duration of the test.
func writeTokens(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "tokens.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write tokens file: %v", err)
	}
	t.Setenv("HIERO_PAY_TOKENS", path)
	return path
}

func TestLoadTokenRegistry_MissingFile_ReturnsEmptyRegistry(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HIERO_PAY_TOKENS", filepath.Join(dir, "does-not-exist.json"))

	reg, err := LoadTokenRegistry()
	if err != nil {
		t.Fatalf("LoadTokenRegistry err=%v, want nil for missing file", err)
	}
	if got := len(reg.bySymbol); got != 0 {
		t.Errorf("missing-file registry size = %d, want 0", got)
	}
}

func TestLoadTokenRegistry_MalformedJSON_Errors(t *testing.T) {
	writeTokens(t, "not json at all")
	if _, err := LoadTokenRegistry(); err == nil {
		t.Fatal("LoadTokenRegistry returned nil error for malformed JSON")
	}
}

func TestLoadTokenRegistry_InvalidSymbolCharacters_Errors(t *testing.T) {
	writeTokens(t, `{"us dc":{"tokenId":"0.0.1","decimals":6}}`)
	if _, err := LoadTokenRegistry(); err == nil {
		t.Fatal("LoadTokenRegistry returned nil error for invalid symbol")
	}
}

func TestLoadTokenRegistry_HBARSymbolReserved_Errors(t *testing.T) {
	writeTokens(t, `{"HBAR":{"tokenId":"0.0.1","decimals":8}}`)
	_, err := LoadTokenRegistry()
	if err == nil {
		t.Fatal("LoadTokenRegistry returned nil error for HBAR symbol in registry")
	}
	if !strings.Contains(err.Error(), "HBAR") {
		t.Errorf("HBAR-reserved error %q does not mention HBAR", err.Error())
	}
}

func TestLoadTokenRegistry_InvalidTokenID_Errors(t *testing.T) {
	writeTokens(t, `{"USDC":{"tokenId":"not-an-id","decimals":6}}`)
	if _, err := LoadTokenRegistry(); err == nil {
		t.Fatal("LoadTokenRegistry returned nil error for malformed tokenId")
	}
}

func TestLoadTokenRegistry_DecimalsOutOfRange_Errors(t *testing.T) {
	cases := []string{
		`{"USDC":{"tokenId":"0.0.1","decimals":-1}}`,
		`{"USDC":{"tokenId":"0.0.1","decimals":19}}`,
	}
	for _, body := range cases {
		t.Run(body, func(t *testing.T) {
			writeTokens(t, body)
			if _, err := LoadTokenRegistry(); err == nil {
				t.Fatal("LoadTokenRegistry returned nil error for decimals out of range")
			}
		})
	}
}

func TestLoadTokenRegistry_DecimalsZero_Valid(t *testing.T) {
	writeTokens(t, `{"WHOLE":{"tokenId":"0.0.123","decimals":0}}`)
	reg, err := LoadTokenRegistry()
	if err != nil {
		t.Fatalf("LoadTokenRegistry err=%v, want nil for decimals=0", err)
	}
	a, err := reg.Lookup("WHOLE")
	if err != nil {
		t.Fatalf("Lookup err=%v", err)
	}
	if a.Decimals != 0 {
		t.Errorf("Lookup decimals=%d, want 0", a.Decimals)
	}
}

func TestLoadTokenRegistry_ValidLoad_LooksUpSymbol(t *testing.T) {
	writeTokens(t, `{"USDC":{"tokenId":"0.0.429274","decimals":6}}`)
	reg, err := LoadTokenRegistry()
	if err != nil {
		t.Fatalf("LoadTokenRegistry err=%v", err)
	}
	got, err := reg.Lookup("USDC")
	if err != nil {
		t.Fatalf("Lookup err=%v", err)
	}
	if got.Kind != AssetKindHTS {
		t.Errorf("Lookup kind=%v, want HTS", got.Kind)
	}
	if got.TokenID != "0.0.429274" {
		t.Errorf("Lookup tokenId=%q, want %q", got.TokenID, "0.0.429274")
	}
	if got.Decimals != 6 {
		t.Errorf("Lookup decimals=%d, want 6", got.Decimals)
	}
}

func TestTokenRegistry_Lookup_HBARWithoutEntry(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HIERO_PAY_TOKENS", filepath.Join(dir, "missing.json"))
	reg, _ := LoadTokenRegistry()

	got, err := reg.Lookup("HBAR")
	if err != nil {
		t.Fatalf("Lookup HBAR err=%v, want nil — HBAR is built-in", err)
	}
	if got.Kind != AssetKindHBAR {
		t.Errorf("Lookup kind=%v, want HBAR", got.Kind)
	}
	if got.TokenID != "" {
		t.Errorf("Lookup tokenId=%q, want empty for HBAR", got.TokenID)
	}
	if got.Decimals != 8 {
		t.Errorf("Lookup decimals=%d, want 8 for HBAR", got.Decimals)
	}
}

func TestTokenRegistry_Lookup_CaseSensitive(t *testing.T) {
	writeTokens(t, `{"USDC":{"tokenId":"0.0.429274","decimals":6}}`)
	reg, _ := LoadTokenRegistry()

	if _, err := reg.Lookup("usdc"); err == nil {
		t.Error("Lookup(\"usdc\") returned nil error, want lookup failure (case-sensitive)")
	}
	if _, err := reg.Lookup("Usdc"); err == nil {
		t.Error("Lookup(\"Usdc\") returned nil error, want lookup failure (case-sensitive)")
	}
}

func TestTokenRegistry_Lookup_UnknownSymbol_ReturnsAssetNotFound(t *testing.T) {
	writeTokens(t, `{"USDC":{"tokenId":"0.0.429274","decimals":6}}`)
	reg, _ := LoadTokenRegistry()

	_, err := reg.Lookup("DOGE")
	if err == nil {
		t.Fatal("Lookup returned nil error for unknown symbol")
	}
	if !errors.Is(err, ErrAssetNotFound) {
		t.Errorf("Lookup err=%v, want it to wrap ErrAssetNotFound", err)
	}
	// HBAR is always available even when the registry has no entry for it,
	// so the suggestion list must mention it alongside any registry symbols.
	if !strings.Contains(err.Error(), "HBAR") {
		t.Errorf("not-found message %q does not mention HBAR as a known asset", err.Error())
	}
	if !strings.Contains(err.Error(), "USDC") {
		t.Errorf("not-found message %q does not include the registry's known symbols", err.Error())
	}
}

func TestTokenRegistry_Lookup_EmptyRegistry_StillSuggestsHBAR(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HIERO_PAY_TOKENS", filepath.Join(dir, "missing.json"))
	reg, _ := LoadTokenRegistry()

	_, err := reg.Lookup("DOGE")
	if err == nil {
		t.Fatal("Lookup returned nil error for unknown symbol in empty registry")
	}
	if !strings.Contains(err.Error(), "HBAR") {
		t.Errorf("empty-registry not-found message %q does not mention HBAR", err.Error())
	}
}
