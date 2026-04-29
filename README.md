# hiero-pay

A Go CLI that lets a Claude Code agent send USDC payments on Hedera, with
each payment recorded to a tamper-proof HCS audit log. Built as a reference
implementation of
[`hiero-sdk-go`](https://github.com/hiero-ledger/hiero-sdk-go).

> Status: v1.5. Testnet only. Not production-ready.

## ⚠️ This binary signs transactions autonomously

When invoked, `hiero-pay` reads a JSON request, signs a USDC transfer with the
operator key from your `.env`, and submits it to Hedera. **There is no
confirmation prompt.** That's the whole point — an LLM (or any caller) can run
it and have the transfer happen end-to-end.

That design has consequences:

- A bug in the LLM's parsing, a prompt-injection on its conversation, or a
  typo in your JSON can move USDC unintendedly.
- The operator key in `.env` is hot — present in your shell, used on every
  call.
- v1 is **testnet-first**. Use it on mainnet only with a dedicated account
  whose balance you're willing to lose. There is no "return-bytes /
  confirm-before-sign" mode in v1; that's planned (see roadmap).

If you are not comfortable with autonomous signing, do not use this on
mainnet.

## What it does

Reads a JSON payment request, executes a USDC transfer on Hedera, returns the
transaction ID. JSON in → USDC moves → result out.

## User flow

1. You (or your AI agent) provide a JSON payment request via stdin or
   `--file`.
2. The CLI validates the JSON.
3. The CLI executes the USDC transfer using your operator credentials.
4. (Optional) The CLI submits an audit message to an HCS topic.
5. The CLI returns transaction ID + audit result as JSON, or a clear error.

## Prerequisites

- Go 1.25.7 or later
- macOS or Linux (Windows untested; should work via WSL2)
- A Hedera testnet account from <https://portal.hedera.com> with HBAR for
  fees and an ECDSA secp256k1 private key
- A USDC token to transfer — either Circle's testnet USDC (token ID
  `0.0.429274`, request from <https://faucet.circle.com>) or your own
  fungible token

## First-time setup

```sh
git clone https://github.com/ivostoynovski/hiero-pay
cd hiero-pay

# 1. Configure operator credentials
cp .env.sample .env
# Edit .env: OPERATOR_ID, OPERATOR_KEY (paste from Hedera portal),
#           HEDERA_NETWORK (= testnet), USDC_TOKEN_ID (= 0.0.429274 for
#           Circle USDC on testnet, or your own token's ID).

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

## Invoking

```sh
echo '{"recipientAccountId":"0.0.5678","amount":1.5,"memo":"vendor invoice"}' \
  | ./hiero-pay
```

Or via Claude Code: open this directory in Claude Code and the
`SKILL.md` under `.claude/skills/hiero-pay/` is auto-discovered. Ask in
plain English: *"pay 1.5 USDC to 0.0.5678"*.

## Output schema

Success (stdout):

```jsonc
{
  "transactionId": "0.0.X@...",
  "status": "SUCCESS",
  "auditStatus": "SUCCESS",     // or SKIPPED / FAILED
  "auditMessage": {
    "topicId": "0.0.X",
    "transactionId": "0.0.X@...",
    "sequenceNumber": 42
  }
}
```

Failure (stderr, exit code 1):

```jsonc
{
  "code": "INVALID_INPUT|CONFIG_MISSING|AUTH_ERROR|TRANSFER_FAILED",
  "error": "<human-readable reason>"
}
```

## Verifiable audit log

When `AUDIT_TOPIC_ID` is set, every successful payment also writes a JSON
message to a Hedera Consensus Service topic. The topic is publicly readable,
ordered, and tamper-proof — an external auditor can read the full payment
history with one URL, and the network's consensus timestamps make backdating
impossible.

The topic's submit key is your operator, so only this binary can write to it.
Reads are unrestricted (that's the design — auditability without manual
permission grants).

If `AUDIT_TOPIC_ID` is empty, audit logging is silently skipped and
`auditStatus` reports `SKIPPED`.

## Troubleshooting

**`CONFIG_MISSING`** — env vars not set in the current shell. Run
`source .env`.

**`AUTH_ERROR: invalid OPERATOR_KEY`** — your private key didn't parse. Strip
any `0x` prefix isn't required (the binary handles it). Make sure you copied
the **ECDSA** private key from the portal (matching your account's key type),
not the ED25519 one.

**`TRANSFER_FAILED: ... INVALID_SIGNATURE`** — the `OPERATOR_KEY` in `.env`
signs for a different account than `OPERATOR_ID`. The portal shows pairs;
make sure both come from the same account.

**`TRANSFER_FAILED: ... TOKEN_NOT_ASSOCIATED_TO_ACCOUNT`** — the recipient
account hasn't associated with the USDC token, and isn't using
auto-associations. They need to call `TokenAssociate` for the token first.

**`TRANSFER_FAILED: ... INSUFFICIENT_TOKEN_BALANCE`** — your operator doesn't
hold enough of the configured token. Top up via the faucet or a peer
transfer.

**`TRANSFER_FAILED: ... INVALID_ACCOUNT_ID`** — the recipient account ID in
your JSON doesn't exist on the network you're using. Double-check both the
ID and `HEDERA_NETWORK`.

**Faucet didn't deliver USDC** — check that you selected **Hedera Testnet**
(not "Arc Testnet" or another option) at <https://faucet.circle.com>, and
pasted the Hedera ID (`0.0.X`), not the EVM address (`0x...`).

## Roadmap

- [x] v1: USDC transfer on testnet, JSON in/out
- [x] HCS audit log (every payment recorded to a topic)
- [x] Claude Code skill (`SKILL.md`) wiring
- [ ] Multi-token (not just USDC)
- [ ] Mainnet hardening (small balances, return-bytes mode for
      confirm-before-sign)

## License

[Apache 2.0](LICENSE)
