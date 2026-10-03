<!-- dth:generated source="internal/secrets/seal.go" — edit only inside dth:human blocks -->
# `internal/secrets/seal.go`

<!-- dth:chunk 689971668201e125 -->
## `__module__`

Module-level constants and error for the hybrid post-quantum cryptographic sealing system used to encrypt secrets. `SealPrefix` identifies sealed values with version marker "dthseal1:"; `SealAlg` documents the algorithm tuple combining X25519 elliptic-curve key agreement with ML-KEM-768 post-quantum encapsulation, HKDF-SHA256 for key derivation, and AES-256-GCM for authenticated encryption. `sealVersion` tracks the format version for future compatibility. `ErrSealed` is returned when a sealed value fails validation due to malformation, key expiration, or tampering. Purpose constants distinguish different secret types (provider API keys, connector credentials, webhook secrets, OIDC client secrets) for domain-specific key derivation using HKDF.
