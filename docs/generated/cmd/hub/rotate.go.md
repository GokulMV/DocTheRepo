<!-- dth:generated source="cmd/hub/rotate.go" — edit only inside dth:human blocks -->
# `cmd/hub/rotate.go`

Implements database key rotation functionality for re-encrypting sealed secrets across multiple tables in the hub service.

<!-- dth:chunk b4fe55059584ead3 -->
## `__module__`

Defines the set of database tables and columns containing encrypted data that require key rotation. Each entry specifies a table name, its primary key column, the encrypted column to rotate, and a description of what data is encrypted. This configuration is used by key rotation operations to systematically re-encrypt sensitive information (provider keys, credentials, secrets, tokens) across the database when cryptographic keys are updated.
