// Package ledger records every money movement as balanced double-entry postings.
package ledger

import "errors"

// ErrUnbalanced is returned when a transfer's debits and credits do not sum to zero.
var ErrUnbalanced = errors.New("ledger transfer is unbalanced")

// Posting moves amountMinor into (positive) or out of (negative) an account.
type Posting struct {
	Account     string
	AmountMinor int64
}

// PostTransfer writes postings atomically after checking that debits and credits balance to zero; an
// unbalanced transfer is rejected with ErrUnbalanced and nothing is written.
func PostTransfer(tx Tx, postings []Posting) error {
	var sum int64
	for _, p := range postings {
		sum += p.AmountMinor
	}
	if sum != 0 || len(postings) < 2 {
		return ErrUnbalanced
	}
	for _, p := range postings {
		if err := tx.Insert(p); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// Tx is a database transaction.
type Tx interface {
	Insert(Posting) error
	Commit() error
}
