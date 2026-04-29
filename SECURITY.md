# Security policy

## Reporting a vulnerability

If you find a security issue in `hiero-pay`, please **do not open a public
issue**. Email <ivostoynovski@gmail.com> with:

- A short description of the issue
- Steps to reproduce
- Whether the issue affects testnet, mainnet, or both
- Your suggested fix, if any

You should expect a response within 7 days. I'll work with you to confirm,
fix, and disclose the issue responsibly.

## Scope

This is a personal reference project. There is no formal SLA, no bug bounty,
and no commercial support. The project is testnet-first; mainnet behavior is
explicitly out of scope for v1 (see the README warning on autonomous signing).

That said, the following are in-scope and welcome:

- Key handling bugs (e.g., logging, leaking, mis-parsing of operator keys)
- Anything that lets a third party trigger a payment without the operator's
  intent (e.g., command injection, prompt-injection-relevant patterns in the
  skill instructions)
- Cryptographic mistakes in how the SDK is used
- Audit-log integrity issues (forged or skipped entries that should not be)

Out-of-scope:

- Issues that require an attacker to already have the operator key
- Vulnerabilities in upstream dependencies (`hiero-sdk-go`, the LLM, the
  Hedera network itself) — please report those to the respective projects
- Anything specific to running this on mainnet without the recommended
  precautions in the README
