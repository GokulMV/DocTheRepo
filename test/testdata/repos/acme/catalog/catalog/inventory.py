"""Stock reservations taken at checkout."""

from datetime import timedelta

RESERVATION_TTL = timedelta(minutes=15)


def reserve_stock(db, product_id: str, quantity: int, cart_id: str):
    """Reserve stock for a cart.

    Locks the inventory row with SELECT ... FOR UPDATE so two checkouts cannot oversell the last unit.
    The reservation expires after RESERVATION_TTL (15 minutes) unless the order is paid.
    Raises OutOfStock when available quantity is too low.
    """
    with db.transaction():
        row = db.fetchrow("SELECT available FROM inventory WHERE product_id = $1 FOR UPDATE", product_id)
        if row is None or row["available"] < quantity:
            raise OutOfStock(product_id)
        db.execute("UPDATE inventory SET available = available - $2 WHERE product_id = $1", product_id, quantity)
        db.execute(
            "INSERT INTO reservations (cart_id, product_id, quantity, expires_at) VALUES ($1, $2, $3, now() + $4)",
            cart_id,
            product_id,
            quantity,
            RESERVATION_TTL,
        )


def release_expired_reservations(db):
    """Return stock held by expired reservations to inventory (runs every minute from cron)."""
    db.execute("SELECT release_expired_reservations()")


class OutOfStock(Exception):
    """Not enough stock to reserve."""
