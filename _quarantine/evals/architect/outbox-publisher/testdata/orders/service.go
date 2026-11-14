package orders

import (
	"context"
	"database/sql"
	"log"
)

type Publisher interface {
	Publish(ctx context.Context, topic string, key string, value []byte) error
}

type Service struct {
	db  *sql.DB
	pub Publisher
}

// Place stores the order and publishes OrderPlaced after the commit. A crash
// between Commit and Publish loses the event; a retry of Place duplicates it.
func (s *Service) Place(ctx context.Context, o Order) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `insert into orders (id, total) values ($1, $2)`, o.ID, o.Total); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	go func() {
		if err := s.pub.Publish(context.Background(), "orders", o.ID, o.Encode()); err != nil {
			log.Printf("publish %s: %v", o.ID, err)
		}
	}()
	return nil
}
