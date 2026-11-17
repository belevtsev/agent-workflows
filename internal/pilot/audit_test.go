package pilot

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

var pilotAuditDir = flag.String("pilot-audit-dir", "", "private posthoc facts and semantic reviews; never changes frozen execution inputs")

type auditCall struct {
	Line       int             `json:"line"`
	Time       string          `json:"time"`
	Name       string          `json:"name"`
	Namespace  string          `json:"namespace,omitempty"`
	CallID     string          `json:"call_id"`
	Input      string          `json:"input"`
	Arguments  json.RawMessage `json:"arguments,omitempty"`
	Output     string          `json:"output,omitempty"`
	OutputLine int             `json:"output_line,omitempty"`
	OutputTime string          `json:"output_time,omitempty"`
}

type auditEvent struct {
	Line int    `json:"line"`
	Time string `json:"time"`
	Type string `json:"type"`
	Role string `json:"role,omitempty"`
	Text string `json:"text,omitempty"`
}

type auditThread struct {
	Trace       asset           `json:"trace"`
	ID          string          `json:"id"`
	ParentID    string          `json:"parent_id,omitempty"`
	AgentPath   string          `json:"agent_path,omitempty"`
	Source      json.RawMessage `json:"source,omitempty"`
	HistoryMode string          `json:"history_mode,omitempty"`
	Models      []string        `json:"models"`
	Efforts     []string        `json:"efforts"`
	Stage       string          `json:"stage"`
	Calls       []*auditCall    `json:"calls"`
	Messages    []auditEvent    `json:"messages"`
	Lifecycle   []auditEvent    `json:"lifecycle"`
}

type auditSource struct {
	Path    string          `json:"path"`
	SHA256  string          `json:"sha256"`
	Bytes   int             `json:"bytes"`
	Content string          `json:"content,omitempty"`
	Record  json.RawMessage `json:"record,omitempty"`
}

type auditFacts struct {
	Version      int           `json:"version"`
	RunID        string        `json:"run_id"`
	Arm          string        `json:"assigned_arm"`
	Candidate    string        `json:"candidate_tree"`
	Result       asset         `json:"result"`
	Manifest     asset         `json:"frozen_manifest"`
	Attestation  asset         `json:"attestation"`
	CaseBinding  bool          `json:"case_binding"`
	BindingNotes []string      `json:"binding_notes"`
	Execution    *invocation   `json:"execution"`
	Threads      []auditThread `json:"threads"`
	Sources      []auditSource `json:"sources"`
	Limitations  []string      `json:"limitations"`
}

// posthocReview holds semantic judgments separately from mechanically collected
// facts. Reviewers must cite actual results: command text, an artifact count,
// or an author's claim does not prove an execution or an independent review.
type posthocReview struct {
	Version      int           `json:"version"`
	Facts        asset         `json:"facts"`
	Audit        protocolAudit `json:"audit"`
	Observations []string      `json:"observations"`
}

type protocolAuditRow struct {
	Run      cell           `json:"run"`
	State    string         `json:"audit_state"`
	Status   string         `json:"protocol_status"`
	Audit    *protocolAudit `json:"protocol,omitempty"`
	Review   *asset         `json:"semantic_review,omitempty"`
	Bindings *bool          `json:"case_binding,omitempty"`
}

type protocolAuditSummary struct {
	Version   int                `json:"version"`
	Manifest  asset              `json:"frozen_manifest"`
	Assigned  int                `json:"assigned"`
	Completed int                `json:"completed"`
	Audited   int                `json:"audited"`
	Statuses  map[string]int     `json:"protocol_status_counts"`
	Rows      []protocolAuditRow `json:"runs"`
	Limits    []string           `json:"limitations"`
}

// TestPilotCollectProtocolFacts runs only against completed immutable results.
// It never invokes Git, a model, or the frozen execution entry point. Solver
// metadata is untrusted data read through os.Root, not a host Git repository.
//
//	/tmp/pilot-audit.test -test.run=^TestPilotCollectProtocolFacts$ \
//	  -pilot-output=/private/pilot -pilot-audit-dir=/private/posthoc
func TestPilotCollectProtocolFacts(t *testing.T) {
	if *pilotAuditDir == "" || *pilotOutput == "" {
		t.Skip("explicit posthoc input and output required")
	}
	var frozen manifest
	var planned plan
	manifestPath := filepath.Join(*pilotOutput, "frozen-manifest.json")
	if err := readJSON(manifestPath, &frozen); err != nil {
		t.Fatal(err)
	}
	if err := readJSON(filepath.Join(*pilotOutput, "plan.json"), &planned); err != nil {
		t.Fatal(err)
	}
	digest, err := hashFile(manifestPath)
	if err != nil || digest != planned.ManifestSHA256 {
		t.Fatal("frozen manifest differs from plan", err)
	}
	if err := os.MkdirAll(filepath.Join(*pilotAuditDir, "facts"), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, selected := range planned.Runs {
		if *pilotRun != "all" && *pilotRun != "next" && *pilotRun != selected.ID {
			continue
		}
		runPath := filepath.Join(*pilotOutput, "runs", selected.ID)
		resultPath := filepath.Join(runPath, "result.json")
		if _, err := os.Stat(resultPath); errors.Is(err, fs.ErrNotExist) {
			continue
		}
		target := filepath.Join(*pilotAuditDir, "facts", selected.ID+".json")
		if _, err := os.Stat(target); err == nil {
			continue
		}
		var result runResult
		if err := readJSON(resultPath, &result); err != nil {
			t.Error(selected.ID, err)
			continue
		}
		facts, err := collectAuditFacts(*pilotOutput, frozen, planned, selected, result)
		if err != nil {
			t.Error(selected.ID, err)
			continue
		}
		if err := writeJSON(target, facts, true); err != nil {
			t.Fatal(err)
		}
		t.Logf("%s: %d traces, %d retained files; case binding %t; %s", selected.ID, len(facts.Threads), len(facts.Sources), facts.CaseBinding, target)
	}
}

// TestPilotApplyProtocolReviews publishes only separately authored reviews
// whose hashes and candidate identities still match the collected facts. It
// appends protocol.json and does not rewrite result.json or grade.json.
func TestPilotApplyProtocolReviews(t *testing.T) {
	if *pilotAuditDir == "" || *pilotOutput == "" {
		t.Skip("explicit posthoc input and output required")
	}
	paths, err := filepath.Glob(filepath.Join(*pilotAuditDir, "reviews", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range paths {
		var review posthocReview
		if err := readJSON(path, &review); err != nil {
			t.Error(path, err)
			continue
		}
		if *pilotRun != "all" && *pilotRun != "next" && *pilotRun != review.Audit.RunID {
			continue
		}
		if err := publishProtocolReview(*pilotOutput, *pilotAuditDir, path, review); err != nil {
			t.Error(path, err)
		}
	}
}

// TestPilotProtocolAuditSummary retains all assigned runs, including pending or
// incomplete attempts. It overlays posthoc protocol judgments only in memory;
// frozen scores and execution results remain byte-for-byte unchanged.
func TestPilotProtocolAuditSummary(t *testing.T) {
	if *pilotAuditDir == "" || *pilotOutput == "" {
		t.Skip("explicit posthoc input and output required")
	}
	var planned plan
	if err := readJSON(filepath.Join(*pilotOutput, "plan.json"), &planned); err != nil {
		t.Fatal(err)
	}
	digest, err := hashFile(filepath.Join(*pilotOutput, "frozen-manifest.json"))
	if err != nil || digest != planned.ManifestSHA256 {
		t.Fatal("frozen manifest differs from plan", err)
	}
	summary := protocolAuditSummary{Version: 1, Assigned: len(planned.Runs), Statuses: map[string]int{},
		Manifest: asset{filepath.Join(*pilotOutput, "frozen-manifest.json"), planned.ManifestSHA256},
		Limits: []string{
			"No functional score, token usage or blind patch assessment is changed by protocol auditing.",
			"Native encrypted assignment equality and findings/disposition content remain unknown unless independently observable; saved neutral briefs and actual tool reads are reported separately.",
			"Positive compact counters can be explicitly documented lower bounds proving uptake. They are not frequency measurements or counts of retained unique artifacts.",
			"Violated includes incomplete required workflow under the fixed runtime limit; consult per-run observations for the missing step rather than inferring bypass or deception.",
		}}
	for _, selected := range planned.Runs {
		row := protocolAuditRow{Run: selected, State: "pending", Status: "not_audited"}
		runDir := filepath.Join(*pilotOutput, "runs", selected.ID)
		var result runResult
		if err := readJSON(filepath.Join(runDir, "result.json"), &result); errors.Is(err, fs.ErrNotExist) {
			summary.Rows = append(summary.Rows, row)
			continue
		} else if err != nil {
			t.Fatal(err)
		}
		summary.Completed++
		row.State = "completed_unreviewed"
		var audit protocolAudit
		if err := readJSON(filepath.Join(runDir, "protocol.json"), &audit); errors.Is(err, fs.ErrNotExist) {
			summary.Rows = append(summary.Rows, row)
			continue
		} else if err != nil {
			t.Fatal(err)
		}
		if result.Method.Facts == nil {
			result.Method.Facts = map[string]any{}
		}
		if err := applyProtocolAudit(runDir, &result); err != nil {
			t.Fatal(err)
		}
		row.State, row.Status, row.Audit = "audited", result.Method.Status, &audit
		path := filepath.Join(*pilotAuditDir, "reviews", selected.ID+".json")
		var review posthocReview
		if err := readJSON(path, &review); err != nil {
			t.Fatal(err)
		}
		if err := publishProtocolReview(*pilotOutput, *pilotAuditDir, path, review); err != nil {
			t.Fatal(err)
		}
		digest, err := hashFile(path)
		if err != nil {
			t.Fatal(err)
		}
		row.Review = &asset{path, digest}
		var facts auditFacts
		if err := readJSON(review.Facts.Path, &facts); err != nil {
			t.Fatal(err)
		}
		row.Bindings = &facts.CaseBinding
		summary.Rows = append(summary.Rows, row)
		summary.Audited++
		summary.Statuses[row.Status]++
	}
	path := filepath.Join(*pilotAuditDir, "audit-summary.json")
	if err := writeJSON(path, summary, false); err != nil {
		t.Fatal(err)
	}
	t.Logf("assigned=%d completed=%d audited=%d statuses=%v; %s", summary.Assigned, summary.Completed, summary.Audited, summary.Statuses, path)
}

func auditBody(raw json.RawMessage) string {
	var body string
	if json.Unmarshal(raw, &body) == nil {
		return body
	}
	var parts []struct {
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &parts) != nil {
		return ""
	}
	var texts []string
	for _, part := range parts {
		if part.Text != "" {
			texts = append(texts, part.Text)
		}
	}
	return strings.Join(texts, "\n")
}

func readAuditTrace(path string) (auditThread, error) {
	thread := auditThread{Trace: asset{Path: path}, Stage: "coordinator"}
	digest, err := hashFile(path)
	if err != nil {
		return thread, err
	}
	thread.Trace.SHA256 = digest
	f, err := os.Open(path)
	if err != nil {
		return thread, err
	}
	defer f.Close()
	calls := map[string]*auditCall{}
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64<<10), 64<<20)
	for line := 1; scanner.Scan(); line++ {
		var event struct {
			Type      string `json:"type"`
			Timestamp string `json:"timestamp"`
			Payload   struct {
				Type        string          `json:"type"`
				ID          string          `json:"id"`
				ParentID    string          `json:"parent_thread_id"`
				AgentPath   string          `json:"agent_path"`
				Source      json.RawMessage `json:"source"`
				HistoryMode string          `json:"history_mode"`
				Model       string          `json:"model"`
				Effort      string          `json:"effort"`
				Name        string          `json:"name"`
				Namespace   string          `json:"namespace"`
				CallID      string          `json:"call_id"`
				Input       string          `json:"input"`
				Arguments   string          `json:"arguments"`
				Output      json.RawMessage `json:"output"`
				Content     json.RawMessage `json:"content"`
				Role        string          `json:"role"`
				Author      string          `json:"author"`
			} `json:"payload"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			return thread, fmt.Errorf("line %d: %w", line, err)
		}
		p := event.Payload
		switch event.Type {
		case "session_meta":
			thread.ID, thread.ParentID, thread.AgentPath, thread.Source, thread.HistoryMode = p.ID, p.ParentID, p.AgentPath, p.Source, p.HistoryMode
			if thread.ParentID == "" {
				var source any
				if json.Unmarshal(p.Source, &source) == nil {
					thread.ParentID = parentID(source)
				}
			}
			if thread.ParentID != "" {
				thread.Stage = "unknown"
				for _, stage := range []string{"tests", "code", "review", "commit"} {
					if strings.Contains(thread.AgentPath, "_"+stage+"_") {
						thread.Stage = stage
					}
				}
			}
		case "turn_context":
			if p.Model != "" && !slices.Contains(thread.Models, p.Model) {
				thread.Models = append(thread.Models, p.Model)
			}
			if p.Effort != "" && !slices.Contains(thread.Efforts, p.Effort) {
				thread.Efforts = append(thread.Efforts, p.Effort)
			}
		case "event_msg":
			if slices.Contains([]string{"task_started", "task_complete", "turn_aborted"}, p.Type) {
				thread.Lifecycle = append(thread.Lifecycle, auditEvent{Line: line, Time: event.Timestamp, Type: p.Type})
			}
		case "response_item":
			switch p.Type {
			case "custom_tool_call", "function_call":
				call := &auditCall{Line: line, Time: event.Timestamp, Name: p.Name, Namespace: p.Namespace, CallID: p.CallID, Input: p.Input}
				if p.Type == "function_call" {
					call.Input = p.Arguments
					if json.Valid([]byte(p.Arguments)) {
						call.Arguments = json.RawMessage(p.Arguments)
					}
				}
				thread.Calls = append(thread.Calls, call)
				calls[p.CallID] = call
			case "custom_tool_call_output", "function_call_output":
				if call := calls[p.CallID]; call != nil {
					call.Output, call.OutputLine, call.OutputTime = auditBody(p.Output), line, event.Timestamp
				}
			case "message", "agent_message":
				role := p.Role
				if role == "" {
					role = p.Author
				}
				thread.Messages = append(thread.Messages, auditEvent{Line: line, Time: event.Timestamp, Type: p.Type, Role: role, Text: auditBody(p.Content)})
			}
		}
	}
	return thread, scanner.Err()
}

func readAuditSource(root *os.Root, path string) (auditSource, error) {
	item := auditSource{Path: path}
	info, err := root.Lstat(path)
	if err != nil {
		return item, err
	}
	if !info.Mode().IsRegular() || info.Size() > 64<<20 {
		return item, fmt.Errorf("nonregular or oversized audit evidence: %s", path)
	}
	file, err := root.Open(path)
	if err != nil {
		return item, err
	}
	defer file.Close()
	info, err = file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > 64<<20 {
		return item, fmt.Errorf("nonregular or oversized audit evidence: %s", path)
	}
	data, err := io.ReadAll(io.LimitReader(file, (64<<20)+1))
	if err != nil {
		return item, fmt.Errorf("read evidence %s: %w", path, err)
	}
	if len(data) > 64<<20 {
		return item, fmt.Errorf("oversized audit evidence: %s", path)
	}
	digest := sha256.Sum256(data)
	item.SHA256, item.Bytes = hex.EncodeToString(digest[:]), len(data)
	if strings.HasSuffix(path, ".json") && json.Valid(data) {
		item.Record = json.RawMessage(data)
	} else {
		item.Content = string(data)
	}
	return item, nil
}

func collectAuditFacts(output string, frozen manifest, planned plan, selected cell, result runResult) (auditFacts, error) {
	runDir := filepath.Join(output, "runs", selected.ID)
	facts := auditFacts{Version: 1, RunID: selected.ID, Arm: selected.Arm, Candidate: result.CandidateTree, Execution: result.Execution,
		Result: asset{Path: filepath.Join(runDir, "result.json")}, Manifest: asset{Path: filepath.Join(output, "frozen-manifest.json")},
		Attestation: asset{Path: filepath.Join(output, "trusted-candidates", selected.ID+".json")}}
	for _, a := range []*asset{&facts.Result, &facts.Manifest} {
		digest, err := hashFile(a.Path)
		if err != nil {
			return facts, err
		}
		a.SHA256 = digest
	}
	facts.CaseBinding = result.Run == selected && result.ManifestSHA256 == planned.ManifestSHA256 && facts.Manifest.SHA256 == planned.ManifestSHA256
	var chosen *pilotCase
	for i := range frozen.Cases {
		if frozen.Cases[i].ID == selected.CaseID {
			chosen = &frozen.Cases[i]
		}
	}
	var attested exportReceipt
	if err := readJSON(facts.Attestation.Path, &attested); err != nil {
		facts.CaseBinding = false
		facts.BindingNotes = append(facts.BindingNotes, "trusted export attestation unavailable: "+err.Error())
	} else {
		digest, err := hashFile(facts.Attestation.Path)
		if err != nil {
			return facts, err
		}
		facts.Attestation.SHA256 = digest
		if chosen == nil || attested.Version != 1 || attested.RunID != selected.ID || attested.PublicSnapshotSHA256 != chosen.SnapshotSHA256 ||
			attested.BaselineTree != result.BaselineTree || attested.CandidateTree != result.CandidateTree ||
			attested.TrustedRepository != result.TrustedCandidate || attested.SourceWorktree != filepath.Join(runDir, "worktree") {
			facts.CaseBinding = false
			facts.BindingNotes = append(facts.BindingNotes, "case, source, or tree differs from trusted export attestation")
		}
	}
	if chosen != nil {
		for _, a := range []asset{frozen.CoordinatorPrompt, frozen.ObservationPrompts[selected.Arm], chosen.Prompt, result.Prompt} {
			digest, err := hashFile(a.Path)
			if err != nil || digest != a.SHA256 {
				facts.CaseBinding = false
				facts.BindingNotes = append(facts.BindingNotes, "prompt asset hash mismatch: "+a.Path)
			}
		}
		expected, err := promptFor(frozen, *chosen, selected.Arm)
		actual, readErr := os.ReadFile(result.Prompt.Path)
		if err != nil || readErr != nil || expected != string(actual) {
			facts.CaseBinding = false
			facts.BindingNotes = append(facts.BindingNotes, "run prompt differs from frozen assigned prompt")
		}
	}
	paths, err := filepath.Glob(filepath.Join(runDir, "traces", "*.jsonl"))
	if err != nil {
		return facts, err
	}
	for _, path := range paths {
		thread, err := readAuditTrace(path)
		if err != nil {
			facts.Limitations = append(facts.Limitations, path+": "+err.Error())
			continue
		}
		facts.Threads = append(facts.Threads, thread)
	}
	root, err := os.OpenRoot(filepath.Join(runDir, "worktree"))
	if err != nil {
		return facts, err
	}
	defer root.Close()
	for _, dir := range []string{".git/pilot-briefs", ".git/kitsu-context", ".git/kitsu/runs", "openspec/changes/.skills"} {
		err := fs.WalkDir(root.FS(), dir, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() || (dir != ".git/pilot-briefs" && !strings.HasSuffix(path, ".json")) {
				return nil
			}
			item, err := readAuditSource(root, path)
			if err != nil {
				facts.Limitations = append(facts.Limitations, path+": "+err.Error())
			} else {
				facts.Sources = append(facts.Sources, item)
			}
			return nil
		})
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			facts.Limitations = append(facts.Limitations, dir+": "+err.Error())
		}
	}
	for _, path := range []string{".git/config", ".git/HEAD", ".git/hooks/pre-commit", ".git/hooks/pre-push", ".git/kitsu/mode", ".git/logs/HEAD"} {
		item, err := readAuditSource(root, path)
		if err == nil {
			facts.Sources = append(facts.Sources, item)
		} else if !errors.Is(err, fs.ErrNotExist) {
			facts.Limitations = append(facts.Limitations, path+": "+err.Error())
		}
	}
	return facts, nil
}

func publishProtocolReview(output, auditDir, reviewPath string, review posthocReview) error {
	if review.Version != 1 || !slug.MatchString(review.Audit.RunID) {
		return errors.New("invalid posthoc review identity")
	}
	expected := filepath.Join(auditDir, "facts", review.Audit.RunID+".json")
	if review.Facts.Path != expected {
		return errors.New("review must cite this run's collected facts")
	}
	digest, err := hashFile(review.Facts.Path)
	if err != nil || digest != review.Facts.SHA256 {
		return errors.New("posthoc facts changed after review")
	}
	var facts auditFacts
	if err := readJSON(review.Facts.Path, &facts); err != nil {
		return err
	}
	if facts.RunID != review.Audit.RunID || facts.Candidate != review.Audit.CandidateTree || facts.Arm != review.Audit.AssignedArm {
		return errors.New("semantic review differs from observed run or candidate")
	}
	if !facts.CaseBinding && (review.Audit.Compliant == nil || *review.Audit.Compliant) {
		return errors.New("case binding failure must remain a recorded violation")
	}
	if len(review.Observations) == 0 || len(review.Audit.EvidencePaths) == 0 {
		return errors.New("semantic review needs observations and evidence references")
	}
	runDir := filepath.Join(output, "runs", facts.RunID)
	var result runResult
	if err := readJSON(filepath.Join(runDir, "result.json"), &result); err != nil {
		return err
	}
	current, err := hashFile(facts.Result.Path)
	if err != nil || current != facts.Result.SHA256 || result.CandidateTree != facts.Candidate {
		return errors.New("immutable result changed after facts were collected")
	}
	for _, source := range facts.Threads {
		current, err := hashFile(source.Trace.Path)
		if err != nil || current != source.Trace.SHA256 {
			return errors.New("retained trace changed after review")
		}
	}
	path := filepath.Join(runDir, "protocol.json")
	if data, err := os.ReadFile(path); err == nil {
		var existing protocolAudit
		if json.Unmarshal(data, &existing) != nil {
			return errors.New("existing protocol audit is invalid")
		}
		before, _ := json.Marshal(existing)
		after, _ := json.Marshal(review.Audit)
		if string(before) != string(after) {
			return errors.New("existing protocol audit differs; retain a separately versioned correction")
		}
		return nil
	}
	if !slices.Contains(review.Audit.EvidencePaths, reviewPath) || !slices.Contains(review.Audit.EvidencePaths, review.Facts.Path) {
		return errors.New("protocol evidence must include semantic review and content-bound facts")
	}
	return writeJSON(path, review.Audit, true)
}

func TestAuditTraceJoinsAllOutputPartsWithoutCountingMentions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "trace.jsonl")
	data := `{"type":"session_meta","payload":{"id":"root","source":"exec"}}
{"type":"response_item","payload":{"type":"custom_tool_call","name":"exec","call_id":"one","input":"text('Do not use kitsu context collect');"}}
{"type":"response_item","payload":{"type":"custom_tool_call_output","call_id":"one","output":[{"text":"header"},{"text":"only a mention"}]}}
`
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	thread, err := readAuditTrace(path)
	if err != nil || len(thread.Calls) != 1 || thread.Calls[0].Output != "header\nonly a mention" {
		t.Fatalf("lost observed output: %+v; %v", thread, err)
	}
	// The collector deliberately returns raw facts; a textual mention does not
	// set an execution count or certify protocol compliance.
	if thread.Calls[0].OutputLine != 3 || thread.Stage != "coordinator" {
		t.Fatal(thread)
	}
}

func TestAuditSourceCannotEscapeThroughSolverGitMetadata(t *testing.T) {
	dir := t.TempDir()
	secret := filepath.Join(t.TempDir(), "private-grader")
	if err := os.WriteFile(secret, []byte("private"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Dir(secret), filepath.Join(dir, ".git")); err != nil {
		t.Skip("symlinks unavailable", err)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if _, err := readAuditSource(root, ".git/private-grader"); err == nil {
		t.Fatal("untrusted metadata escaped the completed solver workspace")
	}
}

func TestAuditReviewBindsFactsAndKeepsImmutableResult(t *testing.T) {
	output, audits := t.TempDir(), t.TempDir()
	runID := "case-r1-normal"
	runDir := filepath.Join(output, "runs", runID)
	resultPath := filepath.Join(runDir, "result.json")
	result := runResult{Run: cell{ID: runID, Arm: "normal"}, CandidateTree: "candidate"}
	if err := writeJSON(resultPath, result, true); err != nil {
		t.Fatal(err)
	}
	resultHash, err := hashFile(resultPath)
	if err != nil {
		t.Fatal(err)
	}
	facts := auditFacts{Version: 1, RunID: runID, Arm: "normal", Candidate: "candidate", CaseBinding: true, Result: asset{resultPath, resultHash}}
	factsPath := filepath.Join(audits, "facts", runID+".json")
	if err := writeJSON(factsPath, facts, true); err != nil {
		t.Fatal(err)
	}
	factsHash, err := hashFile(factsPath)
	if err != nil {
		t.Fatal(err)
	}
	reviewPath := filepath.Join(audits, "reviews", runID+".json")
	review := posthocReview{Version: 1, Facts: asset{factsPath, factsHash}, Observations: []string{"missing assignment contents remain unknown"},
		Audit: protocolAudit{Version: 1, RunID: runID, CandidateTree: "candidate", AssignedArm: "normal", EvidencePaths: []string{reviewPath, factsPath}}}
	changed := review
	changed.Audit.CandidateTree = "other"
	if err := publishProtocolReview(output, audits, reviewPath, changed); err == nil {
		t.Fatal("review certified other content")
	}
	changed = review
	changed.Facts.SHA256 = strings.Repeat("0", 64)
	if err := publishProtocolReview(output, audits, reviewPath, changed); err == nil {
		t.Fatal("review certified changed facts")
	}
	if err := publishProtocolReview(output, audits, reviewPath, review); err != nil {
		t.Fatal(err)
	}
	after, err := hashFile(resultPath)
	if err != nil || after != resultHash {
		t.Fatal("posthoc audit rewrote immutable result", err)
	}
	var protocol protocolAudit
	if err := readJSON(filepath.Join(runDir, "protocol.json"), &protocol); err != nil || protocol.IndependentReview != nil {
		t.Fatal("unknown judgment was changed into a claim", err)
	}
}
