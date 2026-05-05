---
name: hiero-pay
description: Send USDC, HBAR, or any configured HTS token on the Hedera network, and query past payments. Use this skill when the user asks to pay / transfer / send Hedera-network funds, or to look up "what did I pay" history. Wraps a local Go CLI that signs and submits the transfer using the operator credentials in their .env. Do NOT use for non-Hedera chains (Ethereum, Solana, etc.).
---

# hiero-pay

Send Hedera-network payments by piping a JSON request to the local `hiero-pay`
binary, and look up past payments via the `hiero-pay history` subcommand.

## When to invoke

User says things like:
- "pay 1 USDC to 0.0.5678"
- "send Alice 5 USDC"
- "transfer 0.5 HBAR to bob"
- "what did I pay alice last week?"
- "show me my recent payments"

Do **not** invoke for:
- Non-Hedera chains (Ethereum, Solana, etc.)
- EVM addresses without a 0.0.X mapping (binary expects 0.0.X)
- Multi-recipient transfers (single recipient only)

## Pre-flight

The binary requires `OPERATOR_ID`, `OPERATOR_KEY`, and `HEDERA_NETWORK` to be set
in the user's shell, sourced from `~/Projects/hiero-pay/.env`. If they're not
loaded, the binary exits with `CONFIG_MISSING`. Tell the user to:

```sh
cd ~/Projects/hiero-pay && source .env
```

…then retry.

## Sending a payment

### Request schema

| Field                | Required                                      | Notes                                                                                                       |
| -------------------- | --------------------------------------------- | ----------------------------------------------------------------------------------------------------------- |
| `recipient`          | One of `recipient` / `recipientAccountId`     | Contact name from the local address book (`./contacts.json`). Resolved server-side.                         |
| `recipientAccountId` | One of `recipient` / `recipientAccountId`     | Direct Hedera account ID, format `0.0.X`.                                                                   |
| `asset`              | Optional (defaults to `"USDC"`)               | Asset symbol. `"HBAR"` is built-in; other symbols must be in `./tokens.json`.                               |
| `amount`             | Required                                      | JSON **string**, > 0, up to the asset's decimal precision. e.g. `"1.5"`. Bare numbers like `1.5` are rejected. |
| `memo`               | Optional                                      | ≤ 100 bytes. Useful for tracing.                                                                            |

The binary enforces a per-call cap (default 10,000 units of whichever asset,
configurable via `MAX_PAYMENT_AMOUNT` in `.env`). If a request exceeds it, the
binary returns `INVALID_INPUT`. **Don't bypass the cap** — surface the error
and ask the user to raise it explicitly if intended.

### Invoking

```sh
echo '<JSON>' | ~/Projects/hiero-pay/hiero-pay
```

### Reading the result

**Success:**

```json
{
  "transactionId": "0.0.X@SECONDS.NANOS",
  "status": "SUCCESS",
  "dbStatus": "SUCCESS",
  "auditStatus": "SUCCESS",
  "auditMessage": {
    "topicId": "0.0.X",
    "transactionId": "0.0.X@...",
    "sequenceNumber": N
  }
}
```

Report the transaction with a HashScan link. When `auditStatus = SUCCESS`,
also include the audit topic + sequence number with a link to the topic
message: `https://hashscan.io/<network>/topic/<topicId>/message/<sequenceNumber>`.

**Per-step semantics** (when `status` is SUCCESS, the payment moved regardless):

- **`dbStatus`** — local SQLite history record:
  - `SUCCESS`: row written to the operator's `history.db`.
  - `FAILED`: payment landed but the row didn't write (`dbError` has the cause).
    Mention as a warning — the payment is real but won't appear in `hiero-pay history`.
- **`auditStatus`** — public HCS audit log:
  - `SUCCESS`: both payment and audit log entry succeeded.
  - `SKIPPED`: `AUDIT_TOPIC_ID` is unset; no audit was attempted. Mention briefly so the user knows.
  - `FAILED`: payment succeeded but the audit submission errored. `auditError` has the reason. Surface as a warning.

**Failure** (stderr, exit 1):

```json
{"code": "...", "error": "..."}
```

Surface the `error` message and reference the `code`. Common codes:

- `INVALID_INPUT` — request malformed, validation failed, or asset not configured
- `CONFIG_MISSING` — env vars not loaded; ask user to `source .env`
- `AUTH_ERROR` — operator credentials rejected by SDK
- `TRANSFER_FAILED` — network or transaction error (read `error` for detail)
- `CONTACT_NOT_FOUND` — recipient name has no entry in the address book.
  **The error message includes a "did you mean" suggestion list** — surface it
  verbatim. **Do NOT retry by guessing an account ID.** Either ask the user to
  pick one of the suggested names, or to provide a direct `recipientAccountId`.

### Examples

#### USDC by name

User: *"pay 1 USDC to alice"*

```sh
echo '{"recipient":"alice","asset":"USDC","amount":"1"}' | ~/Projects/hiero-pay/hiero-pay
```

The `asset` field can be omitted (defaults to USDC):

```sh
echo '{"recipient":"alice","amount":"1"}' | ~/Projects/hiero-pay/hiero-pay
```

#### USDC by account ID (no contacts file needed)

```sh
echo '{"recipientAccountId":"0.0.5678","amount":"1.5"}' | ~/Projects/hiero-pay/hiero-pay
```

#### HBAR

User: *"send 0.5 HBAR to bob"*

```sh
echo '{"recipient":"bob","asset":"HBAR","amount":"0.5"}' | ~/Projects/hiero-pay/hiero-pay
```

HBAR has 8 decimal places (1 HBAR = 100,000,000 tinybars). The smallest unit
is `"0.00000001"`. HBAR is recognized without a registry entry.

#### Custom HTS token

If the user has configured a token in `./tokens.json`, e.g. `"KARATE": {...}`:

```sh
echo '{"recipientAccountId":"0.0.9999","asset":"KARATE","amount":"42"}' | ~/Projects/hiero-pay/hiero-pay
```

If the symbol isn't in their registry, the binary returns `INVALID_INPUT` with
a "known: HBAR, USDC" suggestion list. Ask the user to add the token to
`tokens.json` or use a recognised symbol.

#### Failure: contact not found

```json
{"code":"CONTACT_NOT_FOUND","error":"contact not found: \"alise\" (known: alice, bob, vendor-acme)"}
```

Reply: *"There's no contact called 'alise' in your address book — did you
mean **alice**? Known contacts: alice, bob, vendor-acme. If alise is a new
recipient, add them to ~/.config/hiero-pay/contacts.json or pass the account
ID directly."*

(**Do NOT silently retry with an account ID you guessed.**)

#### Failure: recipient not associated with the token

```json
{"code":"TRANSFER_FAILED","error":"get receipt: exceptional precheck status TOKEN_NOT_ASSOCIATED_TO_ACCOUNT"}
```

Reply: *"The recipient account isn't associated with the USDC token. They
need to call `TokenAssociate` for the token before they can receive
transfers."*

## Querying payment history

Use `hiero-pay history` for "what did I pay" questions. Pure read — never
constructs the signer, can't move funds. Output is JSON by default; use
`--format=table` for human-readable output.

```sh
~/Projects/hiero-pay/hiero-pay history [flags]
```

| Flag         | What it does                                                                 |
| ------------ | ---------------------------------------------------------------------------- |
| `--since`    | RFC3339 lower bound on submitted_at (inclusive). e.g. `2026-05-01T00:00:00Z` |
| `--until`    | RFC3339 upper bound on submitted_at (exclusive)                              |
| `--asset`    | Filter by asset symbol (e.g. `USDC`, `HBAR`)                                 |
| `--recipient`| Filter by recipient. Accepts account ID OR contact name (case-insensitive)   |
| `--status`   | Filter by status (only `SUCCESS` is recorded in v1.6)                        |
| `--limit`    | Max rows. Default 50.                                                        |
| `--format`   | `json` (default) or `table`                                                  |

### Natural-language → flag mappings

| User asks                           | Flags                                                              |
| ----------------------------------- | ------------------------------------------------------------------ |
| *"what did I pay alice last week?"* | `--recipient alice --since 2026-04-28T00:00:00Z`                   |
| *"show me my HBAR payments"*        | `--asset HBAR`                                                     |
| *"how much USDC did I pay in March 2026?"* | `--asset USDC --since 2026-03-01T00:00:00Z --until 2026-04-01T00:00:00Z` |
| *"my recent payments"*              | `--limit 10 --format table`                                        |

### Reading the JSON output

`hiero-pay history --format=json` returns an array (always an array, never
`null` on empty). Each object is a payment row with `txId`, `status`,
`asset`, `tokenId` (omitted for HBAR), `amount`, `from`, `to`, `toName` (the
contact name if used, omitted otherwise), `submittedAt`, `auditStatus`, and
where applicable `auditTopicId` / `auditSeqNumber`.

## What this skill does NOT do

- **Hold or manage keys.** The operator key lives in the user's `.env` outside this skill's scope.
- **Edit the address book or tokens registry.** If the user wants a new contact or token, tell them where the file lives (`./contacts.json` / `./tokens.json` in the repo root) and what the schema is.
- **Confirm before sending.** v1 sends immediately on invocation. If the request is ambiguous (e.g. user said "send Alice some USDC" without an amount), ask before invoking.
- **Cover mainnet without warning.** If the user's `HEDERA_NETWORK=mainnet`, warn them this is real money before invoking.
- **Bypass the per-call cap.** `INVALID_INPUT` from the cap is intentional — surface the error and let the user decide whether to raise `MAX_PAYMENT_AMOUNT`.

## Building the binary if it's missing

```sh
cd ~/Projects/hiero-pay && go build .
```

The binary is a single static file with no runtime dependencies beyond the
operator's network access.
