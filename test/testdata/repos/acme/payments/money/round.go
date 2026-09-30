// Package money converts and rounds currency amounts.
package money

import "math"

// RoundHalfEven rounds to the nearest integer, ties to even (banker's rounding), so rounding errors do not
// drift in one direction across many currency conversions.
func RoundHalfEven(x float64) int64 {
	return int64(math.RoundToEven(x))
}

// ToMinorUnits converts a decimal amount to minor units (cents) using the currency's exponent: JPY has 0
// decimals, USD and EUR have 2, KWD has 3.
func ToMinorUnits(amount float64, currency string) int64 {
	exp := map[string]int{"JPY": 0, "KWD": 3}
	e, ok := exp[currency]
	if !ok {
		e = 2
	}
	return RoundHalfEven(amount * math.Pow10(e))
}
