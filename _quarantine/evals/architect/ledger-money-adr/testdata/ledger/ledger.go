package ledger

import (
	"context"
	"database/sql"
	"time"
)

// Entry is one ledger line. Amount is in the account's currency as a decimal
// number of major units, for example 12.34 USD or 1500 JPY.
type Entry struct {
	ID        int64     `json:"id"`
	AccountID int64     `json:"account_id"`
	Amount    float64   `json:"amount"`
	Currency  string    `json:"currency"` // ISO 4217 code
	PostedAt  time.Time `json:"posted_at"`
}

// Store keeps entries in the entries table (schema.sql).
type Store struct {
	DB *sql.DB
}

// Post inserts one entry.
func (s *Store) Post(ctx context.Context, e Entry) error {
	_, err := s.DB.ExecContext(ctx,
		`INSERT INTO entries (account_id, amount, currency, posted_at)
		 VALUES ($1, $2, $3, $4)`,
		e.AccountID, e.Amount, e.Currency, e.PostedAt)
	return err
}

// Balance sums an account's entries. The sum runs in DOUBLE PRECISION.
func (s *Store) Balance(ctx context.Context, accountID int64) (float64, error) {
	var sum sql.NullFloat64
	err := s.DB.QueryRowContext(ctx,
		`SELECT SUM(amount) FROM entries WHERE account_id = $1`, accountID).Scan(&sum)
	return sum.Float64, err
}
