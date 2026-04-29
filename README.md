# hiero-pay

A reference Go implementation showing how to build a Claude Code agent skill on
top of [`hiero-sdk-go`](https://github.com/hiero-ledger/hiero-sdk-go), using
stablecoin payments + (planned) HCS audit log as the demo.

> Status: v1 in progress. Testnet only. Not production-ready.

## What it does

Reads a JSON payment request, executes a USDC transfer on Hedera, returns the
transaction ID. That's it. JSON in → USDC moves → result out.

```
echo '{"developerAccountId":"0.0.5678","amountUsdc":1.5}' | hiero-pay
```

## v1 user flow

1. You provide a JSON payment request via stdin or `--file`.
2. The CLI validates the JSON.
3. The CLI executes the USDC transfer using your operator credentials.
4. The CLI returns transaction ID + status as JSON, or a clear error.

## Why this exists

The Hedera + AI agent space already has MCP servers for payments
([`hedera-mcp/hedera-mcp-server`](https://github.com/hedera-mcp/hedera-mcp-server),
[`pisuthd/hedera-mcp`](https://github.com/pisuthd/hedera-mcp)) — all in
JS/TypeScript, all wrapping the Hedera Agent Kit. There is no Go equivalent.

This repo is the Go reference: how to wire `hiero-sdk-go` into a small CLI
that an agent (or a human) can drive. Keep it minimal, keep it readable.

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

- [ ] v1: USDC transfer on testnet, JSON in/out
- [ ] HCS audit log (every payment recorded to a topic)
- [ ] Multi-token (not just USDC)
- [ ] Claude Code skill (`SKILL.md`) wiring

## License

Apache 2.0 (matching the Hiero SDK).
