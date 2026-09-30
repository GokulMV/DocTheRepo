# ADR 0001: PostgreSQL full-text search instead of Elasticsearch

## Status

Accepted

## Context

The catalog has about 200,000 products. Running an Elasticsearch cluster for that size costs more than it
saves.

## Decision

Use PostgreSQL full-text search with a generated tsvector column and pg_trgm for fuzzy matching.

## Consequences

One database to operate. Revisit if the catalog passes five million products.
