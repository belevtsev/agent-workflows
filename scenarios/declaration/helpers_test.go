// Package declaration_test holds the scenarios of the specification
// check-declaration
// (openspec/changes/reduce-cw-to-evidence-gate/specs/check-declaration/spec.md).
package declaration_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/belevtsev/gatecub/v3/internal/gatecubtest"
)

// statusCheck is one entry of checks in the output of gatecub status --json.
type statusCheck struct {
	ID    string `json:"id"`
	State string `json:"state"`
}

// statusChecks runs gatecub status --json and returns its checks.
func statusChecks(t *testing.T, repo *gatecubtest.Repo) []statusCheck {
	t.Helper()
	result := repo.Gatecub("status", "--json")
	var status struct {
		Checks []statusCheck `json:"checks"`
	}
	if err := json.Unmarshal([]byte(result.Stdout), &status); err != nil {
		t.Fatalf("gatecub status --json (exit %d) did not print one JSON object: %v\n%s", result.Code, err, result.Output())
	}
	return status.Checks
}

// checkIDs is the sorted list of the ids that gatecub status --json reports.
func checkIDs(t *testing.T, repo *gatecubtest.Repo) []string {
	t.Helper()
	var ids []string
	for _, check := range statusChecks(t, repo) {
		ids = append(ids, check.ID)
	}
	sort.Strings(ids)
	return ids
}

// stateOf is the state gatecub status --json reports for one check.
func stateOf(t *testing.T, repo *gatecubtest.Repo, id string) string {
	t.Helper()
	for _, check := range statusChecks(t, repo) {
		if check.ID == id {
			return check.State
		}
	}
	t.Fatalf("gatecub status --json reports no check %q", id)
	return ""
}

// record is a run record as the store keeps it.
type record struct {
	ID         string `json:"id"`
	Outcome    string `json:"outcome"`
	ExitStatus *int   `json:"exit_status"`
	StartedAt  string `json:"started_at"`
	EndedAt    string `json:"ended_at"`
	Log        string `json:"log"`
}

// records reads every run record of a check.
func records(t *testing.T, repo *gatecubtest.Repo, id string) []record {
	t.Helper()
	var out []record
	for _, path := range repo.RecordFiles(id) {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var rec record
		if err := json.Unmarshal(data, &rec); err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		out = append(out, rec)
	}
	return out
}

// fingerprint describes every file and directory below the given roots, so
// that a test can tell whether a command wrote anything: a directory by its
// mode, a file by its mode, size, modification time and content. The
// modification time of a git object is left out, because git refreshes it
// when the same object is written again; its content cannot change.
func fingerprint(t *testing.T, roots ...string) map[string]string {
	t.Helper()
	objects := string(filepath.Separator) + filepath.Join(".git", "objects") + string(filepath.Separator)
	prints := map[string]string{}
	for _, root := range roots {
		err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			info, err := entry.Info()
			if err != nil {
				return err
			}
			desc := info.Mode().String()
			if !info.IsDir() {
				desc += fmt.Sprintf(" %d", info.Size())
				if !strings.Contains(path, objects) {
					desc += fmt.Sprintf(" %d", info.ModTime().UnixNano())
				}
			}
			if info.Mode().IsRegular() {
				data, err := os.ReadFile(path)
				if err != nil {
					return err
				}
				sum := sha256.Sum256(data)
				desc += " " + hex.EncodeToString(sum[:])
			}
			prints[path] = desc
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	return prints
}

// assertWroteNothing fails the test when the files below the repository or
// its home directory differ from an earlier fingerprint.
func assertWroteNothing(t *testing.T, repo *gatecubtest.Repo, before map[string]string) {
	t.Helper()
	after := fingerprint(t, repo.Dir, repo.Home)
	var changes []string
	for path, desc := range after {
		if old, ok := before[path]; !ok {
			changes = append(changes, "created "+path)
		} else if old != desc {
			changes = append(changes, "changed "+path)
		}
	}
	for path := range before {
		if _, ok := after[path]; !ok {
			changes = append(changes, "removed "+path)
		}
	}
	if len(changes) > 0 {
		sort.Strings(changes)
		t.Errorf("a refused command must write nothing, but:\n%s", strings.Join(changes, "\n"))
	}
}

// sha256File is the SHA-256 of a file of the repository.
func sha256File(repo *gatecubtest.Repo, rel string) string {
	sum := sha256.Sum256([]byte(repo.Read(rel)))
	return hex.EncodeToString(sum[:])
}

// holdsNumber says whether text holds n as a number of its own, not as part
// of a longer number.
func holdsNumber(text string, n int) bool {
	return regexp.MustCompile(`(^|[^0-9])` + fmt.Sprint(n) + `($|[^0-9])`).MatchString(text)
}

// commitAll commits everything in the working tree.
func commitAll(repo *gatecubtest.Repo, message string) {
	repo.Git("add", "-A")
	repo.Git("commit", "-q", "-m", message)
}
