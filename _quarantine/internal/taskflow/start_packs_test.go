package taskflow_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	agentworkflows "github.com/belevtsev/agent-workflows/v3"
	"github.com/belevtsev/agent-workflows/v3/internal/langpack"
	"github.com/belevtsev/agent-workflows/v3/internal/taskflow"
)

// spRepo writes files under a fresh resolved directory; store is a sibling
// path that does not exist yet.
func spRepo(t *testing.T, files map[string]string) (repo, store string) {
	t.Helper()
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	repo = filepath.Join(base, "repo")
	for name, body := range files {
		p := filepath.Join(repo, filepath.FromSlash(name))
		if os.MkdirAll(filepath.Dir(p), 0o755) != nil || os.WriteFile(p, []byte(body), 0o644) != nil {
			t.Fatal("cannot write the project")
		}
	}
	return repo, filepath.Join(base, "store")
}

func spPlan(argv ...string) taskflow.Plan {
	return taskflow.Plan{Title: "t", Kind: "substantial", Checkpoints: &taskflow.CheckpointPolicy{Required: false, Reason: "test"},
		Requirements: []taskflow.Requirement{{ID: "r1", Description: "d", CheckIDs: []string{"unit"}}},
		Checks:       []taskflow.Check{{ID: "unit", Argv: argv, TimeoutSeconds: 300}}}
}

func spManifest(id, rule string, detect ...string) langpack.Manifest {
	return langpack.Manifest{Schema: 1, ID: id, Kind: langpack.KindLanguage, Detect: detect, EvidenceRule: rule}
}

// A19: StartWithPacks records Applicable over the baseline's top-level files,
// with Applied non-nil.
func TestStartWithPacksRecordsPackSet(t *testing.T) {
	builtIn, err := langpack.LoadManifests(agentworkflows.Packs)
	if err != nil || len(builtIn) != 1 {
		t.Fatalf("built-in packs: %v, %v", builtIn, err)
	}
	cases := []struct {
		name, selection string
		files           map[string]string
		selected        []langpack.Manifest
		want            []taskflow.PackBinding
	}{
		{"default go", taskflow.SelectionDefault, map[string]string{"go.mod": ""}, langpack.DefaultSelection(builtIn),
			[]taskflow.PackBinding{{ID: "go", Detected: "go.mod", EvidenceRule: builtIn[0].EvidenceRule}}},
		{"go.mod only in a subdirectory", taskflow.SelectionDefault, map[string]string{"sub/go.mod": ""}, langpack.DefaultSelection(builtIn), nil},
		{"nil selected", taskflow.SelectionExplicit, map[string]string{"go.mod": ""}, nil, nil},
		{"first detect entry, selected order", taskflow.SelectionExplicit, map[string]string{"go.mod": "", "go.work": ""},
			[]langpack.Manifest{spManifest("z", "r.z/v1", "go.mod"), spManifest("both", "", "go.work", "go.mod"), spManifest("miss", "", "x.mod")},
			[]taskflow.PackBinding{{ID: "z", Detected: "go.mod", EvidenceRule: "r.z/v1"}, {ID: "both", Detected: "go.work"}}},
	}
	for _, c := range cases {
		repo, store := spRepo(t, c.files)
		rec, err := taskflow.StartWithPacks(context.Background(), repo, store, spPlan("true"), nil, c.selection, c.selected)
		if err != nil || rec.Packs == nil || rec.Packs.Selection != c.selection || rec.Packs.Applied == nil ||
			len(rec.Packs.Applied) != len(c.want) || (len(c.want) > 0 && !reflect.DeepEqual(rec.Packs.Applied, c.want)) {
			t.Errorf("A19 violated (%s): packs %+v, %v; want applied %+v", c.name, rec.Packs, err, c.want)
			continue
		}
		// A22, I2: the stored record's packs member, written by hand: evidence_rule
		// only when non-empty, applied [] when none.
		applied := []any{}
		for _, b := range c.want {
			m := map[string]any{"id": b.ID, "detected": b.Detected}
			if b.EvidenceRule != "" {
				m["evidence_rule"] = b.EvidenceRule
			}
			applied = append(applied, m)
		}
		want := map[string]any{"selection": c.selection, "applied": applied}
		loaded, err := taskflow.Load(rec.Directory)
		data, _ := json.Marshal(loaded)
		var doc map[string]any
		_ = json.Unmarshal(data, &doc)
		if err != nil || !reflect.DeepEqual(doc["packs"], want) {
			t.Errorf("A22/I2 violated (%s): stored packs %v, %v; want %v", c.name, doc["packs"], err, want)
		}
	}
}

// A19: StartWithPacks refuses a missing project alike for either selection;
// an unknown selection is refused without a task; later revisions keep the
// pack set.
func TestStartWithPacksKeepsPackSet(t *testing.T) {
	ctx := context.Background()
	repo, store := spRepo(t, map[string]string{"go.mod": ""})
	_, errA := taskflow.StartWithPacks(ctx, filepath.Join(repo, "absent"), store, spPlan("true"), nil, taskflow.SelectionExplicit, nil)
	_, errB := taskflow.StartWithPacks(ctx, filepath.Join(repo, "absent"), store, spPlan("true"), nil, taskflow.SelectionDefault, nil)
	if errA == nil || errB == nil || errA.Error() != errB.Error() {
		t.Fatalf("A19 violated: missing project gives explicit %v, default %v", errA, errB)
	}
	_, err := taskflow.StartWithPacks(ctx, repo, store, spPlan("true"), nil, "auto", nil)
	if !errors.Is(err, taskflow.ErrInvalidInput) {
		t.Fatalf("A19 violated: selection auto gives %v", err)
	}
	entries, _ := os.ReadDir(store)
	for _, d := range entries {
		if d.IsDir() {
			t.Fatalf("A19 violated: a refused selection created the task directory %s", d.Name())
		}
	}
	// A22, I2: StartWithPacks keeps its pack set, with [] for no applied pack,
	// in later revisions.
	rec, err := taskflow.StartWithPacks(ctx, repo, store, spPlan("true"), nil, taskflow.SelectionExplicit, nil)
	if err != nil || rec.Packs == nil {
		t.Fatalf("A19 violated: packs %+v, %v", rec.Packs, err)
	}
	if _, err := taskflow.RunCheck(ctx, rec.Directory, "unit"); err != nil {
		t.Fatal(err)
	}
	later, err := taskflow.Load(rec.Directory)
	data, _ := json.Marshal(later)
	if err != nil || later.Revision <= rec.Revision || !reflect.DeepEqual(later.Packs, rec.Packs) || !strings.Contains(string(data), `"applied":[]`) {
		t.Fatalf("A22/I2 violated: later revision %s (%v)", data, err)
	}
	// I2: a non-empty pack set survives a later revision even after its detect
	// file is gone (packs are detected at start only).
	repo2, store2 := spRepo(t, map[string]string{"go.mod": ""})
	c, err := taskflow.StartWithPacks(ctx, repo2, store2, spPlan("true"), nil, taskflow.SelectionExplicit,
		[]langpack.Manifest{spManifest("go", langpack.GoPackageTestV2, "go.mod")})
	if err != nil || c.Packs == nil || len(c.Packs.Applied) != 1 {
		t.Fatalf("A19 violated: packs %+v, %v", c.Packs, err)
	}
	if err := os.Remove(filepath.Join(repo2, "go.mod")); err != nil {
		t.Fatal(err)
	}
	if _, err := taskflow.RunCheck(ctx, c.Directory, "unit"); err != nil {
		t.Fatal(err)
	}
	if later, err := taskflow.Load(c.Directory); err != nil || later.Revision <= c.Revision || !reflect.DeepEqual(later.Packs, c.Packs) {
		t.Fatalf("I2 violated: packs changed from %+v to %+v (%v)", c.Packs, later.Packs, err)
	}
	if data, _ := json.Marshal(taskflow.PackBinding{ID: "x", Detected: "go.mod"}); strings.Contains(string(data), "evidence_rule") {
		t.Fatalf("A22 violated: an empty evidence_rule is not omitted: %s", data)
	}
}
