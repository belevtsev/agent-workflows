package cli

import (
	"testing"

	"github.com/belevtsev/kitsu/v3/internal/tokens"
)

func TestHarnessShowsAvailableWorkerUsageEvenWhenCoordinatorMissing(t *testing.T) {
	worker := tokens.Start{Session: "parent", Agent: "reviewer", Task: "topic", Stage: "review", Round: 1, Known: true, Models: map[string]tokens.Counts{"model": {Output: 23}}}
	parent := tokens.Start{Session: "parent", Agent: tokens.SessionAgent, Known: true}
	for _, tc := range []struct {
		starts []tokens.Start
		want   string
	}{
		{nil, "unavailable"}, {[]tokens.Start{worker}, "partial"}, {[]tokens.Start{worker, parent}, "complete for attributed notices"},
	} {
		v := harnessView{Task: "topic"}
		v.observeUsage(tokens.Build(tc.starts, nil))
		if v.Coverage != tc.want || ((v.Captured == nil) != (tc.want == "unavailable")) || (v.Captured != nil && *v.Captured != 23) {
			t.Fatalf("coverage=%s total=%v", v.Coverage, v.Captured)
		}
	}
}
