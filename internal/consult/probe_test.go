package consult_test

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/belevtsev/agent-workflows/internal/consult"
)

func requireMissingCredentials(test *testing.T, ctx context.Context) {
	test.Helper()
	type outcome struct {
		statuses map[string]string
		err      error
	}
	completed := make(chan outcome, 1)
	go func() {
		statuses, err := consult.CheckCredentials(ctx, func(string) string { return "" })
		completed <- outcome{statuses: statuses, err: err}
	}()
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	select {
	case result := <-completed:
		if result.err != nil {
			test.Fatalf("all-missing optional check must not fail: %v", result.err)
		}
		want := map[string]string{
			"jev":              "missing",
			"subq_snap":        "missing",
			"openai_decisions": "missing",
		}
		if !reflect.DeepEqual(result.statuses, want) {
			test.Fatal("all-missing optional check must return exactly the three missing provider statuses")
		}
	case <-timer.C:
		test.Fatal("all-missing optional check did not finish within five seconds")
	}
}

func TestRP1MissingCredentials(test *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	requireMissingCredentials(test, ctx)
}

func TestRP1PrecancelledMissingCredentials(test *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	requireMissingCredentials(test, ctx)
}
