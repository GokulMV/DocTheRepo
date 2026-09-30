"""Discount and price rules."""

MAX_DISCOUNT = 0.40

BULK_TIERS = [(100, 0.15), (50, 0.10), (10, 0.05)]


def apply_discount(unit_price_cents: int, quantity: int, coupon_percent: float = 0.0) -> int:
    """Return the line total after bulk tier discounts and an optional coupon.

    Bulk tiers: 10+ units 5%, 50+ units 10%, 100+ units 15%. The coupon stacks on top, but the combined
    discount never exceeds MAX_DISCOUNT (40%).
    """
    bulk = next((pct for min_qty, pct in BULK_TIERS if quantity >= min_qty), 0.0)
    discount = min(bulk + coupon_percent, MAX_DISCOUNT)
    return round(unit_price_cents * quantity * (1 - discount))
