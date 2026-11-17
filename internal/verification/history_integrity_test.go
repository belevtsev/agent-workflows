package verification_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/belevtsev/kitsu/v3/internal/verification"
)

func TestHistoryRequiresExplicitZeroFields(t *testing.T) {
	t.Parallel()
	for _, field := range []string{"expect", "run_id", "content_key", "tree", "log_sha256", "exit_status"} {
		t.Run(field, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			a := historyFixture()
			a.Results = []verification.Result{{ID: "full-check", State: "pending"}}
			h := historyPublish(t, root, a)
			dir := historyDirectory(root, "small")
			data, err := os.ReadFile(filepath.Join(dir, "attempts", h.ID+".json"))
			if err != nil {
				t.Fatal(err)
			}
			needle := []byte(fmt.Sprintf("      %q: \"\",\n", field))
			if field == "exit_status" {
				needle = []byte(",\n      \"exit_status\": null")
			}
			modified := bytes.Replace(data, needle, nil, 1)
			if !json.Valid(modified) || bytes.Equal(data, modified) {
				t.Fatal("mutation must remove an explicit zero field while preserving valid JSON")
			}
			id := fmt.Sprintf("%x", sha256.Sum256(modified))
			historyWrite(t, filepath.Join(dir, "attempts", id+".json"), modified)
			historyWrite(t, filepath.Join(dir, "binding.json"), []byte(fmt.Sprintf("{\n  \"version\": 1,\n  \"id\": %q\n}\n", id)))
			if _, err := verification.Load(root, a.Task); err == nil {
				t.Fatal("missing explicit zero field accepted")
			}
		})
	}
}

func TestHistoryPublicationRecoversInterruptedPromotion(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	a := historyFixture()
	old := historyPublish(t, root, a)
	if err := os.MkdirAll(filepath.Join(root, "openspec/changes", a.Task), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(historyDirectory(root, "small"), historyDirectory(root, "full")); err != nil {
		t.Fatal(err)
	}
	prior, err := verification.Load(root, a.Task)
	if !errors.Is(err, verification.ErrPromotionPending) || prior.ID != old.ID || prior.Attempt.Workflow != "small" {
		t.Fatalf("incomplete promotion = %#v, %v; want usable prior history and ErrPromotionPending", prior, err)
	}
	if problems, err := verification.ValidateRecords(root, a.Task); err != nil || len(problems) == 0 {
		t.Fatalf("incomplete promotion passed validation: %v, %v", problems, err)
	}
	a.Workflow = "full"
	next := historyPublish(t, root, a)
	loaded, err := verification.Load(root, a.Task)
	if err != nil || loaded.ID != next.ID {
		t.Fatalf("recovered binding = %#v, %v", loaded, err)
	}
	if _, err := os.Stat(filepath.Join(historyDirectory(root, "full"), "attempts", old.ID+".json")); err != nil {
		t.Fatalf("recovery lost the small attempt: %v", err)
	}
}

func TestHistoryCorruptionCannotRequestPromotionRecovery(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	a := historyFixture()
	old := historyPublish(t, root, a)
	if err := os.MkdirAll(filepath.Join(root, "openspec/changes", a.Task), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(historyDirectory(root, "small"), historyDirectory(root, "full")); err != nil {
		t.Fatal(err)
	}
	name := filepath.Join(historyDirectory(root, "full"), "attempts", old.ID+".json")
	data, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	historyWrite(t, name, bytes.Replace(data, []byte("reject invalid input"), []byte("tampered expectation"), 1))
	prior, err := verification.Load(root, a.Task)
	if err == nil || errors.Is(err, verification.ErrPromotionPending) || prior.ID != "" {
		t.Fatalf("corruption returned recoverable history: %#v, %v", prior, err)
	}
	a.Workflow = "full"
	if _, err := verification.Publish(root, a.Task, a.Workflow, a); err == nil {
		t.Fatal("publication repaired corrupted promoted history")
	}
}

func TestHistoryBoundsAndUnexpectedFiles(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"oversized-record", "too-many-files", "unexpected-file", "linked-temporary"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			a := historyFixture()
			h := historyPublish(t, root, a)
			dir := historyDirectory(root, "small")
			switch name {
			case "oversized-record":
				historyWrite(t, filepath.Join(dir, "attempts", h.ID+".json"), []byte(strings.Repeat(" ", 1<<20+1)))
			case "too-many-files":
				for i := range 4097 {
					historyWrite(t, filepath.Join(dir, "attempts", fmt.Sprintf(".tmp-%d", i)), nil)
				}
			case "unexpected-file":
				historyWrite(t, filepath.Join(dir, "raw.log"), []byte("private runner details"))
			case "linked-temporary":
				if err := os.Symlink(filepath.Join(dir, "binding.json"), filepath.Join(dir, ".tmp-linked")); err != nil {
					t.Skipf("symlink unavailable: %v", err)
				}
			}
			if _, err := verification.Load(root, a.Task); err == nil {
				t.Fatal("malformed or unbounded history accepted")
			}
		})
	}
}

func TestHistoryTaskDiscoveryRejectsMalformedSmallEntries(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"not-a-directory", "Bad-Task", "linked-task"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			dir := filepath.Join(root, "openspec/changes/.verification", name)
			historyWrite(t, dir, nil)
			if name == "Bad-Task" {
				if err := os.Remove(dir); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(dir, 0o755); err != nil {
					t.Fatal(err)
				}
			}
			if name == "linked-task" {
				if err := os.Remove(dir); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(root, dir); err != nil {
					t.Skipf("symlink unavailable: %v", err)
				}
			}
			if _, err := verification.Tasks(root); err == nil {
				t.Fatal("malformed small task discovery succeeded")
			}
		})
	}
}
