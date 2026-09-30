// Package tax computes VAT.
package tax

// Rates are standard VAT rates by country.
var Rates = map[string]float64{"DE": 0.19, "FR": 0.20, "NL": 0.21, "IE": 0.23}

// CalculateVAT returns the VAT in minor units. EU business customers with a valid VAT ID in another member
// state pay no VAT (reverse charge); consumers pay their country's standard rate.
func CalculateVAT(netMinor int64, country string, business, validVATID bool) int64 {
	if business && validVATID {
		return 0 // reverse charge
	}
	return int64(float64(netMinor) * Rates[country])
}
