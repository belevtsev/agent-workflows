package pilot

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestPilotMissingWorkerUsageRemainsUnknown(t *testing.T) {
	for _, available := range []int{0, 1, 4} {
		t.Run(fmt.Sprint(available), func(t *testing.T) {
			base := t.TempDir()
			home := filepath.Join(base, "home")
			dir := filepath.Join(home, ".codex", "sessions")
			if err := os.MkdirAll(dir, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "root.jsonl"), fakeRollout("root-001", "", "", "normal", 200, 50, 10, 5), 0o600); err != nil {
				t.Fatal(err)
			}
			for i := range available {
				id := fmt.Sprintf("child-%d", i)
				if err := os.WriteFile(filepath.Join(dir, id+".jsonl"), fakeRollout(id, "root-001", "tests", "normal", 100, 10, 2, 1), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			got, _, err := collectUsage(home, ".codex/sessions", filepath.Join(base, "run"), "root-001")
			if err != nil || got.Complete != (available == 4) || got.Total != int64(210+102*available) {
				t.Fatalf("missing worker became zero or disappeared: available=%d usage=%+v error=%v", available, got, err)
			}
			if available < 4 && len(got.Unknown) == 0 {
				t.Fatal("partial known usage was reported complete")
			}
		})
	}
}
