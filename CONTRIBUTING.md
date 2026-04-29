# Contributing

Thanks for your interest. This is a small reference project, but PRs are
welcome — especially for bug fixes, the open roadmap items, or docs.

## Development setup

```sh
git clone https://github.com/ivostoynovski/hiero-pay
cd hiero-pay
cp .env.sample .env
# Fill in .env with your testnet operator credentials.
source .env
go build .
```

See the [README](README.md) for full setup details.

## Running checks before opening a PR

```sh
go vet ./...
go test ./...
go build ./...
```

GitHub Actions runs the same three commands on every PR.

End-to-end tests (real Hedera transfers) are intentionally not in CI — they
need testnet credentials. If your change touches the SDK call path, please
test manually against testnet and mention it in the PR description.

## Pull request shape

- Use Conventional Commits style for the PR title (it becomes the squash
  commit message): `feat:`, `fix:`, `refactor:`, `chore:`, `docs:`.
- One concern per PR. Two unrelated bug fixes? Two PRs.
- Describe the change and the motivation in the PR body. If it changes the
  CLI's input or output schema, note that explicitly — `SKILL.md` may need
  updating to match.
- Don't commit `.env`, real keys, or any file containing testnet/mainnet
  credentials. The repo has gitleaks-clean history; let's keep it that way.

## Project scope

In scope:

- USDC and other HTS token transfers
- HCS audit logging
- Claude Code skill / agent integration
- Mainnet hardening (return-bytes mode, etc.)
- Other reasonable features that align with "AI-driven payments on Hedera"

Out of scope:

- Non-Hedera chains
- HBAR-only transfers (use `hiero-sdk-go` directly)
- Custodial wallet features
- Forks of the SDK itself

If you're not sure, open an issue first to discuss before investing time.

## License

By contributing, you agree your contribution is licensed under the
[Apache License 2.0](LICENSE).
