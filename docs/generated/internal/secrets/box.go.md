<!-- dth:generated source="internal/secrets/box.go" — edit only inside dth:human blocks -->
# `internal/secrets/box.go`

<!-- dth:chunk 78942f161f181863 -->
## `KeyIDOf`

Extracts the key ID from a sealed blob without decrypting it. Parses the blob format to read the length byte and key ID string. Returns `ErrCorrupt` if the blob is malformed (too short, invalid format byte, or incomplete key ID field).

<!-- dth:chunk e7e0f8c18258c044 -->
## `Rewrap`

Re-encrypts a sealed secret's data key from one key-encryption key to another while preserving the encrypted payload and its binding. The blob is parsed to extract the wrapped data key, which is unwrapped using `from` and re-wrapped using `to`; the secret ciphertext and format remain unchanged. Returns the original blob unchanged with `changed=false` if already sealed under the target key. Returns an error if the blob is corrupted, the key IDs don't match expected values, or key operations fail; also returns an error if the new key ID or wrapped key exceeds size limits (255 and 65535 bytes respectively).
