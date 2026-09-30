/** Formats minor units (cents) as a localized currency string with Intl.NumberFormat, e.g. 1999 USD → "$19.99". */
export function formatPrice(amountMinor: number, currency: string, locale = 'en-US'): string {
  const digits = currency === 'JPY' ? 0 : 2;
  return new Intl.NumberFormat(locale, { style: 'currency', currency, minimumFractionDigits: digits }).format(amountMinor / 10 ** digits);
}
