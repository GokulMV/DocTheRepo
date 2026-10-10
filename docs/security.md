# Security

## Sealed secrets

Provider API keys and connector credentials (git tokens, GitHub App keys and client secrets, Wiz, Splunk, Confluence and Jira
credentials, webhook secrets) are **sealed**. Only the Hub can use them, and nobody can read them back:
not through the UI, the API, `dth`, the settings export, or the audit log.

### How a secret travels

| Step | What happens |
|---|---|
| 1. In your browser (or in `dth apply`) | Before it is sent, the value is encrypted to the Hub's **sealing key**, which is hybrid: **X25519 + ML-KEM-768** (NIST FIPS 203, post-quantum). A fresh X25519 exchange and a fresh ML-KEM encapsulation produce two shared secrets. HKDF-SHA256 combines them into an AES-256-GCM key, bound to the key id and to the field (`provider.api_key`, `connector.credentials`, …). |
| 2. On the wire | TLS, plus the sealed envelope `dthseal1:…`. Whoever sees the request body (a TLS-terminating proxy, a request log, a crash dump) sees only the envelope. |
| 3. In the Hub | The envelope is opened in memory, only to encrypt the value for storage. A value sealed for another field is refused. |
| 4. At rest | Per-secret envelope encryption: a fresh AES-256-GCM data key per value, wrapped by the key-encryption key (a local key file, **AWS KMS** or **GCP KMS**). The ciphertext is bound to its row, so copying it onto another row fails to decrypt. |
| 5. Afterwards | Write-only. Lists show **Sealed ••••WXYZ · 2 days ago**: the last four characters of long keys, so you can tell which key is set, and when it was set. To change a secret, **Replace** it. |

**Why hybrid:** an attacker has to break **both** X25519 and ML-KEM-768. If a future quantum computer breaks
X25519, ML-KEM still protects traffic recorded today ("harvest now, decrypt later"). If a flaw is found in the
newer ML-KEM, X25519 still holds. At rest, AES-256 keeps about 128-bit security even against a quantum
attacker (Grover's algorithm).

**Implementations:**
- **Go:** `crypto/mlkem`, `crypto/ecdh` and `crypto/hkdf` from the standard library.
- **Browser:** the audited, dependency-free `@noble` libraries (post-quantum, curves, hashes, ciphers), in
  pure JavaScript, so sealing also works on plain-HTTP intranet addresses.
- **Interoperability:** tests check both directions. The Go side opens values the browser code sealed, and
  both derive the same ML-KEM key from one seed.

### Operating it

- **Key:** the sealing key is created on first use. Its private half is stored encrypted under the
  key-encryption key.
- **Rotation:** `POST /api/v1/seal/rotate` (owner) rotates it. Values sealed to the previous key are still
  accepted for 24 hours, for pages already open.
- **Refusing plain values:** set `DTH_REQUIRE_SEALED_SECRETS=true` (or `settings.require_sealed_secrets`)
  to reject secrets that arrive unsealed. The UI and `dth apply` always seal.

### What it does not do

No system can promise that it can never be hacked. Be clear about the limits:

- **The running Hub uses the secrets.** It has to: it calls your LLM provider and git host with them. Anyone
  who takes over the Hub process or its host while it runs can use them.
- **The key-encryption key protects everything at rest.** Whoever holds it (the key file, or permission to
  call KMS Decrypt) can decrypt the database. Keep it outside the database backups, and use KMS with
  narrowly scoped IAM in production (see the backup guide).
- **Your browser.** Sealing happens after you type the value; malware or a hostile browser extension on
  your own machine can read what you type.
- **Owners and admins** cannot read a secret, but they can replace it and choose where the Hub sends it
  (for example, a provider's base URL). Treat those roles as privileged.

## Other protections

- **Access:** OIDC single sign-on or a local owner; roles (viewer, editor, admin, owner) and per-repository
  access are enforced on every API call and in retrieval. Repositories you can't see never appear: not in
  answers, Architecture, or as a dependency.
- **Sessions:** HTTP-only, SameSite cookies, with a CSRF token on every change. Personal access tokens are
  stored hashed.
- **Webhooks:** every delivery is verified (HMAC signatures or shared secrets) and rate-limited per
  connector.
- **Never send to a model:** per connector (on by default for Wiz). Those events are grouped and matched,
  but never shown to a model.
- **Content Security Policy:** the UI allows scripts only from itself. Authored architecture diagrams run in
  a sandboxed, network-less frame.
- **Supply chain:** CI runs govulncheck (shipped binaries), `npm audit` (high and above) and Trivy (image,
  HIGH/CRITICAL), plus a weekly scan. `make sbom` writes CycloneDX SBOMs for the Go modules, the CLI and the
  web UI.
