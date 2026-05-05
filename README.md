# hiero-pay

A Go CLI that lets a Claude Code agent send Hedera-network payments — USDC,
HBAR, or any HTS token configured locally — with each payment recorded to a
local SQLite history database and (optionally) to a tamper-proof HCS audit
log. Built as a reference implementation of
[`hiero-sdk-go`](https://github.com/hiero-ledger/hiero-sdk-go).

> Status: v1.6. Testnet-first. Not production-ready.

## ⚠️ This binary signs transactions autonomously

When invoked, `hiero-pay` reads a JSON request, signs the transfer with the
operator key from your `.env`, and submits it to Hedera. **There is no
confirmation prompt.** That's the whole point — an LLM (or any caller) can run
it and have the transfer happen end-to-end.

That design has consequences:

- A bug in the LLM's parsing, a prompt-injection on its conversation, or a
  typo in your JSON can move funds unintendedly.
- The operator key in `.env` is hot — present in your shell, used on every
  call.
- Testnet-first. Use it on mainnet only with a dedicated account whose
  balance you're willing to lose. There is no "return-bytes /
  confirm-before-sign" mode yet; that's planned (see roadmap).

If you are not comfortable with autonomous signing, do not use this on
mainnet.

## What it does

Reads a JSON payment request, executes a transfer on Hedera, persists the
result locally, and returns the transaction ID. JSON in → funds move →
result out, plus a queryable history database.

## User flow

1. You (or your AI agent) provide a JSON payment request via stdin or
   `--file`.
2. The CLI validates the JSON and (when `recipient` is used) resolves the
   contact name to an account ID via the local address book.
3. The CLI executes the transfer using your operator credentials.
4. The CLI records the payment to the local SQLite history database.
5. (Optional) The CLI submits an audit message to an HCS topic.
6. The CLI returns transaction ID + DB / audit results as JSON, or a clear
   error.

Past payments can be queried via `hiero-pay history` without re-running any
network operations.

## Prerequisites

- Go 1.25.7 or later
- macOS or Linux (Windows untested; should work via WSL2)
- A Hedera testnet account from <https://portal.hedera.com> with HBAR for
  fees and an ECDSA secp256k1 private key
- (For HTS payments) the recipient's account associated with the token

## First-time setup

```sh
git clone https://github.com/ivostoynovski/hiero-pay
cd hiero-pay

# 1. Configure operator credentials
cp .env.sample .env
# Edit .env: OPERATOR_ID, OPERATOR_KEY (paste from Hedera portal),
#           HEDERA_NETWORK (= testnet).

# 2. (Optional) Bootstrap an HCS audit topic
source .env
go run ./scripts/create-audit-topic
# Copy the printed topic ID into AUDIT_TOPIC_ID in .env, then re-source.

# 3. Build
source .env
go build .

# 4. Sanity-test
./hiero-pay --help
```

## Local configuration files

`hiero-pay` reads three optional JSON files from the working directory.
Override their paths via env vars listed below.

### `tokens.json` — token registry (committed, edit for mainnet)

Maps asset symbols → token IDs and decimal precision. Ships with Circle's
testnet USDC pre-configured. HBAR is built-in and must NOT appear here.

```jsonc
{
  "USDC": { "tokenId": "0.0.429274", "decimals": 6 }
}
```

For mainnet, edit `tokens.json` (Circle's mainnet USDC is `0.0.456858`) or
override the path via `HIERO_PAY_TOKENS=/path/to/your/tokens.json`.

### `contacts.json` — address book (gitignored, operator-private)

Maps contact names → account IDs so requests can use `"recipient": "alice"`
instead of `"recipientAccountId": "0.0.1234"`. Missing file is fine — the
by-id path keeps working without it.

```jsonc
[
  { "name": "alice",       "accountId": "0.0.1234" },
  { "name": "vendor-acme", "accountId": "0.0.9999" }
]
```

Override the path via `HIERO_PAY_CONTACTS=/path/to/your/contacts.json`.

### `history.db` — SQLite payment log (gitignored, operator-private)

Created automatically on first run. Holds every successful payment. Query
via `hiero-pay history` (see below). Override the path via
`HIERO_PAY_DB=/path/to/your/history.db`.

**DB placement:** keep `history.db` outside cloud-sync directories (iCloud,
Dropbox, OneDrive). Cloud sync clients can replace files mid-write or
restore them without their WAL/SHM sidecars, leading to silent corruption.
For single-writer single-machine use the risk is low but real — restoring
from cloud sync is the realistic failure mode.

## Sending a payment

```sh
echo '{"recipient":"alice","asset":"USDC","amount":"1.5","memo":"vendor invoice"}' \
  | ./hiero-pay
```

Request schema:

| Field                | Required                                   | Notes                                                                            |
| -------------------- | ------------------------------------------ | -------------------------------------------------------------------------------- |
| `recipient`          | One of `recipient` / `recipientAccountId`  | Contact name from `contacts.json`. Resolved before signing.                      |
| `recipientAccountId` | One of `recipient` / `recipientAccountId`  | Direct Hedera account ID (`0.0.X`).                                              |
| `asset`              | Optional                                   | Asset symbol. Defaults to `"USDC"`. `"HBAR"` is built-in. Anything else: `tokens.json`. |
| `amount`             | Required                                   | JSON **string**, > 0, ≤ asset's decimal precision. e.g. `"1.5"`. JSON numbers rejected. |
| `memo`               | Optional                                   | ≤ 100 bytes. Useful for tracing.                                                 |

A per-call cap (default 10,000 in canonical units of whichever asset) is
enforced. Raise it via `MAX_PAYMENT_AMOUNT` in `.env` if needed.

Via Claude Code: open this directory in Claude Code and the skill at
`.claude/skills/hiero-pay/SKILL.md` is auto-discovered. Ask in plain
English: *"pay 1.5 USDC to alice"* / *"send 0.5 HBAR to 0.0.5678"*.

## Querying payment history

```sh
./hiero-pay history --asset USDC --since 2026-05-01T00:00:00Z --format table
```

Filters: `--since`, `--until` (RFC3339 timestamps), `--asset`,
`--recipient` (matches account ID OR contact name, case-insensitive on
name), `--status`, `--limit` (default 50). Output: `--format=json`
(default, machine-readable) or `--format=table` (human-readable).

Pure read — never constructs the signer, can't move funds.

## Output schema

Success (stdout):

```jsonc
{
  "transactionId": "0.0.X@...",
  "status": "SUCCESS",
  "dbStatus": "SUCCESS",          // SUCCESS | SKIPPED | FAILED
  "auditStatus": "SUCCESS",       // SUCCESS | SKIPPED | FAILED
  "auditMessage": {
    "topicId": "0.0.X",
    "transactionId": "0.0.X@...",
    "sequenceNumber": 42
  }
}
```

A `dbStatus` or `auditStatus` of `FAILED` means the corresponding side
effect didn't land — but the payment itself is on-chain and `status =
SUCCESS`. Payment integrity > log completeness.

Failure (stderr, exit code 1):

```jsonc
{
  "code": "INVALID_INPUT|CONFIG_MISSING|AUTH_ERROR|TRANSFER_FAILED|CONTACT_NOT_FOUND",
  "error": "<human-readable reason>"
}
```

## Verifiable audit log

When `AUDIT_TOPIC_ID` is set, every successful payment also writes a JSON
message to a Hedera Consensus Service topic. The topic is publicly readable,
ordered, and tamper-proof — an external auditor can read the full payment
history with one URL, and the network's consensus timestamps make backdating
impossible.

The topic's submit key is your operator, so only this binary can write to
it. Reads are unrestricted.

If `AUDIT_TOPIC_ID` is empty, audit logging is silently skipped and
`auditStatus` reports `SKIPPED`.

## Roadmap

- [x] USDC transfer on testnet, JSON in/out
- [x] HCS audit log (every payment recorded to a topic)
- [x] Claude Code skill (`SKILL.md`) wiring
- [x] Address book (pay by name)
- [x] Multi-currency: HBAR + configurable HTS tokens
- [x] Local SQLite payment history + `hiero-pay history` subcommand
- [ ] Reconciliation against mirror node (`hiero-pay reconcile`)
- [ ] `TokenInfoQuery` runtime decimals fallback for unconfigured tokens
- [ ] Mainnet hardening (small balances, return-bytes mode for
      confirm-before-sign)

## License

[Apache 2.0](LICENSE)
