package hooks

import (
	"context"
	"database/sql"
	"net/http"
	"time"
)

// Delivery is one webhook event queued for one endpoint. Rows live in the
// deliveries table; status is "pending", "delivered" or "failed".
type Delivery struct {
	ID         int64
	EndpointID int64
	Payload    []byte
	Status     string
	Attempts   int
	CreatedAt  time.Time
}

// Sender posts one delivery to its endpoint.
type Sender interface {
	Send(ctx context.Context, d Delivery) (*http.Response, error)
}

// Sweeper sends every pending delivery. It polls the deliveries table every
// Interval and is the only component that sends webhooks today; deliveries
// survive a restart because their state is in the table.
type Sweeper struct {
	DB       *sql.DB
	Sender   Sender
	Interval time.Duration // 5 * time.Second in production
}

// Run polls until ctx is cancelled.
func (s *Sweeper) Run(ctx context.Context) error {
	ticker := time.NewTicker(s.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := s.sweepOnce(ctx); err != nil {
				return err
			}
		}
	}
}

// sweepOnce sends each pending delivery once. A delivery that fails is
// marked "failed" and never sent again: there are no retries today.
func (s *Sweeper) sweepOnce(ctx context.Context) error {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT id, endpoint_id, payload, attempts, created_at
		   FROM deliveries WHERE status = 'pending' ORDER BY id`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var d Delivery
		if err := rows.Scan(&d.ID, &d.EndpointID, &d.Payload, &d.Attempts, &d.CreatedAt); err != nil {
			return err
		}
		status := "delivered"
		resp, err := s.Sender.Send(ctx, d)
		if err != nil || resp.StatusCode >= 300 {
			status = "failed"
		}
		if _, err := s.DB.ExecContext(ctx,
			`UPDATE deliveries SET status = $1, attempts = attempts + 1 WHERE id = $2`,
			status, d.ID); err != nil {
			return err
		}
	}
	return rows.Err()
}
