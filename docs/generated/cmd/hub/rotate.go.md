<!-- dth:generated source="cmd/hub/rotate.go" — edit only inside dth:human blocks -->
# `cmd/hub/rotate.go`

<!-- dth:chunk 1a1a6f577d9119da -->
## `rotateKey`

Implements the `dth-hub rotate-key` command to re-wrap all stored secrets' data keys under a new key-encryption key in a single transaction, without decrypting the secrets themselves. Also supports migrating deployments between key providers (local file, AWS KMS, or Google Cloud KMS). Accepts one of `--to-key-file`, `--to-awskms`, `--to-gcpkms`, or `DTH_NEW_LOCAL_KEY` environment variable to specify the destination key, and `--dry-run` to preview changes. Loads configuration from the given path, opens both current and new KEKs, re-wraps all sealed columns, and outputs a summary of moved secrets and next steps for rotating the live deployment.

<!-- dth:chunk 06bc682a90fc1025 -->
## `rewrapAll`

Re-wraps every sealed column's encrypted data from the current KEK to a new one within a single database transaction. For each sealed column, reads all non-null encrypted blobs with row locks, calls `secrets.Rewrap` to re-encrypt under the new key (skipping unchanged entries), and updates rows if not a dry run. Returns a map counting moved secrets per category; `changed` from `Rewrap` indicates whether a blob actually needed re-encryption under the new key.

<!-- dth:chunk 5e169ea6218b46f9 -->
## `rotateKeyMain`

Entry point for the `rotate-key` subcommand; calls `rotateKey` with command-line arguments, the DTH_CONFIG environment variable or default `dth.yaml`, and stdout, then exits with code 0 on success or 1 on error.

<!-- dth:chunk b4fe55059584ead3 -->
## `__module__`

Metadata table defining which database columns contain sealed (encrypted) data across five tables: model provider keys, connector credentials, webhook secrets, sealing keys, and single sign-on client secrets. Used by `rewrapAll` to systematically iterate through all encrypted values during key rotation.
