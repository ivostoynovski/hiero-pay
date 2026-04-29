# hiero-pay

A reference Go implementation showing how to build a Claude Code agent skill on
top of [`hiero-sdk-go`](https://github.com/hiero-ledger/hiero-sdk-go), using
stablecoin payments + an HCS audit log as the demo.

> Status: v1.5. Testnet only. Not production-ready.

## What it does

Reads a JSON payment request, executes a USDC transfer on Hedera, returns the
transaction ID. That's it. JSON in → USDC moves → result out.

## v1 user flow

1. You provide a JSON payment request via stdin or `--file`.
2. The CLI validates the JSON.
3. The CLI executes the USDC transfer using your operator credentials.
4. (Optional) The CLI submits an audit message to an HCS topic.
5. The CLI returns transaction ID + audit result as JSON, or a clear error.

## Verifiable audit log

When `AUDIT_TOPIC_ID` is set in `.env`, every successful payment also writes a
JSON message to a Hedera Consensus Service topic. The topic is publicly
readable, ordered, and tamper-proof — an external auditor can read the full
payment history with one URL, and the network's consensus timestamps make
backdating impossible.

Bootstrap your own audit topic:

```sh
go run ./scripts/create-audit-topic
# prints a topic ID; paste it into AUDIT_TOPIC_ID in .env
```

The topic's submit key is your operator, so only this binary can write to it.
Reads are unrestricted (that's the design).

If `AUDIT_TOPIC_ID` is empty, audit logging is silently skipped.

## Setup

```sh
cp .env.sample .env
# edit .env with your testnet OPERATOR_ID and OPERATOR_KEY
source .env

go build .
./hiero-pay --help
```

Get free testnet credentials from <https://portal.hedera.com>.

## Roadmap

- [x] v1: USDC transfer on testnet, JSON in/out
- [x] HCS audit log (every payment recorded to a topic)
- [x] Claude Code skill (`SKILL.md`) wiring
- [ ] Multi-token (not just USDC)
- [ ] Mainnet hardening (small balances, return-bytes mode for confirm-before-sign)

## License

Apache 2.0
