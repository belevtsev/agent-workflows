package pushgate_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/belevtsev/kitsu/v3/internal/kitsutest"
)

// Git gives the pre-push hook the list of pushed refs on a pipe. A wrapper
// that does not pass it on would make every push read as a push of nothing.

// droppedInput installs a hooks directory whose pre-push runs kitsu's own
// hook with the null device as its standard input.
func droppedInput(t *testing.T, repo *kitsutest.Repo) {
	t.Helper()
	own := filepath.ToSlash(filepath.Join(hooksDir(t, repo), "pre-push"))
	writeProgram(t, repo.Path(".githooks/pre-push"), "#!/bin/sh\nexec sh '"+own+"' \"$@\" </dev/null\n")
	repo.Git("config", "core.hooksPath", ".githooks")
}

// decisions is every decision the hooks of a repository recorded, as text.
func decisions(t *testing.T, repo *kitsutest.Repo) string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(repo.StoreDir(), "decisions", "*.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	var all strings.Builder
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		all.Write(data)
	}
	return all.String()
}

func TestAPushWhoseRefListDidNotReachTheHookIsDenied(t *testing.T) {
	t.Parallel()
	repo, origin := gated(t)
	before := refOn(origin, "refs/heads/main")
	repo.Write("src/app.txt", "broken\n")
	commitAround(repo, "a change no check has seen")
	droppedInput(t, repo)

	reason := assertDenied(t, repo.TryGit("push", "origin", "main"), origin, map[string]string{"refs/heads/main": before})
	if !strings.Contains(reason, "standard input") {
		t.Errorf("the denial does not name the standard input:\n%s", reason)
	}
	if recorded := decisions(t, repo); !strings.Contains(recorded, `"outcome":"denied"`) || !strings.Contains(recorded, `"cause":"internal"`) {
		t.Errorf("the denial is not recorded with its cause:\n%s", recorded)
	}
}

// The same wrapper also denies a push that verified content would pass: the
// hook cannot tell which refs are pushed.
func TestAVerifiedPushIsDeniedTooWithoutItsRefList(t *testing.T) {
	t.Parallel()
	repo, origin := gated(t)
	before := refOn(origin, "refs/heads/main")
	repo.Write("docs/note.md", "note\n")
	commitAround(repo, "a note")
	checked(t, repo)
	droppedInput(t, repo)

	assertDenied(t, repo.TryGit("push", "origin", "main"), origin, map[string]string{"refs/heads/main": before})
}

// A pipe that holds no line is what Git sends when a push updates nothing.
func TestAnEmptyRefListOnAPipeIsAPushOfNothing(t *testing.T) {
	t.Parallel()
	repo, origin := gated(t)
	command := repo.Command(filepath.Join(kitsutest.Binary(t), "kitsu"), "hook", "pre-push", "origin", origin.Dir)
	command.Stdin = strings.NewReader("")
	if out, err := command.CombinedOutput(); err != nil {
		t.Fatalf("kitsu hook pre-push with an empty pipe: %v\n%s", err, out)
	}
	if recorded := decisions(t, repo); !strings.Contains(recorded, `"cause":"no_ref_updates"`) {
		t.Errorf("the push of nothing is not recorded as one:\n%s", recorded)
	}
}
