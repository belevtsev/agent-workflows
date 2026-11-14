package fixture

import (
	"context"
	"time"
)

type Client interface{ Call(context.Context) error }

func Send(ctx context.Context, client Client) error {
	bounded, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
	defer cancel()
	_ = bounded
	return client.Call(ctx)
}
