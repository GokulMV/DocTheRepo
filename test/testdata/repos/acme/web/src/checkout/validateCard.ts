/** Returns true when the card number passes the Luhn checksum (digits only, 12–19 long). */
export function validateCardNumber(number: string): boolean {
  const digits = number.replace(/\s+/g, '');
  if (!/^\d{12,19}$/.test(digits)) return false;
  let sum = 0;
  for (let i = 0; i < digits.length; i++) {
    let d = Number(digits[digits.length - 1 - i]);
    if (i % 2 === 1) {
      d *= 2;
      if (d > 9) d -= 9;
    }
    sum += d;
  }
  return sum % 10 === 0;
}

/** Rejects card expiry dates (MM/YY) in the past. */
export function validateExpiry(mmYY: string, now = new Date()): boolean {
  const [mm, yy] = mmYY.split('/').map(Number);
  if (!mm || mm > 12) return false;
  const end = new Date(2000 + yy, mm, 1);
  return end > now;
}
