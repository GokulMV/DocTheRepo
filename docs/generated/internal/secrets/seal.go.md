<!-- dth:generated source="internal/secrets/seal.go" — edit only inside dth:human blocks -->
# `internal/secrets/seal.go`

Provides cryptographic sealing and unsealing of secrets using hybrid post-quantum encryption.

<!-- dth:chunk 689971668201e125 -->
## `__module__`

Defines constants for sealing and unsealing secrets in the Hub. `SealPrefix` identifies sealed values; `SealAlg` specifies the hybrid encryption scheme combining post-quantum ML-KEM-768 with classical X25519, deriving keys via HKDF-SHA256 and encrypting with AES-256-GCM. `sealVersion` indicates the format version. `ErrSealed` reports invalid, expired, or tampered sealed values. Purpose constants categorize sealed data by usage context: provider API keys, connector credentials/webhooks, OIDC client secrets, and MCP secrets.
