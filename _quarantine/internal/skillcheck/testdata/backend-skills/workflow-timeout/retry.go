package retry

import (
	"context"
	"errors"
)

type Provider interface {
	Apply(context.Context, string, string) error
}

func Apply(ctx context.Context, provider Provider, operationID, payload string) error {
	err := provider.Apply(ctx, operationID+"-attempt-1", payload)
	if errors.Is(err, context.DeadlineExceeded) {
		return provider.Apply(ctx, operationID+"-attempt-2", payload)
	}
	return err
}
