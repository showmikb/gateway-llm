# Gateway-LLM Security Policy

Gateway-LLM sits on the hot path between production apps and every
major LLM provider. The security story is our moat, not a checkbox.
This document is the contract we offer operators and the process we
follow when something goes wrong.

## Supported versions

| Version          | Status                          |
| ---------------- | ------------------------------- |
| `main`           | Actively supported              |
| `v1.x`           | Security fixes for 12 months    |
| `< v1.0`         | Unsupported; please upgrade     |

## Reporting a vulnerability

**Do not open a public issue.** Email `security@gateway-llm.dev` with:

1. A clear description of the vulnerability and its impact.
2. Steps to reproduce (a minimal reproducer is deeply appreciated).
3. Your disclosure preference and any deadline.

You will receive an acknowledgement within 48 hours, a triage
assessment within 5 business days, and a fix-or-mitigation plan within
10 business days. We follow a **90-day coordinated disclosure
window**; if you need a shorter or longer window, tell us up front.

We publish a CVE for every fixed issue that affects a released
version. Reporters are credited in the release notes unless they
request otherwise.

## Supply-chain guarantees

Every tagged release produces the following artifacts:

* **Signed container images** on `ghcr.io/gateway-llm/gateway-llm`.
  Verify with `cosign verify --certificate-identity-regexp='.+' \
  --certificate-oidc-issuer='https://token.actions.githubusercontent.com' \
  ghcr.io/gateway-llm/gateway-llm:<tag>`.
* **SLSA v1 Level 3 provenance** attested via
  `slsa-github-generator`. Verify with `slsa-verifier`.
* **SBOM** in both SPDX and CycloneDX for the Go binary and the final
  container image. Attached to the GitHub release and pushed as an
  OCI referrer.
* **Deterministic builds**: the `build-from-source` Makefile target
  produces byte-identical binaries on every amd64/arm64 Linux runner.

Release pipeline: `.github/workflows/release.yml`. Keyless signing
uses GitHub OIDC → Fulcio; no long-lived secrets leave the runner.

## Trust model

Gateway-LLM is designed so **a compromised upstream provider cannot
escalate into a data-plane compromise**. Specifically:

* PII redaction runs before the provider sees the request.
* The policy engine can pin every request to a region and a provider
  allowlist.
* All billing receipts are Ed25519 signed and optionally
  hash-chained. Customers can re-verify their bills off-line with only
  the public key at `/.well-known/gateway-llm-receipts.json`.
* WASM plugins run in a capability-scoped wazero sandbox with no file,
  network, or environment access unless explicitly granted.
* The OSS core has **zero closed-source dependencies**. Enterprise
  features are gated by a compile-time `ee` tag and live in the EE
  repository.

## Secrets handling

Gateway-LLM never logs secrets. API keys, provider tokens, policy
rules, and license files are redacted from every log sink (stdout,
OpenTelemetry, Datadog, Langfuse, etc.) by the log formatter in
`internal/logging/`.

If you observe a secret in any log, consider it a security
vulnerability and report it per the process above.

## Hardening checklist for operators

* Set `GATEWAY_LLM_MASTER_KEY` in a secret manager, not in-process env.
* Run the gateway behind a TLS-terminating proxy or set
  `server.tls_cert_path` / `server.tls_key_path`.
* Enable `privacy.enabled: true` with a durable vault URL in
  production.
* Enable `receipts.enabled: true` and publish
  `/.well-known/gateway-llm-receipts.json` to your downstream
  auditors.
* Enable `policy.enabled: true` with at least a region pin for any
  residency-sensitive tenant.
* Turn on rate-limiting and set a per-key spend budget.

## Security contacts

* `security@gateway-llm.dev` — primary.
* PGP key: `openpgp4fpr:TBD` — rotating annually.
* For embargoed coordination, request a private GitHub security
  advisory.
