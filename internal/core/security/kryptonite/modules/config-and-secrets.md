---
name: config-and-secrets
description: "Probes 12-factor config discipline, env drift, secret leakage across code/logs/bundles/history, default credentials, and over-broad IAM."
applies-to: [web, api, mobile, cli, library, language:any, framework:any]
---

# Config and Secrets

## Purpose

Surface configuration that is hardcoded instead of externalized, environments that have silently drifted apart, secrets that leak into places an attacker can read them, and credentials/permissions broader than the app actually needs — any of which turns a config mistake into a breach.

## Applies-to (detail)

Targets any app that reads configuration or holds credentials at all: web frontends (client-bundle leakage), APIs and backend services (env vars, connection strings, IAM roles), mobile apps (bundled API keys, embedded certs), CLIs (dotfiles, shell history, flags), and libraries (default constructor secrets, example code leaking into consumers). Applies regardless of language or framework — probes adapt to whatever config-loading mechanism (env vars, `.env` files, config server, secrets manager, hardcoded constants) the surface map records. Does not apply to a pure static-content site with zero credentials, zero third-party integrations, and no deploy-time configuration (note the skip reason explicitly rather than silently matching).

## Probes

- **Hardcoded config vs 12-factor**: grep source for literal values that should be env-driven — DB hosts/ports, API base URLs, feature-flag booleans, timeout/retry constants — and check whether the same binary/image can run against dev/staging/prod purely via env var changes, or whether a rebuild is required.
- **Env drift across environments**: diff the env var/config-key set actually consumed by the code against what's documented (`.env.example`, `README`, IaC templates) and against what's set in each environment (dev/staging/prod deploy configs, CI secrets); flag keys present in prod but undocumented, and keys documented but unset anywhere (silently falling back to an insecure default).
- **Secrets in code**: grep the current tree and full git history (`git log -p`, `trufflehog`/`gitleaks`-style entropy + pattern scan) for API keys, DB connection strings with embedded passwords, private key blocks (`-----BEGIN ... PRIVATE KEY-----`), OAuth client secrets, and cloud access-key patterns (`AKIA[0-9A-Z]{16}`, `sk-`, `ghp_`, `xox[baprs]-`).
- **Secrets in logs**: trigger error paths, login flows, and payment/PII-bearing requests, then inspect application logs for request bodies, headers (`Authorization`, `Cookie`, `X-Api-Key`), or full stack traces that echo passwords, tokens, or connection strings verbatim.
- **Secrets in error pages**: force a 500 (malformed input, dependency timeout, unhandled exception) in a non-debug/production-mode build and check whether the response body includes a stack trace, file paths, env var dumps, or a framework debug console (Django `DEBUG=True` page, Rails `config.consider_all_requests_local`, Flask Werkzeug debugger).
- **Secrets in client bundles**: search built JS/CSS bundles, source maps, and mobile app binaries (`strings`, decompiled APK/IPA resources) for API keys, backend-only secrets, or internal hostnames that should never ship client-side; check whether `NEXT_PUBLIC_*`/`VITE_*`/`REACT_APP_*`-style public-prefix env vars accidentally carry server-only secrets.
- **Default/weak credentials**: enumerate every credential the app ships or seeds (admin seed user, DB default user/password, message-queue/cache default auth, example `.env` values committed as real) and attempt login/connect with each unmodified; check for credentials still matching framework scaffolding defaults (Django `SECRET_KEY` placeholder, Rails `secret_key_base` sample, default Grafana `admin/admin`).
- **Over-broad IAM/permissions**: read the deployed service's IAM role/policy, DB user grants, and filesystem/container permissions; flag wildcard resource/action grants (`"Action": "*"`, `"Resource": "*"`), a DB user with `GRANT ALL` when the app only needs CRUD on its own tables, and a service account with cross-service reach (e.g., a web app's role that can also read unrelated S3 buckets or invoke unrelated Lambdas).

## What "safe" looks like

Every environment-specific value is read from config (env var, mounted secret, config-service key) at runtime, never compiled into source — the same artifact deploys to any environment via config alone. The full set of consumed config keys is documented and every environment sets exactly that set, with no undocumented prod-only keys and no documented-but-unset keys silently degrading to an insecure default. No secret pattern matches anywhere in current source or git history. Logs and error responses in production mode never contain raw credentials, tokens, full stack traces, or env var dumps — errors return a generic message with a correlation ID, detail goes only to an internal log sink. Client bundles and mobile binaries contain zero backend-only secrets. Every shipped default credential has been rotated or is rejected at first-boot (forces a change). IAM/DB grants are scoped to exactly the resources and actions the app's own code paths require — no wildcard grants, no cross-service reach beyond documented need.

## Severity rubric

- `critical`: a live, unrotated production secret (DB password, cloud access key, payment-provider key) readable from a public-reachable surface (client bundle, public repo, unauthenticated error page) or a default admin credential that still works against production.
- `high`: a secret found in git history or an internal log that is not public-reachable today but is exploitable by anyone with repo/log access; an IAM role or DB grant broad enough to reach unrelated sensitive resources; production `DEBUG`/stack-trace mode left on.
- `medium`: env drift causing a documented key to silently fall back to an insecure default in one environment; a secret embedded in code but in a low-exposure internal tool with restricted access; overly broad grants scoped to non-sensitive resources.
- `low`: hardcoded non-secret config (a URL, a timeout) that should be externalized but carries no confidentiality risk; verbose (but secret-free) error detail exposed to users.
- `info`: missing `.env.example` documentation for a non-sensitive key; a credential rotation policy that is undocumented but currently followed.

## Detection method

For hardcoded config, detection is a literal environment-specific value found via grep in source that has no corresponding env var read. For env drift, detection is a diff between the documented/`.env.example` key set, the code's actual `os.getenv`/`process.env`/config-loader call sites, and each environment's set deploy configuration — any asymmetry is a fail. For secrets, detection is a regex/entropy match (API-key shape, PEM header, high-entropy string near a `password=`/`key=`/`secret=` token) found in source, git history, logs, error output, or a built bundle — the match itself, redacted to first/last 4 chars, is the evidence. For default credentials, detection is a successful authenticated connection/login using the unmodified shipped value. For IAM, detection is a policy document containing a wildcard `Action`/`Resource`, or a DB `SHOW GRANTS`/`information_schema` query returning privileges beyond the app's actual query surface. Evidence for the Finding is the file:line or log excerpt (with the secret redacted), the diff of expected vs actual env keys, or the policy JSON snippet proving over-grant.

## References

Skills: `secrets-management`, `security-audit`, `dependency-management-deps-audit` (for env-consuming manifests). Standards: The Twelve-Factor App (Config), OWASP ASVS (Configuration, Secrets Management), CIS Benchmarks (least-privilege IAM sections). Tools referenced: `gitleaks`/`trufflehog`-style secret scanning, cloud IAM policy simulators.
