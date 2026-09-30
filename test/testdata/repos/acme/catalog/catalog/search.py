"""Product search over PostgreSQL full-text search with a trigram fallback."""

MAX_RESULTS = 50


def search_products(db, query: str, limit: int = 20):
    """Search products by name and description.

    Uses PostgreSQL full-text search (websearch_to_tsquery) ranked by ts_rank. When full-text search
    finds nothing, falls back to pg_trgm trigram similarity so typos like "sneekers" still match.
    The limit is capped at MAX_RESULTS.
    """
    limit = min(limit, MAX_RESULTS)
    rows = db.fetch(
        "SELECT id, name FROM products WHERE search_vector @@ websearch_to_tsquery($1) "
        "ORDER BY ts_rank(search_vector, websearch_to_tsquery($1)) DESC LIMIT $2",
        query,
        limit,
    )
    if rows:
        return rows
    return db.fetch(
        "SELECT id, name FROM products WHERE similarity(name, $1) > 0.3 ORDER BY similarity(name, $1) DESC LIMIT $2",
        query,
        limit,
    )
