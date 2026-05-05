# Personal rules for Claude Code

## Always confirm before publishing or committing work

Never run any of the following without first asking the user and waiting for an explicit "yes":

- `git commit` (and `git commit --amend`)
- `git push` (any form, including `--force-with-lease`)
- `gh pr create` / `gh pr merge` / `gh pr close`
- `gh issue create` / `gh issue close`
- Any other command that publishes work, sends a message, or modifies shared state.

**This rule is not satisfied by an earlier "go ahead" in the same session.** Every commit, push, and PR needs its own explicit confirmation. If the user said "open a PR" two minutes ago, that authorized that one PR — not the next one.

The only exception: the user has just explicitly asked for the action in the same turn ("commit it now", "push it", "open the PR"). In that case, run the action without re-confirming.

When in doubt, ask. The cost of asking once is trivial; the cost of an unwanted commit / push / PR is high (force-pushes, branch deletions, manual revert, lost work).

# Project context

## What this binary does

`hiero-pay` is a Go CLI that signs and submits USDC token transfers on Hedera **autonomously** — there is no per-call confirmation prompt. Every successful transfer is also written to a public HCS audit topic. v1.5, testnet-first.

Practical implication: any code change that affects the request → sign → submit path is one keystroke from moving real (testnet) USDC. Treat the signing path like a hot key. Do **not** invoke the binary against real env vars from a test or example unless the user explicitly asks.

## Layout

Single Go package at the repo root — there is no `cmd/` or `pkg/` directory yet, despite what `ADDRESS_BOOK_PLAN.md` suggests. Source files:

- `main.go` — entry point, env-var config, client setup, run() orchestration, `parseOperatorKey`, `toRawUnits`
- `request.go` — `PaymentRequest` schema, custom `UnmarshalJSON` that rejects JSON-number amounts, `Validate()`
- `audit.go` — HCS audit submission, `AuditMessage` (schema version constant `auditMessageVersion`)
- `request_test.go` — unit tests for the request schema
- `scripts/create-audit-topic/` — one-shot helper to bootstrap an HCS topic

If you add new files, default to keeping them in `package main` at the root unless the user explicitly asks for a sub-package. The address-book plan proposes `pkg/contacts/`, but it's a plan, not merged.

## Build & test

`source .env` is required before `go build` / `go test` (the binary reads operator creds from process env, and tests may too). Standard commands:

- `go build .` → produces `./hiero-pay`
- `go test ./...` → runs unit tests; safe to run without a funded account because tests don't hit the network
- `./hiero-pay --help` → sanity check
- `./hiero-pay --file examples/payment.json` or `echo '<json>' | ./hiero-pay` → real submission (testnet)

## Safety invariants — do not regress

These exist on purpose; preserve them across refactors:

1. `amount` must arrive as a JSON **string** (`"1.5"`, not `1.5`). The custom `UnmarshalJSON` enforces this — keep it.
2. Decimal precision is capped at `usdcDecimals` (6) in `Validate()`.
3. `MAX_PAYMENT_AMOUNT` (default 10 000) is a hard upper bound checked before any signing.
4. Memo capped at `maxMemoBytes` (100, Hedera's limit).
5. Audit failures **never** turn a successful transfer into an error. Payment integrity > audit completeness — the result reports `status: SUCCESS`, `auditStatus: FAILED`. Don't invert this.
6. `parseOperatorKey` tries ECDSA before Ed25519. Reason: Hedera's `PrivateKeyFromString` auto-detect silently mis-parses raw-hex ECDSA keys as Ed25519. Don't simplify back to a single-call parse.
7. Error codes (`INVALID_INPUT` / `CONFIG_MISSING` / `AUTH_ERROR` / `TRANSFER_FAILED`) are part of the public contract — they're documented in the README and consumed by the skill. Add new codes; don't rename existing ones.

## Conventions observed in this repo

- Commit messages follow Conventional Commits (`chore:`, `refactor:`, `docs:`, `fix:`, `chore(deps):`). Match that style.
- PRs are merged via squash; commit subjects show the PR number in `(#N)` form. Don't try to replicate that suffix in local commits — GitHub adds it on merge.
- Comments lean toward explaining *why*, not *what*. Existing comments are a good model — match the density and tone, don't add narration.

## Testing philosophy

Test **behaviour, not implementation detail** ([why](https://quii.gitbook.io/learn-go-with-tests/meta/why)) — tests exist to enable refactoring, and tests-on-internals make refactoring fail. Before authoring, modifying, or reviewing any test in this repo, consult `.claude/agents/test-verifier.md`; that's the operational checklist (author guidance + reviewer checks). This section just names the principle.

## Skills

Project-scoped skills live in `.claude/skills/`:

- `hiero-pay/SKILL.md` — wraps the CLI for plain-English payment requests in Claude Code
- `grill-me/SKILL.md` — sourced from `mattpocock/skills` (pinned by hash in `skills-lock.json`)

A near-duplicate `.agents/skills/` directory also exists. Don't deduplicate without asking — the two roots may serve different agents.
