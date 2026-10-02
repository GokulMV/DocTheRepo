# Security policy

## Supported versions

Security fixes go to the latest release and the `main` branch. Please run the latest version.

## Reporting a vulnerability

**Please do not open a public issue for security problems.**

Report them privately through GitHub:
[**Report a vulnerability**](https://github.com/GokulMV/DocTheRepo/security/advisories/new) (Security →
Advisories → Report a vulnerability). Please include:

- what is affected (version, component, configuration),
- steps to reproduce, or a proof of concept,
- the impact as you understand it.

You will get an acknowledgement within 3 working days and an assessment within 10. We will keep you updated
while we fix it, agree a disclosure date with you, and credit you in the advisory unless you prefer
otherwise.

## Scope

In scope:
- The Hub (`dth-hub`), the CLI (`dth`), the web UI, the container image, and the deployment templates in
  `deploy/`.
- Especially: authentication and access control, the secret handling described in
  [docs/security.md](docs/security.md), webhook verification, and anything that could leak repository
  content to someone without access.

Out of scope:
- Vulnerabilities in your own LLM providers, git hosts or monitoring tools.
- Findings that need an already-compromised Hub host or key-encryption key.
- Missing hardening headers that have no demonstrated impact.

## How the project stays secure

CI runs govulncheck on the shipped binaries, `npm audit`, and a Trivy image scan, plus a weekly scheduled
scan. `make sbom` produces CycloneDX SBOMs. The security model is documented in
[docs/security.md](docs/security.md).
