package apply

import "context"

type Effect interface {
	Apply(context.Context, string) error
}
type Records interface {
	MarkDone(context.Context, string) error
}

func Process(ctx context.Context, effect Effect, records Records, operation string) error {
	if err := effect.Apply(ctx, operation); err != nil {
		return err
	}
	return records.MarkDone(ctx, operation)
}
