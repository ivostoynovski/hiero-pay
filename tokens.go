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

// AssetKind identifies whether a transfer moves HBAR (the network's native
// currency) or an HTS token. The Signer adapter branches on this to pick
// between AddHbarTransfer and AddTokenTransfer.
type AssetKind string

const (
	AssetKindHBAR AssetKind = "HBAR"
	AssetKindHTS  AssetKind = "HTS"
)

const (
	hbarSymbol   = "HBAR"
	hbarDecimals = int32(8)
)

// TokenEntry is one HTS token registered for use by symbol.
type TokenEntry struct {
	TokenID  string `json:"tokenId"`
	Decimals int32  `json:"decimals"`
}

var tokenSymbolPattern = regexp.MustCompile(`^[A-Z0-9_-]+$`)

// TokenRegistry maps asset symbols to their token IDs and decimal precision.
// HBAR is recognised without a registry entry and must not be configured here.
type TokenRegistry struct {
	bySymbol map[string]TokenEntry
}

// ErrAssetNotFound is returned by Lookup when the symbol is not HBAR and has
// no entry in the registry. Wrapped with a "known: ..." suggestion list.
var ErrAssetNotFound = errors.New("asset not found")

// LoadTokenRegistry reads the registry from $HIERO_PAY_TOKENS or, if unset,
// from ./tokens.json in the current working directory. A missing file
// returns an empty registry without error — HBAR-only deployments need no
// configuration.
func LoadTokenRegistry() (*TokenRegistry, error) {
	path := os.Getenv("HIERO_PAY_TOKENS")
	if path == "" {
		path = "tokens.json"
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &TokenRegistry{bySymbol: map[string]TokenEntry{}}, nil
		}
		return nil, fmt.Errorf("read tokens file %q: %w", path, err)
	}

	var raw map[string]TokenEntry
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("decode tokens file %q: %w", path, err)
	}

	reg := &TokenRegistry{bySymbol: make(map[string]TokenEntry, len(raw))}
	for symbol, entry := range raw {
		s := strings.TrimSpace(symbol)
		if !tokenSymbolPattern.MatchString(s) {
			return nil, fmt.Errorf("tokens file %q: symbol %q has invalid characters (allowed: A-Z, 0-9, _, -)", path, symbol)
		}
		if s == hbarSymbol {
			return nil, fmt.Errorf("tokens file %q: symbol %q is reserved (HBAR is built-in and must not appear in the registry)", path, symbol)
		}
		if !accountIDPattern.MatchString(entry.TokenID) {
			return nil, fmt.Errorf("tokens file %q symbol %q: tokenId %q is not a valid Hedera token ID", path, symbol, entry.TokenID)
		}
		if entry.Decimals < 0 || entry.Decimals > 18 {
			return nil, fmt.Errorf("tokens file %q symbol %q: decimals %d out of range (must be 0..18)", path, symbol, entry.Decimals)
		}
		reg.bySymbol[s] = entry
	}
	return reg, nil
}

// ResolvedAsset is the metadata downstream code needs after a request's
// asset symbol has been resolved against the registry (or recognised as
// HBAR).
type ResolvedAsset struct {
	Kind     AssetKind
	Symbol   string
	TokenID  string // empty for HBAR
	Decimals int32
}

// Lookup returns asset metadata for the given symbol. HBAR is recognised
// without a registry entry. Lookup is case-sensitive: token symbols have
// canonical capitalisation by convention.
func (r *TokenRegistry) Lookup(symbol string) (ResolvedAsset, error) {
	if symbol == hbarSymbol {
		return ResolvedAsset{
			Kind:     AssetKindHBAR,
			Symbol:   hbarSymbol,
			Decimals: hbarDecimals,
		}, nil
	}
	if entry, ok := r.bySymbol[symbol]; ok {
		return ResolvedAsset{
			Kind:     AssetKindHTS,
			Symbol:   symbol,
			TokenID:  entry.TokenID,
			Decimals: entry.Decimals,
		}, nil
	}
	return ResolvedAsset{}, fmt.Errorf("%w: %q (known: HBAR, %s)", ErrAssetNotFound, symbol, r.symbolList())
}

func (r *TokenRegistry) symbolList() string {
	if len(r.bySymbol) == 0 {
		return "registry is empty"
	}
	syms := make([]string, 0, len(r.bySymbol))
	for s := range r.bySymbol {
		syms = append(syms, s)
	}
	sort.Strings(syms)
	return strings.Join(syms, ", ")
}
