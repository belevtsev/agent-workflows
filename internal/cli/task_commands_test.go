package cli

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	json "encoding/json/v2"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/belevtsev/agent-workflows/internal/manageruntime"
	"github.com/belevtsev/agent-workflows/internal/taskflow"
)

func TestTaskCLIUsesSelectedProjectAndPrivateExternalStorage(t *testing.T) {
	repo, home := t.TempDir(), t.TempDir()
	data := []byte("verified source\n")
	if err := os.WriteFile(filepath.Join(repo, "source.txt"), data, 0600); err != nil {
		t.Fatal(err)
	}
	config, out, _, _ := testConfig(t)
	config.HomeDir = func() (string, error) { return home, nil }
	config.WorkingDir = func() (string, error) { return repo, nil }
	config.Getenv = func(key string) string {
		if key != "CODEX_HOME" && key != "CLAUDE_CONFIG_DIR" && key != "XDG_STATE_HOME" {
			t.Fatal("task command queried adviser credentials")
		}
		return ""
	}
	plan := `{"title":"Inspect real source","kind":"small","requirements":[{"id":"source","description":"Existing source inspected","check_ids":[],"allow_non_execution":true}],"checks":[]}`
	config.In = strings.NewReader(plan)
	if err := Execute(t.Context(), []string{"task", "start", "--plan", "-"}, config); err != nil {
		t.Fatal(err)
	}
	var record taskflow.Record
	if err := json.Unmarshal(out.Bytes(), &record); err != nil {
		t.Fatal(err)
	}
	canonicalRepo, err := filepath.EvalSymlinks(repo)
	if err != nil {
		t.Fatal(err)
	}
	canonicalHome, err := filepath.EvalSymlinks(home)
	if err != nil {
		t.Fatal(err)
	}
	if record.Baseline.Root != canonicalRepo || !strings.HasPrefix(record.Directory, filepath.Join(canonicalHome, ".local", "state", "agent-workflows", "workflow-tasks")) {
		t.Fatalf("wrong project/storage: %+v", record)
	}
	out.Reset()
	if err := Execute(t.Context(), []string{"task", "verify", "--task", record.Directory}, config); err == nil {
		t.Fatal("incomplete task was completed")
	}
	var gate taskflow.Verification
	if err := json.Unmarshal(out.Bytes(), &gate); err != nil || gate.Complete || len(gate.Problems) == 0 {
		t.Fatalf("missing gate problems: %s %v", out.String(), err)
	}
	hash := sha256.Sum256(data)
	ref := taskflow.SourceRef{Path: "source.txt", StartLine: 1, EndLine: 1, SHA256: hex.EncodeToString(hash[:])}
	input := taskflow.AssessmentInput{Requirements: []taskflow.Assessment{{ID: "source", Status: "satisfied", Evidence: []taskflow.SourceRef{ref}, CheckIDs: []string{}, NonExecutionReason: "Read-only source inspection; no executable behavior changed."}}, Decision: &taskflow.Decision{Status: "resolved", Reason: "Actual source reference establishes this small inspection result.", Evidence: []taskflow.SourceRef{ref}, CheckIDs: []string{}, NonExecutionReason: "Read-only source inspection; no executable behavior changed."}}
	encoded, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	config.In = bytes.NewReader(encoded)
	out.Reset()
	if err := Execute(t.Context(), []string{"task", "assess", "--task", record.Directory, "--input", "-"}, config); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := Execute(t.Context(), []string{"task", "verify", "--task", record.Directory}, config); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(out.Bytes(), &gate); err != nil || !gate.Complete {
		t.Fatalf("gate failed: %s %v", out.String(), err)
	}
	out.Reset()
	if err := Execute(t.Context(), []string{"--dry-run", "task", "show", "--task", record.Directory}, config); err != nil {
		t.Fatal(err)
	}
}

// R3.1: a task record chain whose revisions are version 1 is refused with the
// restart guidance, and its record files stay byte-identical.
func TestTaskCLIRefusesVersionOneRecordsWithoutWrites(t *testing.T) {
	repo, home := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(repo, "source.txt"), []byte("source\n"), 0600); err != nil {
		t.Fatal(err)
	}
	config, out, _, _ := testConfig(t)
	stderr := new(bytes.Buffer)
	config.Err = stderr
	config.HomeDir = func() (string, error) { return home, nil }
	config.WorkingDir = func() (string, error) { return repo, nil }
	config.In = strings.NewReader(`{"title":"Inspect source","kind":"small","requirements":[{"id":"source","description":"Read source","allow_non_execution":true}],"checks":[]}`)
	if err := Execute(t.Context(), []string{"task", "start", "--plan", "-"}, config); err != nil {
		t.Fatal(err)
	}
	var record taskflow.Record
	if err := json.Unmarshal(out.Bytes(), &record); err != nil {
		t.Fatal(err)
	}
	revisions, err := filepath.Glob(filepath.Join(record.Directory, "record-*.json"))
	if err != nil || len(revisions) == 0 {
		t.Fatalf("no task revisions in %s: %v", record.Directory, err)
	}
	// Turn the stored chain into a version-1 chain without other changes.
	for _, path := range revisions {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var document map[string]any
		if err := json.Unmarshal(data, &document); err != nil {
			t.Fatal(err)
		}
		if document["version"] != float64(taskflow.Version) {
			t.Fatalf("%s is not a current record: version %v", path, document["version"])
		}
		document["version"] = 1
		if data, err = json.Marshal(document); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(path, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(path, info.Mode().Perm()); err != nil {
			t.Fatal(err)
		}
	}
	snapshot := func() map[string]string {
		t.Helper()
		names, err := filepath.Glob(filepath.Join(record.Directory, "record-*.json"))
		if err != nil {
			t.Fatal(err)
		}
		files := map[string]string{}
		for _, name := range names {
			data, err := os.ReadFile(name)
			if err != nil {
				t.Fatal(err)
			}
			files[filepath.Base(name)] = string(data)
		}
		return files
	}
	before := snapshot()
	const refusal = "task record version 1 is no longer supported; start a new task with 'cw task start'"
	for _, command := range []string{"show", "verify"} {
		t.Run(command, func(t *testing.T) {
			out.Reset()
			stderr.Reset()
			err := Execute(t.Context(), []string{"task", command, "--task", record.Directory}, config)
			if err == nil {
				t.Fatal("version-1 task record was loaded")
			}
			if !strings.Contains(err.Error()+out.String()+stderr.String(), refusal) {
				t.Fatalf("missing version-1 guidance: %v stdout %q stderr %q", err, out, stderr)
			}
			after := snapshot()
			if len(after) != len(before) {
				t.Fatalf("task revisions changed: %d -> %d", len(before), len(after))
			}
			for name, data := range before {
				if after[name] != data {
					t.Fatalf("%s changed after refusal", name)
				}
			}
		})
	}
}

func TestTaskMutationsRejectInheritedDryRunBeforeWrites(t *testing.T) {
	for _, command := range []string{"start", "context", "run", "assess", "assign", "handoff", "accept", "consult", "verify"} {
		t.Run(command, func(t *testing.T) {
			config, _, _, _ := testConfig(t)
			config.HomeDir = func() (string, error) { t.Fatal("dry-run accessed storage"); return "", nil }
			config.WorkingDir = config.HomeDir
			err := Execute(t.Context(), []string{"--dry-run", "task", command}, config)
			if err == nil || ExitCode(err) != 2 || !strings.Contains(err.Error(), "reject --dry-run") {
				t.Fatalf("dry-run was not rejected explicitly: %v", err)
			}
		})
	}
}

func TestTaskCLIRejectsUnknownPlanFields(t *testing.T) {
	config, _, _, _ := testConfig(t)
	config.In = strings.NewReader(`{"title":"x","kind":"small","commands_from_agents_md":true}`)
	if err := Execute(t.Context(), []string{"task", "start", "--plan", "-"}, config); err == nil || !strings.Contains(err.Error(), "unknown") {
		t.Fatalf("unknown plan accepted: %v", err)
	}
}

func TestTaskUnexpectedArgumentsUseUsageExitCode(t *testing.T) {
	for _, args := range [][]string{{"task", "unknown"}, {"task", "start", "unexpected"}, {"task", "assign", "unexpected"}, {"task", "handoff", "unexpected"}, {"task", "accept", "unexpected"}, {"task", "show", "unexpected"}} {
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			config, _, _, _ := testConfig(t)
			config.HomeDir = func() (string, error) { t.Fatal("invalid arguments reached task execution"); return "", nil }
			config.WorkingDir = config.HomeDir
			if err := Execute(t.Context(), args, config); err == nil || ExitCode(err) != 2 {
				t.Fatalf("task usage %v returned %v (exit %d)", args, err, ExitCode(err))
			}
		})
	}
}

func TestTaskStoreUsesActiveCodexProfilePrecedence(t *testing.T) {
	base := t.TempDir()
	repo := filepath.Join(base, "repo")
	if err := os.Mkdir(repo, 0700); err != nil {
		t.Fatal(err)
	}
	defaultHome := filepath.Join(base, "default-home")
	explicitHome := filepath.Join(base, "explicit-home")
	envState := filepath.Join(base, "environment-state")
	explicitState := filepath.Join(base, "explicit-state")
	explicitStore := filepath.Join(base, "explicit-store")
	plan := `{"title":"Profile storage","kind":"small","requirements":[{"id":"source","description":"Read source","allow_non_execution":true}],"checks":[]}`
	cases := []struct {
		name, environment, want string
		flags                   []string
	}{
		{name: "default", want: filepath.Join(defaultHome, ".local", "state", "agent-workflows", "workflow-tasks")},
		{name: "explicit home", flags: []string{"--home", explicitHome}, want: filepath.Join(explicitHome, ".local", "state", "agent-workflows", "workflow-tasks")},
		{name: "environment beats home", environment: envState, flags: []string{"--home", explicitHome}, want: filepath.Join(envState, "agent-workflows", "workflow-tasks")},
		{name: "explicit state beats environment", environment: envState, flags: []string{"--home", explicitHome, "--state-dir", explicitState}, want: filepath.Join(explicitState, "workflow-tasks")},
		{name: "explicit store beats profile", environment: envState, flags: []string{"--home", explicitHome, "--state-dir", explicitState, "--store", explicitStore}, want: explicitStore},
	}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			config, out, _, _ := testConfig(t)
			config.In = strings.NewReader(plan)
			config.HomeDir = func() (string, error) {
				if item.name == "explicit store beats profile" {
					t.Fatal("explicit storage resolved a home")
				}
				return defaultHome, nil
			}
			config.WorkingDir = func() (string, error) { return repo, nil }
			config.Getenv = func(key string) string {
				if item.name == "explicit store beats profile" {
					t.Fatal("explicit storage read environment")
				}
				if key == "XDG_STATE_HOME" {
					return item.environment
				}
				return ""
			}
			config.Resolve = func(string) (manageruntime.Locator, error) { return manageruntime.Locator{}, os.ErrNotExist }
			args := append([]string{"task", "start", "--plan", "-"}, item.flags...)
			if err := Execute(t.Context(), args, config); err != nil {
				t.Fatal(err)
			}
			var record taskflow.Record
			if err := json.Unmarshal(out.Bytes(), &record); err != nil {
				t.Fatal(err)
			}
			canonical, err := filepath.EvalSymlinks(item.want)
			if err != nil {
				t.Fatal(err)
			}
			if filepath.Dir(record.Directory) != canonical {
				t.Fatalf("profile store got %s want %s", record.Directory, canonical)
			}
		})
	}
}

func TestWorkerCLIValidatesHandoffsOutsideTheManagerRepository(t *testing.T) {
	repo, home := t.TempDir(), t.TempDir()
	data := []byte("resource \"example\" \"checked\" {}\n")
	if err := os.WriteFile(filepath.Join(repo, "main.tf"), data, 0600); err != nil {
		t.Fatal(err)
	}
	config, out, _, _ := testConfig(t)
	config.HomeDir = func() (string, error) { return home, nil }
	config.WorkingDir = func() (string, error) { return repo, nil }
	config.Getenv = func(string) string { return "" }
	config.In = strings.NewReader(`{"title":"Inspect project source","kind":"small","requirements":[{"id":"source","description":"Inspect source","allow_non_execution":true}],"checks":[]}`)
	if err := Execute(t.Context(), []string{"task", "start", "--plan", "-"}, config); err != nil {
		t.Fatal(err)
	}
	var record taskflow.Record
	if err := json.Unmarshal(out.Bytes(), &record); err != nil {
		t.Fatal(err)
	}
	task := record.Directory
	run := func(command string, input any) {
		t.Helper()
		encoded, err := json.Marshal(input)
		if err != nil {
			t.Fatal(err)
		}
		config.In = bytes.NewReader(encoded)
		out.Reset()
		if err := Execute(t.Context(), []string{"task", command, "--task", task, "--input", "-"}, config); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(out.Bytes(), &record); err != nil {
			t.Fatalf("%s returned invalid JSON: %v", command, err)
		}
	}
	run("assign", taskflow.WorkerBrief{
		ID: "inspect", AgentID: "/root/project_explorer", Role: "evidence",
		Goal: "Inspect the selected Terraform source", OwnedPaths: []string{"main.tf"},
		Constraints: []string{"Read source without changing it"}, CheckIDs: []string{},
		StopCondition: "Return the source evidence and uncertainties",
	})
	if len(record.Assignments) != 1 {
		t.Fatal("assignment was not retained")
	}
	assignment := record.Assignments[0]
	hash := sha256.Sum256(data)
	ref := taskflow.SourceRef{Path: "main.tf", StartLine: 1, EndLine: 1, SHA256: hex.EncodeToString(hash[:])}
	result := taskflow.WorkerResult{
		TaskID: record.ID, AssignmentID: "inspect", AssignmentSHA256: assignment.SHA256,
		AgentID: "/root/project_explorer", Snapshot: assignment.Snapshot,
		Status: "completed", Summary: "Inspected the selected resource declaration",
		Evidence: []taskflow.SourceRef{ref}, ChangedFiles: []taskflow.WorkerChangedFile{},
		CheckIDs: []string{}, Findings: []taskflow.WorkerFinding{}, Uncertainties: []string{},
	}
	bad := result
	bad.AgentID = "/root/different_agent"
	encoded, err := json.Marshal(bad)
	if err != nil {
		t.Fatal(err)
	}
	config.In = bytes.NewReader(encoded)
	out.Reset()
	if err := Execute(t.Context(), []string{"task", "handoff", "--task", task, "--input", "-"}, config); err == nil {
		t.Fatal("handoff from the wrong worker was accepted")
	}
	stored, err := taskflow.Load(task)
	if err != nil || len(stored.WorkerResults) != 0 {
		t.Fatalf("rejected handoff changed task evidence: %v", err)
	}
	run("handoff", result)
	run("accept", taskflow.WorkerAcceptance{
		TaskID: record.ID, AssignmentID: "inspect", ResultSHA256: record.WorkerResults[0].SHA256,
		CoordinatorID: "/root", Status: "accepted", Reason: "Checked the returned reference against the selected source",
		Evidence: []taskflow.SourceRef{ref}, CheckIDs: []string{}, Findings: []taskflow.FindingDisposition{},
	})
	if len(record.WorkerAcceptances) != 1 || record.WorkerAcceptances[0].Acceptance.ResultSHA256 != record.WorkerResults[0].SHA256 {
		t.Fatal("coordinator acceptance was not bound to the exact handoff")
	}
}

func TestWorkerCLIRejectsUnknownAndDuplicateFields(t *testing.T) {
	for _, command := range []string{"assign", "handoff", "accept"} {
		for _, input := range []string{`{"unrecognized":true}`, `{"task_id":"first","task_id":"second"}`} {
			t.Run(command+input, func(t *testing.T) {
				config, _, _, _ := testConfig(t)
				config.In = strings.NewReader(input)
				if err := Execute(t.Context(), []string{"task", command, "--task", "/missing/task", "--input", "-"}, config); err == nil {
					t.Fatal("invalid worker input was accepted")
				} else if !strings.Contains(err.Error(), "unknown") && !strings.Contains(err.Error(), "duplicate") {
					t.Fatalf("invalid worker input reached task storage: %v", err)
				}
			})
		}
	}
}

func TestSubstantialCLIReviewGateAppliesToSourceChanges(t *testing.T) {
	for _, changed := range []bool{false, true} {
		name := "unchanged read-only"
		if changed {
			name = "changed source"
		}
		t.Run(name, func(t *testing.T) {
			repo, home := t.TempDir(), t.TempDir()
			file := filepath.Join(repo, "settings.yaml")
			if err := os.WriteFile(file, []byte("enabled: false\n"), 0600); err != nil {
				t.Fatal(err)
			}
			config, out, _, _ := testConfig(t)
			config.HomeDir = func() (string, error) { return home, nil }
			config.WorkingDir = func() (string, error) { return repo, nil }
			config.Getenv = func(string) string { return "" }
			config.In = strings.NewReader(`{"title":"Inspect selected project","kind":"substantial","requirements":[{"id":"source","description":"Inspect actual source","allow_non_execution":true}],"checks":[]}`)
			if err := Execute(t.Context(), []string{"task", "start", "--plan", "-"}, config); err != nil {
				t.Fatal(err)
			}
			var record taskflow.Record
			if err := json.Unmarshal(out.Bytes(), &record); err != nil {
				t.Fatal(err)
			}
			if changed {
				if err := os.WriteFile(file, []byte("enabled: true\n"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			out.Reset()
			if err := Execute(t.Context(), []string{"task", "verify", "--task", record.Directory}, config); err == nil {
				t.Fatal("incomplete task passed completion")
			}
			var result taskflow.Verification
			if err := json.Unmarshal(out.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			reviewRequired := false
			for _, problem := range result.Problems {
				if strings.Contains(problem, "independent") && strings.Contains(problem, "review") {
					reviewRequired = true
				}
			}
			if reviewRequired != changed {
				t.Fatalf("source change=%t, independent review required=%t: %v", changed, reviewRequired, result.Problems)
			}
		})
	}
}
