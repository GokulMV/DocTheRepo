"""HTTP API (FastAPI)."""

from fastapi import FastAPI, HTTPException

from .inventory import OutOfStock, reserve_stock
from .search import search_products

app = FastAPI()


@app.get("/products")
def list_products(q: str, limit: int = 20):
    """GET /products?q=... searches the catalog."""
    return search_products(app.state.db, q, limit)


@app.post("/products/{product_id}/reserve")
def reserve(product_id: str, quantity: int, cart_id: str):
    """POST /products/{id}/reserve reserves stock for a cart; 409 when out of stock."""
    try:
        reserve_stock(app.state.db, product_id, quantity, cart_id)
    except OutOfStock:
        raise HTTPException(status_code=409, detail="out of stock")
    return {"reserved": quantity}
