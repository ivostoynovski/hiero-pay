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
   - `amount` (number, USDC, > 0; up to 6 decimals)
   - `memo` (optional, ≤ 100 bytes — useful for tracing)

2. Pipe it to the binary:

   ```sh
   echo '<JSON>' | ~/Projects/hiero-pay/hiero-pay
   ```

3. Read the result:
   - **Success:** stdout contains JSON `{"transactionId":"...","status":"SUCCESS"}`.
     Report the transaction ID to the user with a HashScan link:
     `https://hashscan.io/<network>/transaction/<transactionId>`
   - **Failure:** stderr contains JSON `{"code":"...","error":"..."}`.
     Surface the `error` message to the user; reference the `code` so they know
     the failure category. Common codes:
     - `INVALID_INPUT` — JSON malformed or validation failed
     - `CONFIG_MISSING` — env vars not loaded; ask user to `source .env`
     - `AUTH_ERROR` — operator credentials rejected by SDK
     - `TRANSFER_FAILED` — network or transaction error (read `error` for detail)

## Examples

### Simple payment

User: *"pay 1 USDC to 0.0.5678"*

```sh
echo '{"recipientAccountId":"0.0.5678","amount":1}' | ~/Projects/hiero-pay/hiero-pay
```

Expected stdout:

```json
{"transactionId":"0.0.8812171@1714400000.123456789","status":"SUCCESS"}
```

Reply: *"Done — sent 1 USDC to 0.0.5678. Transaction: [view on HashScan](https://hashscan.io/testnet/transaction/0.0.8812171@1714400000.123456789)."*

### With memo

User: *"send 5.5 USDC to 0.0.9999, memo: invoice 42"*

```sh
echo '{"recipientAccountId":"0.0.9999","amount":5.5,"memo":"invoice 42"}' | ~/Projects/hiero-pay/hiero-pay
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
