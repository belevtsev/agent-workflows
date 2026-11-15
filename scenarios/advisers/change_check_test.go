package advisers_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func assertSmallChangeChecks(t *testing.T, f *fixture, namedTask string, want int) {
	t.Helper()
	for _, args := range [][]string{{"change", "check"}, {"change", "check", namedTask}} {
		result := requireCode(t, f.Kitsu(args...), want)
		if want != 0 && !strings.Contains(result.Output(), "adviser") {
			t.Fatalf("malformed adviser history was not identified:\n%s", result.Output())
		}
	}
}

func linkSmallRecord(t *testing.T, f *fixture, relative string) {
	t.Helper()
	original := f.Path(relative)
	destination := filepath.Join(f.Home, "linked-record")
	if err := os.Rename(original, destination); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(destination, original); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
}

// Small histories remain ordinary record evidence. Missing checkpoints and
// interrupted calls are valid structures; malformed evidence fails the check.
func TestSmallTaskRecordChecks(t *testing.T) {
	t.Parallel()
	t.Run("no adviser histories", func(t *testing.T) {
		t.Parallel()
		f := newFixture(t, "answer", "", false)
		f.Remove("kitsu-advisers.toml")
		result := requireCode(t, f.Kitsu("change", "check"), 0)
		if !strings.Contains(result.Output(), "holds no change folder") {
			t.Fatalf("the no-record report changed:\n%s", result.Output())
		}
		requireCode(t, f.Kitsu("change", "check", task), 1)
		if f.Exists("openspec") || f.callCount() != 0 {
			t.Fatal("a read-only check created records or invoked an adviser")
		}
	})
	t.Run("missing future checkpoints", func(t *testing.T) {
		t.Parallel()
		f := newFixture(t, "answer", allCheckpoints, false)
		f.bind("small", false)
		assertSmallChangeChecks(t, f, task, 0)
		f.check(5, "")
		requireCode(t, f.Kitsu("change", "status", task, "--json"), 1)
		if f.Exists(fullFolder) || f.callCount() != 0 {
			t.Fatal("small checks created a full folder or invoked an adviser")
		}
	})
	t.Run("pending consultation", func(t *testing.T) {
		t.Parallel()
		f := newFixture(t, "answer", allCheckpoints, false)
		f.bind("small", false)
		c := f.consult("reviewer", "review", "review-advice",
			`{"version":1,"kind":"advice","question":"Consider this small change."}`, 0)
		// Removing only the terminal result models interruption after Begin.
		f.Remove(smallAdvice + "exchanges/" + c.ID + "/result.json")
		assertSmallChangeChecks(t, f, task, 0)
		requireState(t, f.check(5, ""), "review-advice", "pending")
		if f.Exists(fullFolder) || f.callCount() != 1 {
			t.Fatal("checking pending evidence created a full folder or ran another call")
		}
	})
	for _, test := range []struct {
		name   string
		mutate func(t *testing.T, f *fixture)
	}{
		{
			name: "malformed binding",
			mutate: func(t *testing.T, f *fixture) {
				f.Write(smallAdvice+"binding.json", "{")
			},
		},
		{
			name: "missing binding",
			mutate: func(t *testing.T, f *fixture) {
				f.Remove(smallAdvice + "binding.json")
			},
		},
		{
			name: "malformed consultation",
			mutate: func(t *testing.T, f *fixture) {
				c := f.consult("reviewer", "review", "review-advice",
					`{"version":1,"kind":"advice","question":"Consider this small change."}`, 0)
				f.Write(smallAdvice+"exchanges/"+c.ID+"/request.json", "{}")
			},
		},
		{
			name: "malformed terminal result",
			mutate: func(t *testing.T, f *fixture) {
				c := f.consult("reviewer", "review", "review-advice",
					`{"version":1,"kind":"advice","question":"Consider this small change."}`, 0)
				f.Write(smallAdvice+"exchanges/"+c.ID+"/result.json", `{"outcome":"claimed"}`)
			},
		},
		{
			name: "duplicate full and small histories",
			mutate: func(t *testing.T, f *fixture) {
				if err := os.MkdirAll(f.Path(fullAdvice), 0o700); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "history is a file",
			mutate: func(t *testing.T, f *fixture) {
				f.Remove(smallAdvice)
				f.Write(strings.TrimSuffix(smallAdvice, "/"), "{}")
			},
		},
		{
			name: "container is a file",
			mutate: func(t *testing.T, f *fixture) {
				f.Remove("openspec/changes/.advice")
				f.Write("openspec/changes/.advice", "{}")
			},
		},
		{
			name: "symlink history",
			mutate: func(t *testing.T, f *fixture) {
				linkSmallRecord(t, f, smallAdvice)
			},
		},
		{
			name: "symlink binding",
			mutate: func(t *testing.T, f *fixture) {
				linkSmallRecord(t, f, smallAdvice+"binding.json")
			},
		},
		{
			name: "symlink container",
			mutate: func(t *testing.T, f *fixture) {
				linkSmallRecord(t, f, "openspec/changes/.advice")
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			f := newFixture(t, "answer", allCheckpoints, false)
			f.bind("small", false)
			test.mutate(t, f)
			calls := f.callCount()
			assertSmallChangeChecks(t, f, task, 5)
			if f.callCount() != calls {
				t.Fatal("a malformed-record check invoked an adviser")
			}
		})
	}
	for _, name := range []string{"invalid_task", "unexpected.json"} {
		t.Run("invalid entry "+name, func(t *testing.T) {
			t.Parallel()
			f := newFixture(t, "answer", "", false)
			f.bind("small", false)
			f.Write("openspec/changes/.advice/"+name, "{}")
			result := requireCode(t, f.Kitsu("change", "check"), 5)
			if !strings.Contains(result.Output(), name) {
				t.Fatalf("invalid history entry was not identified:\n%s", result.Output())
			}
			// A named check stays scoped to the selected valid history.
			requireCode(t, f.Kitsu("change", "check", task), 0)
		})
	}
}
