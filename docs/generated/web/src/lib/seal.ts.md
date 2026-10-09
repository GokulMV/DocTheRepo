<!-- dth:generated source="web/src/lib/seal.ts" — edit only inside dth:human blocks -->
# `web/src/lib/seal.ts`

This module provides cryptographic sealing and unsealing functionality for encrypting secrets with different purposes using hybrid encryption (X25519 key exchange with ML-KEM768 post-quantum backup and AES-GCM encryption).

<!-- dth:chunk 367688967b5cf4f3 -->
## `Purpose`

A union type enumerating the different contexts where secrets are encrypted and stored. Each purpose identifies what type of sensitive data is being sealed: API keys for providers, connector credentials and webhook secrets, OIDC client secrets for authentication, and MCP service secrets.
