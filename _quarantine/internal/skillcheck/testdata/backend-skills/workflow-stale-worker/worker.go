package worker

import "context"

type Owners interface {
	CurrentGeneration(context.Context, string) (uint64, error)
}
type Effect interface {
	Set(context.Context, string, string) error
}

func Run(ctx context.Context, owners Owners, effect Effect, resource, value string, generation uint64) error {
	current, err := owners.CurrentGeneration(ctx, resource)
	if err != nil {
		return err
	}
	if current != generation {
		return nil
	}
	return effect.Set(ctx, resource, value)
}
