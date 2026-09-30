// Package invoices generates monthly subscription invoices.
package invoices

import (
	"fmt"
	"time"
)

// Invoice is one customer invoice.
type Invoice struct {
	Number     string
	CustomerID string
	IssuedAt   time.Time
	TotalMinor int64
}

// GenerateInvoice issues the monthly invoice on the 1st of the month with sequential invoice numbers in
// the form INV-YYYY-NNNN (the sequence restarts every January).
func GenerateInvoice(customerID string, seq int, totalMinor int64, now time.Time) Invoice {
	issued := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	return Invoice{Number: fmt.Sprintf("INV-%d-%04d", issued.Year(), seq), CustomerID: customerID, IssuedAt: issued, TotalMinor: totalMinor}
}
