package worker

import "context"

type Message struct {
	OperationID, Account string
	Amount               int64
}
type Provider interface {
	Charge(context.Context, string, int64) error
}
type Completion interface {
	MarkDone(context.Context, string) error
}

func Handle(ctx context.Context, provider Provider, completion Completion, message Message) error {
	if err := provider.Charge(ctx, message.Account, message.Amount); err != nil {
		return err
	}
	return completion.MarkDone(ctx, message.OperationID)
}
