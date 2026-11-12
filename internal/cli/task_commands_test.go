package cli

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	json "encoding/json/v2"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/belevtsev/agent-workflows/v3/internal/taskflow"
)

func TestTaskCLIUsesSelectedProjectAndPrivateExternalStorage(t *testing.T) {
	repo, home := t.TempDir(), t.TempDir()
	data := []byte("verified source\n")
	if err := os.WriteFile(filepath.Join(repo, "source.txt"), data, 0600); err != nil {
		t.Fatal(err)
	}
	config, out := testConfig(t)
	config.HomeDir = func() (string, error) { return home, nil }
	config.WorkingDir = func() (string, error) { return repo, nil }
	// D5 / Revision 3: task start reads the credential environment to bind
	// advisers; none is set here.
	config.Getenv = func(string) string { return "" }
	plan := `{"title":"Inspect real source","kind":"small","requirements":[{"id":"source","description":"Existing source inspected","check_ids":[],"allow_non_execution":true}],"checks":[]}`
	config.In = strings.NewReader(plan)
	if err := Execute(t.Context(), []string{"task", "start", "--plan", "-", "--policy", shippedPolicyPath}, config); err != nil {
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
	if len(record.Advisers) != 0 {
		t.Fatalf("task start bound %v without credentials", record.Advisers)
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

// D5 and D11 (task records, versions 3 to 5): a task record chain whose
// revisions hold an older version is refused with the restart guidance, one
// with a newer version with the upgrade guidance, and its record files stay
// byte-identical.
func TestTaskCLIRefusesOtherRecordVersionsWithoutWrites(t *testing.T) {
	for _, version := range []int{1, 2, 6} {
		t.Run(fmt.Sprintf("version_%d", version), func(t *testing.T) {
			repo, home := t.TempDir(), t.TempDir()
			if err := os.WriteFile(filepath.Join(repo, "source.txt"), []byte("source\n"), 0600); err != nil {
				t.Fatal(err)
			}
			config, out := testConfig(t)
			stderr := new(bytes.Buffer)
			config.Err = stderr
			config.HomeDir = func() (string, error) { return home, nil }
			config.WorkingDir = func() (string, error) { return repo, nil }
			config.In = strings.NewReader(`{"title":"Inspect source","kind":"small","requirements":[{"id":"source","description":"Read source","allow_non_execution":true}],"checks":[]}`)
			if err := Execute(t.Context(), []string{"task", "start", "--plan", "-", "--policy", shippedPolicyPath}, config); err != nil {
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
			// Change only the version of every stored revision.
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
					t.Fatalf("%s is not a version %d record: version %v", path, taskflow.Version, document["version"])
				}
				document["version"] = version
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
			refusal := fmt.Sprintf("task record version %d is not supported; this cw reads versions 3 to 5; start a new task with 'cw task start'", version)
			if version > 5 {
				refusal = fmt.Sprintf("task record version %d was written by a newer cw; this cw reads versions 3 to 5; upgrade cw to read it", version)
			}
			for _, command := range []string{"show", "verify"} {
				t.Run(command, func(t *testing.T) {
					out.Reset()
					stderr.Reset()
					err := Execute(t.Context(), []string{"task", command, "--task", record.Directory}, config)
					if err == nil {
						t.Fatalf("version-%d task record was loaded", version)
					}
					if !strings.Contains(err.Error()+out.String()+stderr.String(), refusal) {
						t.Fatalf("missing restart guidance %q: %v stdout %q stderr %q", refusal, err, out, stderr)
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
		})
	}
}

func TestTaskMutationsRejectInheritedDryRunBeforeWrites(t *testing.T) {
	for _, command := range []string{"start", "context", "run", "assess", "assign", "handoff", "accept", "consult", "verify"} {
		t.Run(command, func(t *testing.T) {
			config, _ := testConfig(t)
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
	config, _ := testConfig(t)
	config.In = strings.NewReader(`{"title":"x","kind":"small","commands_from_agents_md":true}`)
	if err := Execute(t.Context(), []string{"task", "start", "--plan", "-", "--policy", shippedPolicyPath}, config); err == nil || !strings.Contains(err.Error(), "unknown") {
		t.Fatalf("unknown plan accepted: %v", err)
	}
}

func TestTaskUnexpectedArgumentsUseUsageExitCode(t *testing.T) {
	for _, args := range [][]string{{"task", "unknown"}, {"task", "start", "unexpected"}, {"task", "assign", "unexpected"}, {"task", "handoff", "unexpected"}, {"task", "accept", "unexpected"}, {"task", "show", "unexpected"}} {
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			config, _ := testConfig(t)
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
			config, out := testConfig(t)
			config.In = strings.NewReader(plan)
			// D5 / Revision 3: task start also resolves the credential
			// environment (env-or-saved, like cw consult), so even an explicit
			// store may read the home and the environment; only the store
			// location is checked here.
			config.HomeDir = func() (string, error) { return defaultHome, nil }
			config.WorkingDir = func() (string, error) { return repo, nil }
			config.Getenv = func(key string) string {
				if key == "XDG_STATE_HOME" {
					return item.environment
				}
				return ""
			}
			args := append([]string{"task", "start", "--plan", "-", "--policy", shippedPolicyPath}, item.flags...)
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
	config, out := testConfig(t)
	config.HomeDir = func() (string, error) { return home, nil }
	config.WorkingDir = func() (string, error) { return repo, nil }
	config.Getenv = func(string) string { return "" }
	config.In = strings.NewReader(`{"title":"Inspect project source","kind":"small","requirements":[{"id":"source","description":"Inspect source","allow_non_execution":true}],"checks":[]}`)
	if err := Execute(t.Context(), []string{"task", "start", "--plan", "-", "--policy", shippedPolicyPath}, config); err != nil {
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
				config, _ := testConfig(t)
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
			config, out := testConfig(t)
			config.HomeDir = func() (string, error) { return home, nil }
			config.WorkingDir = func() (string, error) { return repo, nil }
			config.Getenv = func(string) string { return "" }
			config.In = strings.NewReader(`{"title":"Inspect selected project","kind":"substantial","requirements":[{"id":"source","description":"Inspect actual source","check_ids":["unit"],"allow_non_execution":true}],"checks":[{"id":"unit","argv":["go","test","./..."]}]}`)
			if err := Execute(t.Context(), []string{"task", "start", "--plan", "-", "--policy", shippedPolicyPath}, config); err != nil {
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

// D5 (flow enforcement): a substantial task rejects cw task assign until the
// before checkpoint is attached and resolved, and stores no assignment; a
// small task is not gated (see TestWorkerCLIValidatesHandoffsOutsideTheManagerRepository).
func TestSubstantialAssignRequiresResolvedBeforeCheckpoint(t *testing.T) {
	repo, home := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(repo, "main.tf"), []byte("resource \"example\" \"checked\" {}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	config, out := testConfig(t)
	stderr := new(bytes.Buffer)
	config.Err = stderr
	config.HomeDir = func() (string, error) { return home, nil }
	config.WorkingDir = func() (string, error) { return repo, nil }
	config.Getenv = func(string) string { return "" }
	config.In = strings.NewReader(`{"title":"Change project source","kind":"substantial","requirements":[{"id":"source","description":"Inspect actual source","check_ids":["unit"],"allow_non_execution":true}],"checks":[{"id":"unit","argv":["go","test","./..."]}]}`)
	if err := Execute(t.Context(), []string{"task", "start", "--plan", "-", "--policy", shippedPolicyPath}, config); err != nil {
		t.Fatal(err)
	}
	var record taskflow.Record
	if err := json.Unmarshal(out.Bytes(), &record); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(taskflow.WorkerBrief{
		ID: "inspect", AgentID: "/root/project_explorer", Role: "evidence",
		Goal: "Inspect the selected Terraform source", OwnedPaths: []string{"main.tf"},
		Constraints: []string{"Read source without changing it"}, CheckIDs: []string{},
		StopCondition: "Return the source evidence and uncertainties",
	})
	if err != nil {
		t.Fatal(err)
	}
	config.In = bytes.NewReader(encoded)
	out.Reset()
	err = Execute(t.Context(), []string{"task", "assign", "--task", record.Directory, "--input", "-"}, config)
	if err == nil {
		t.Fatal("a substantial task dispatched a worker before its before checkpoint")
	}
	const refusal = "adjudicate and attach the before checkpoint before dispatching workers"
	if text := err.Error() + out.String() + stderr.String(); !strings.Contains(text, refusal) {
		t.Fatalf("refusal does not say %q: %s", refusal, text)
	}
	stored, err := taskflow.Load(record.Directory)
	if err != nil || len(stored.Assignments) != 0 {
		t.Fatalf("rejected assignment changed the task: %v", err)
	}
}
