# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/).

## [Unreleased]

## [v1.5] — 2026-04-29

### Added

- HCS audit log: every successful payment writes a JSON message to a Hedera
  Consensus Service topic when `AUDIT_TOPIC_ID` is set in `.env`. Provides
  a public, ordered, tamper-proof record of all transfers.
- `scripts/create-audit-topic` to bootstrap an audit topic with the
  operator as the submit key.
- `auditStatus` (`SUCCESS` / `SKIPPED` / `FAILED`) and `auditMessage`
  (`topicId`, `transactionId`, `sequenceNumber`) fields in the success
  response. Audit failure does **not** fail the payment — payment integrity
  trumps audit completeness.

### Changed

- `parseOperatorKey` now tries ECDSA secp256k1 first, then Ed25519, then
  the SDK's auto-detect. Fixes silent mis-parsing when the portal-issued
  ECDSA hex key was being interpreted as Ed25519.
- The decimal "rounds-to-zero" check for `amount` now returns
  `INVALID_INPUT` instead of `TRANSFER_FAILED` (it's an input issue, no
  network call is involved).
- `SKILL.md` moved from repo root to `.claude/skills/hiero-pay/SKILL.md`
  for automatic discovery by Claude Code.

## [v1] — 2026-04-29

### Added

- Initial CLI: reads a JSON payment request from stdin or `--file`,
  executes a USDC transfer on Hedera, returns transaction ID + status as
  JSON.
- Stable error codes: `INVALID_INPUT`, `CONFIG_MISSING`, `AUTH_ERROR`,
  `TRANSFER_FAILED`. Errors land on stderr as structured JSON.
- `SKILL.md` so Claude Code can drive the CLI from natural-language
  requests.
- `.env.sample` documenting required configuration.

[Unreleased]: https://github.com/ivostoynovski/hiero-pay/compare/v1.5...HEAD
[v1.5]: https://github.com/ivostoynovski/hiero-pay/compare/v1...v1.5
[v1]: https://github.com/ivostoynovski/hiero-pay/releases/tag/v1
