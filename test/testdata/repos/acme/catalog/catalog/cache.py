"""Read-through product cache."""

PRODUCT_CACHE_TTL_SECONDS = 300


def get_product(cache, db, product_id: str):
    """Return a product from the cache, loading it from the database on a miss.

    Entries live for PRODUCT_CACHE_TTL_SECONDS (five minutes).
    """
    key = f"product:{product_id}"
    hit = cache.get(key)
    if hit is not None:
        return hit
    product = db.fetchrow("SELECT * FROM products WHERE id = $1", product_id)
    cache.set(key, product, ex=PRODUCT_CACHE_TTL_SECONDS)
    return product


def invalidate_on_update(cache, product_id: str):
    """Delete the cached product after an update so readers never see a stale price."""
    cache.delete(f"product:{product_id}")
