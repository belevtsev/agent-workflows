package onboarding

import (
	"context"
	"errors"
	"os"
)

// workflowCommandInput keeps a blocked request write out of the stdout
// decoder. Closing the file and cancelling the writer also unblocks it if
// the server exits early, before it reads the next message.
func workflowCommandInput(ctx context.Context, cancel context.CancelFunc) (input *os.File, send func([]byte) error, finish func() error, err error) {
	input, writer, err := os.Pipe()
	if err != nil {
		return nil, nil, nil, err
	}
	requests := make(chan []byte, 2)
	done := make(chan error, 1)
	go func() {
		err := writeWorkflowRequests(ctx, writer, requests)
		if err != nil && ctx.Err() == nil {
			done <- err
			cancel()
			return
		}
		done <- nil
	}()
	send = func(data []byte) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case requests <- data:
			return nil
		}
	}
	finish = func() error {
		cancel()
		inErr, outErr := input.Close(), writer.Close()
		if errors.Is(outErr, os.ErrClosed) {
			outErr = nil
		}
		return errors.Join(<-done, inErr, outErr)
	}
	return input, send, finish, nil
}

func writeWorkflowRequests(ctx context.Context, writer *os.File, requests <-chan []byte) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		case request := <-requests:
			if request == nil {
				return writer.Close()
			}
			if _, err := writer.Write(request); err != nil {
				return err
			}
		}
	}
}
