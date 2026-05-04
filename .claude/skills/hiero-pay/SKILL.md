---
name: hiero-pay
description: Send USDC payments on the Hedera network. Use this skill when the user explicitly asks to pay, transfer, or send USDC to a Hedera account (format 0.0.X). Wraps a local Go CLI that signs and submits the transfer using the user's operator credentials from .env. Do NOT use for HBAR transfers or non-Hedera chains.
---

# hiero-pay

Send USDC on Hedera by constructing a JSON payment request and piping it to the
local `hiero-pay` binary.

## When to invoke

User says things like:
- "pay 1 USDC to 0.0.5678"
- "send Alice 5 USDC" (only if you can resolve Alice → account ID; otherwise ask)
- "transfer 0.50 USDC to 0.0.9999"

Do **not** invoke for:
- HBAR transfers (the binary is USDC-only in v1)
- EVM addresses without a 0.0.X mapping (binary expects 0.0.X)
- Multi-recipient transfers (single recipient only)

## Pre-flight

The binary requires these env vars to be set in the user's shell:
`OPERATOR_ID`, `OPERATOR_KEY`, `HEDERA_NETWORK`, `USDC_TOKEN_ID`.

These come from `~/Projects/hiero-pay/.env`. If the user hasn't sourced `.env`,
the binary will exit with `CONFIG_MISSING`. In that case, tell the user to:

```sh
cd ~/Projects/hiero-pay && source .env
```

…then retry.

## How to invoke

1. Build the request JSON. Required fields:
   - `recipientAccountId` (string, format `0.0.X`)
   - `amount` (JSON **string**, USDC, > 0, up to 6 decimals — e.g. `"1.5"`.
     Bare JSON numbers like `1.5` are rejected at decode time; always quote
     the value.)
   - `memo` (optional, ≤ 100 bytes — useful for tracing)

   The binary enforces a per-call cap (default 10,000 USDC, configurable via
   `MAX_PAYMENT_AMOUNT` in `.env`). If a request exceeds it, the binary
   returns `INVALID_INPUT`. Don't attempt to bypass the cap — surface the
   error to the user and ask them to raise it explicitly if intended.

2. Pipe it to the binary:

   ```sh
   echo '<JSON>' | ~/Projects/hiero-pay/hiero-pay
   ```

3. Read the result:
   - **Success:** stdout contains JSON like:
     ```json
     {
       "transactionId": "...",
       "status": "SUCCESS",
       "auditStatus": "SUCCESS",
       "auditMessage": {"topicId":"0.0.X","transactionId":"...","sequenceNumber":N}
     }
     ```
     Report the transaction ID with a HashScan transaction link, and (when
     `auditStatus = SUCCESS`) also include the audit topic + sequence number
     with a link to the topic message:
     `https://hashscan.io/<network>/topic/<topicId>/message/<sequenceNumber>`
   - **Audit semantics** (when `status` is SUCCESS, the payment moved regardless):
     - `auditStatus: "SUCCESS"` — both payment and audit log entry succeeded.
     - `auditStatus: "SKIPPED"` — `AUDIT_TOPIC_ID` is unset; no audit was attempted.
       Mention this briefly so the user knows their payment was not logged on
       HCS. Do not treat it as an error.
     - `auditStatus: "FAILED"` — payment succeeded but the audit submission
       errored. The `auditError` field has the reason. Surface it as a warning
       — the payment is real but the on-chain log entry is missing.
   - **Failure:** stderr contains JSON `{"code":"...","error":"..."}`.
     Surface the `error` message to the user; reference the `code` so they know
     the failure category. Common codes:
     - `INVALID_INPUT` — JSON malformed or validation failed
     - `CONFIG_MISSING` — env vars not loaded; ask user to `source .env`
     - `AUTH_ERROR` — operator credentials rejected by SDK
     - `TRANSFER_FAILED` — network or transaction error (read `error` for detail)

## Examples

### Simple payment (with audit)

User: *"pay 1 USDC to 0.0.5678"*

```sh
echo '{"recipientAccountId":"0.0.5678","amount":"1"}' | ~/Projects/hiero-pay/hiero-pay
```

Expected stdout:

```json
{"transactionId":"0.0.8812171@1714400000.123456789","status":"SUCCESS","auditStatus":"SUCCESS","auditMessage":{"topicId":"0.0.8819445","transactionId":"0.0.8812171@1714400000.987654321","sequenceNumber":42}}
```

Reply: *"Done — sent 1 USDC to 0.0.5678. Transaction: [view on HashScan](https://hashscan.io/testnet/transaction/0.0.8812171@1714400000.123456789). Audit log entry #42 on topic [0.0.8819445](https://hashscan.io/testnet/topic/0.0.8819445/message/42)."*

### Audit skipped (AUDIT_TOPIC_ID not configured)

Same payment input. Output:

```json
{"transactionId":"...","status":"SUCCESS","auditStatus":"SKIPPED"}
```

Reply: *"Done — sent 1 USDC to 0.0.5678. Transaction: [view on HashScan](...). (Audit logging is disabled — set AUDIT_TOPIC_ID in .env to enable.)"*

### Audit failed (payment still succeeded)

```json
{"transactionId":"...","status":"SUCCESS","auditStatus":"FAILED","auditError":"submit topic message: exceptional receipt status: INVALID_TOPIC_ID"}
```

Reply: *"Sent 1 USDC to 0.0.5678. ⚠️ The audit log entry failed to record (INVALID_TOPIC_ID — check AUDIT_TOPIC_ID in your .env). The payment itself is on-chain at [tx link]."*

### With memo

User: *"send 5.5 USDC to 0.0.9999, memo: invoice 42"*

```sh
echo '{"recipientAccountId":"0.0.9999","amount":"5.5","memo":"invoice 42"}' | ~/Projects/hiero-pay/hiero-pay
```

### Failure (recipient not associated with USDC)

stderr:
```json
{"code":"TRANSFER_FAILED","error":"get receipt: exceptional precheck status TOKEN_NOT_ASSOCIATED_TO_ACCOUNT"}
```

Reply: *"The recipient account 0.0.9999 isn't associated with the USDC token. They need to call `TokenAssociate` for `0.0.429274` (testnet USDC) before they can receive transfers."*

## What this skill does NOT do

- **Hold or manage keys.** The operator key lives in the user's `.env` file
  outside this skill's scope.
- **Look up names.** There's no developer / contact registry yet — if the user
  says a name instead of an account ID, ask them for the 0.0.X.
- **Confirm before sending.** v1 sends immediately on invocation. If you sense
  the request is ambiguous (e.g. user said "send Alice some USDC" without an
  amount), ask before invoking.
- **Cover mainnet without warning.** If the user's `HEDERA_NETWORK=mainnet`,
  warn them this is real money before invoking.

## Building the binary if it's missing

If `~/Projects/hiero-pay/hiero-pay` doesn't exist, build it:

```sh
cd ~/Projects/hiero-pay && go build .
```
