package advisers

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

func recordsConfig() *Config {
	return &Config{
		Advisers: map[string]Definition{
			"wise": {Model: "configured-model", Command: []string{"fake-adviser", "{model}"}, Timeout: time.Minute},
		},
		Access:      map[string][]string{"default": {"wise"}},
		Checkpoints: []Checkpoint{{ID: "before-commit", Stage: "review", Role: "coordinator", Advisers: []string{"wise"}}},
	}
}

func recordsBind(t *testing.T, root, workflow string, cfg *Config) *Binding {
	t.Helper()
	binding, err := Bind(root, "sample-task", workflow, cfg, false)
	if err != nil {
		t.Fatal(err)
	}
	return binding
}

func recordsRequest(stage, checkpoint string) Consultation {
	return Consultation{
		Task: "sample-task", Role: "coordinator", Stage: stage, Adviser: "wise", Checkpoint: checkpoint,
		Content: Content{Spec: "spec-a", Design: "design-a", Code: "code-a"},
		Request: Request{Version: 1, Kind: "advice", Question: "What should change?"}, Prompt: "Rendered question and explicit context.",
	}
}

func recordsBegin(t *testing.T, root, stage, checkpoint string) *Consultation {
	t.Helper()
	consultation, err := Begin(root, recordsRequest(stage, checkpoint))
	if err != nil {
		t.Fatal(err)
	}
	return consultation
}

func recordsResult(consultation Consultation, outcome, answer string) Consultation {
	consultation.Execution = Execution{
		Outcome: outcome, Answer: answer, ActualModel: "unknown", StartedAt: time.Now().UTC(), EndedAt: time.Now().UTC(),
	}
	return consultation
}

func recordsWriteJSON(t *testing.T, file string, value any) {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(file, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestRecordBindingIsExplicitAndContainsNoCommand(t *testing.T) {
	root := t.TempDir()
	cfg := recordsConfig()
	cfg.Advisers["wise"] = Definition{
		Model: "configured-model", Command: []string{"private-program", "{model}", "private-token"}, Timeout: time.Minute,
		Source: "/private/config",
	}
	first := recordsBind(t, root, "small", cfg)
	cfg.Advisers["wise"] = Definition{Model: "new-model", Command: []string{"different", "{model}"}, Timeout: time.Minute}
	unchanged := recordsBind(t, root, "small", cfg)
	if first.ID != unchanged.ID || unchanged.Advisers["wise"].Model != "configured-model" {
		t.Fatalf("configuration drift changed an existing binding: %+v", unchanged)
	}
	refreshed, err := Bind(root, "sample-task", "small", cfg, true)
	if err != nil || refreshed.ID == first.ID || refreshed.Advisers["wise"].Model != "new-model" {
		t.Fatalf("refresh = %+v, %v", refreshed, err)
	}
	rel, err := Location(root, "sample-task")
	if err != nil || rel != "openspec/changes/.advice/sample-task" {
		t.Fatalf("Location = %q, %v", rel, err)
	}
	for _, id := range []string{first.ID, refreshed.ID} {
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel), "bindings", id+".json"))
		if err != nil {
			t.Fatal(err)
		}
		for _, forbidden := range []string{"private-program", "private-token", "/private/config", "command", "timeout"} {
			if strings.Contains(string(data), forbidden) {
				t.Fatalf("binding retained private definition detail %q", forbidden)
			}
		}
	}
	cfg.Access["default"][0] = "changed-after-binding"
	loaded, err := LoadBinding(root, "sample-task")
	if err != nil || !slices.Equal(loaded.Access["default"], []string{"wise"}) {
		t.Fatalf("snapshot alias = %+v, %v", loaded, err)
	}
}

func TestRecordReadersDoNotCreateOrNeedConfiguration(t *testing.T) {
	root := t.TempDir()
	if _, err := LoadBinding(root, "sample-task"); !errors.Is(err, ErrNoBinding) {
		t.Fatalf("LoadBinding = %v", err)
	}
	if _, err := Location(root, "sample-task"); !errors.Is(err, ErrNoBinding) {
		t.Fatalf("Location = %v", err)
	}
	if _, err := Evaluate(root, "sample-task", Content{}, "", ""); !errors.Is(err, ErrNoBinding) {
		t.Fatalf("Evaluate = %v", err)
	}
	if problems, err := ValidateRecords(root, "sample-task"); err != nil || len(problems) != 0 {
		t.Fatalf("ValidateRecords = %v, %v", problems, err)
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 0 {
		t.Fatalf("read-only operations wrote files: %v, %v", entries, err)
	}
	recordsBind(t, root, "small", recordsConfig())
	if err = os.WriteFile(filepath.Join(root, FileName), []byte("invalid TOML ["), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err = Evaluate(root, "sample-task", Content{Code: "code-a"}, "", ""); err != nil {
		t.Fatalf("portable evaluation loaded local configuration: %v", err)
	}
}

func TestRecordsKeepPendingRequestsOriginalAnswersAndEveryDisposition(t *testing.T) {
	root := t.TempDir()
	recordsBind(t, root, "small", recordsConfig())
	request := recordsRequest("review", "before-commit")
	request.Request = Request{
		Version: 1, Kind: "decision", Question: "Which approach?",
		Choices: []Choice{{ID: "minimal", Text: "Small change"}, {ID: "general", Text: "General change"}},
	}
	pending, err := Begin(root, request)
	if err != nil {
		t.Fatal(err)
	}
	consultations, err := ReadConsultations(root, "sample-task")
	if err != nil || len(consultations) != 1 || consultations[0].Execution.Outcome != "pending" || consultations[0].Prompt != request.Prompt {
		t.Fatalf("pending records = %+v, %v", consultations, err)
	}
	answer := "Choose minimal.\nOriginal answer stays exactly as returned.\n"
	finished := recordsResult(*pending, "success", answer)
	if err = Finish(root, finished); err != nil {
		t.Fatal(err)
	}
	changed := finished
	changed.Execution.Answer = "rewritten"
	if err = Finish(root, changed); !errors.Is(err, fs.ErrExist) {
		t.Fatalf("rewriting a result = %v", err)
	}
	for _, bad := range []struct{ disposition, reason, choice string }{
		{"accept", "", "minimal"}, {"reject", " \n", ""}, {"defer", "", ""},
		{"unknown", "because", ""}, {"accept", "because", "invented-choice"},
	} {
		if _, err = Resolve(root, "sample-task", pending.ID, bad.disposition, bad.reason, bad.choice); err == nil {
			t.Fatalf("invalid disposition was accepted: %+v", bad)
		}
	}
	first, err := Resolve(root, "sample-task", pending.ID, "accept", "Keeps the change small.", "minimal")
	if err != nil {
		t.Fatal(err)
	}
	last, err := Resolve(root, "sample-task", pending.ID, "reject", "New evidence favors the general approach.", "general")
	if err != nil {
		t.Fatal(err)
	}
	decisions, err := ReadDispositions(root, "sample-task")
	if err != nil || len(decisions) != 2 || decisions[0].ID != first.ID || decisions[1].ID != last.ID {
		t.Fatalf("decision history = %+v, %v", decisions, err)
	}
	r, err := openRecordFS(root)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	history, err := r.history("sample-task")
	if err != nil || history.dispositions[pending.ID].ID != last.ID {
		t.Fatalf("latest disposition = %+v, %v", history, err)
	}
	consultations, err = ReadConsultations(root, "sample-task")
	if err != nil || consultations[0].Execution.Answer != answer {
		t.Fatalf("original answer changed: %+v, %v", consultations, err)
	}
	problems, err := ValidateRecords(root, "sample-task")
	if err != nil || len(problems) != 0 {
		t.Fatalf("valid records = %v, %v", problems, err)
	}
}

func TestRecordMutationCannotChangeAnOriginalRequest(t *testing.T) {
	root := t.TempDir()
	recordsBind(t, root, "small", recordsConfig())
	pending := recordsBegin(t, root, "review", "before-commit")
	finished := recordsResult(*pending, "success", "Answer")
	finished.Request.Question = "Edited question"
	if err := Finish(root, finished); err == nil {
		t.Fatal("Finish changed the original request")
	}
	if _, err := Begin(root, *pending); err == nil {
		t.Fatal("Begin replaced an existing request")
	}
	if _, err := Resolve(root, "sample-task", "missing", "defer", "No result yet", ""); err == nil {
		t.Fatal("Resolve accepted an unknown consultation")
	}
}

func TestRecordPromotionMovesAllHistoryAndRejectsMerge(t *testing.T) {
	root := t.TempDir()
	first := recordsBind(t, root, "small", recordsConfig())
	pending := recordsBegin(t, root, "review", "before-commit")
	if err := Finish(root, recordsResult(*pending, "success", "Small history answer")); err != nil {
		t.Fatal(err)
	}
	if _, err := Resolve(root, "sample-task", pending.ID, "defer", "Discuss after promotion", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := Bind(root, "sample-task", "full", recordsConfig(), false); err == nil {
		t.Fatal("promotion succeeded without refresh")
	}
	full, small := recordLocations("sample-task")
	if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(full)), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := Bind(root, "sample-task", "full", recordsConfig(), true); err == nil {
		t.Fatal("promotion merged two history directories")
	}
	if err := os.Remove(filepath.Join(root, filepath.FromSlash(full))); err != nil {
		t.Fatal(err)
	}
	promoted, err := Bind(root, "sample-task", "full", recordsConfig(), true)
	if err != nil || promoted.Workflow != "full" || promoted.ID == first.ID {
		t.Fatalf("promotion = %+v, %v", promoted, err)
	}
	if _, err = os.Stat(filepath.Join(root, filepath.FromSlash(small))); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("old location remains: %v", err)
	}
	if _, err = os.Stat(filepath.Join(root, filepath.FromSlash(full), "bindings", first.ID+".json")); err != nil {
		t.Fatalf("old binding was lost: %v", err)
	}
	consultations, err := ReadConsultations(root, "sample-task")
	if err != nil || len(consultations) != 1 || consultations[0].Execution.Answer != "Small history answer" {
		t.Fatalf("promotion lost history: %+v, %v", consultations, err)
	}
	if decisions, err := ReadDispositions(root, "sample-task"); err != nil || len(decisions) != 1 {
		t.Fatalf("promotion lost dispositions: %+v, %v", decisions, err)
	}
	if _, err := Bind(root, "sample-task", "small", recordsConfig(), true); err == nil {
		t.Fatal("full history was demoted")
	}
}

func TestConcurrentRecordPublicationPreservesAllAttemptsAndOneResult(t *testing.T) {
	root := t.TempDir()
	recordsBind(t, root, "small", recordsConfig())
	const workers = 12
	results := make(chan error, workers)
	var group sync.WaitGroup
	for range workers {
		group.Go(func() {
			_, err := Begin(root, recordsRequest("review", "before-commit"))
			results <- err
		})
	}
	group.Wait()
	for range workers {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	consultations, err := ReadConsultations(root, "sample-task")
	if err != nil || len(consultations) != workers {
		t.Fatalf("concurrent attempts = %d, %v", len(consultations), err)
	}
	finished := recordsResult(consultations[0], "success", "Exactly one result")
	for range workers {
		group.Go(func() { results <- Finish(root, finished) })
	}
	group.Wait()
	successes := 0
	for range workers {
		err := <-results
		if err == nil {
			successes++
		} else if !errors.Is(err, fs.ErrExist) {
			t.Fatal(err)
		}
	}
	if successes != 1 {
		t.Fatalf("%d competing results were published", successes)
	}
}

func TestConcurrentBindingsAndDispositionsKeepImmutableHistory(t *testing.T) {
	root := t.TempDir()
	cfg := recordsConfig()
	const workers = 12
	bindings := make(chan *Binding, workers)
	errors := make(chan error, workers)
	var group sync.WaitGroup
	for range workers {
		group.Go(func() {
			binding, err := Bind(root, "sample-task", "small", cfg, false)
			bindings <- binding
			errors <- err
		})
	}
	group.Wait()
	firstID := ""
	for range workers {
		if err := <-errors; err != nil {
			t.Fatal(err)
		}
		binding := <-bindings
		if firstID == "" {
			firstID = binding.ID
		}
		if binding.ID != firstID {
			t.Fatal("initial binding was refreshed by a racing writer")
		}
	}
	pending := recordsBegin(t, root, "review", "before-commit")
	if err := Finish(root, recordsResult(*pending, "success", "Shared advice")); err != nil {
		t.Fatal(err)
	}
	for range workers {
		group.Go(func() {
			_, err := Resolve(root, "sample-task", pending.ID, "defer", "Concurrent coordinator rationale", "")
			errors <- err
		})
	}
	group.Wait()
	for range workers {
		if err := <-errors; err != nil {
			t.Fatal(err)
		}
	}
	decisions, err := ReadDispositions(root, "sample-task")
	if err != nil || len(decisions) != workers {
		t.Fatalf("concurrent decisions = %d, %v", len(decisions), err)
	}
	for i := 1; i < len(decisions); i++ {
		if !decisions[i].CreatedAt.After(decisions[i-1].CreatedAt) {
			t.Fatal("concurrent decisions do not retain an unambiguous chronological order")
		}
	}
}

func TestPendingConsultationCanFinishAfterPromotion(t *testing.T) {
	root := t.TempDir()
	recordsBind(t, root, "small", recordsConfig())
	pending := recordsBegin(t, root, "review", "before-commit")
	if _, err := Bind(root, "sample-task", "full", recordsConfig(), true); err != nil {
		t.Fatal(err)
	}
	if err := Finish(root, recordsResult(*pending, "success", "Answer from the in-flight call")); err != nil {
		t.Fatalf("promotion lost an in-flight request: %v", err)
	}
	if _, err := Resolve(root, "sample-task", pending.ID, "accept", "Applied after promotion", ""); err != nil {
		t.Fatal(err)
	}
	status, err := Evaluate(root, "sample-task", pending.Content, "", "")
	if err != nil || !status.Ready {
		t.Fatalf("promoted task lost its in-flight evidence: %+v, %v", status, err)
	}
}

func TestExplicitRefreshRecoversInterruptedPromotionWithoutLosingHistory(t *testing.T) {
	root := t.TempDir()
	first := recordsBind(t, root, "small", recordsConfig())
	pending := recordsBegin(t, root, "review", "before-commit")
	full, small := recordLocations("sample-task")
	if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(filepath.Dir(full))), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(root, filepath.FromSlash(small)), filepath.Join(root, filepath.FromSlash(full))); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadBinding(root, "sample-task"); err == nil {
		t.Fatal("an interrupted promotion should fail closed until refreshed")
	}
	if _, err := Bind(root, "sample-task", "full", recordsConfig(), false); err == nil {
		t.Fatal("an interrupted promotion was recovered without an explicit refresh")
	}
	recovered, err := Bind(root, "sample-task", "full", recordsConfig(), true)
	if err != nil || recovered.Workflow != "full" || recovered.ID == first.ID {
		t.Fatalf("promotion recovery = %+v, %v", recovered, err)
	}
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(full), "bindings", first.ID+".json")); err != nil {
		t.Fatalf("promotion recovery lost the original binding: %v", err)
	}
	if err := Finish(root, recordsResult(*pending, "success", "An answer retained across interruption")); err != nil {
		t.Fatal(err)
	}
	consultations, err := ReadConsultations(root, "sample-task")
	if err != nil || len(consultations) != 1 || consultations[0].Execution.Answer != "An answer retained across interruption" {
		t.Fatalf("promotion recovery lost an original attempt: %+v, %v", consultations, err)
	}
}

func TestRecordsMissingPointerAndUnsafeIDsFailClosed(t *testing.T) {
	root := t.TempDir()
	recordsBind(t, root, "small", recordsConfig())
	for _, id := range []string{"../other", ".tmp-hidden", ".", "..", "/absolute", "trailing.", "UPPERCASE"} {
		request := recordsRequest("review", "before-commit")
		request.ID = id
		if _, err := Begin(root, request); err == nil {
			t.Fatalf("unsafe consultation id %q was accepted", id)
		}
	}
	if _, err := LoadBinding(root, "../other"); err == nil {
		t.Fatal("unsafe task name was accepted")
	}
	rel, err := Location(root, "sample-task")
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Remove(filepath.Join(root, filepath.FromSlash(rel), "binding.json")); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadBinding(root, "sample-task"); !errors.Is(err, ErrNoBinding) {
		t.Fatalf("missing binding pointer = %v", err)
	}
	if problems, err := ValidateRecords(root, "sample-task"); err != nil || len(problems) == 0 {
		t.Fatalf("orphaned history was accepted: %v, %v", problems, err)
	}
}

func TestRecordsRetainMaximumEscapedPromptAndAnswer(t *testing.T) {
	root := t.TempDir()
	recordsBind(t, root, "small", recordsConfig())
	request := recordsRequest("review", "before-commit")
	request.Prompt = strings.Repeat("\x00", MaxInput)
	pending, err := Begin(root, request)
	if err != nil {
		t.Fatalf("maximum prompt was not persisted: %v", err)
	}
	answer := strings.Repeat("\x00", MaxAnswer)
	if err = Finish(root, recordsResult(*pending, "success", answer)); err != nil {
		t.Fatalf("maximum answer was not persisted: %v", err)
	}
	consultations, err := ReadConsultations(root, "sample-task")
	if err != nil || consultations[0].Prompt != request.Prompt || consultations[0].Execution.Answer != answer {
		t.Fatalf("bounded escaped content was changed: %v", err)
	}
}

func TestRecordsRejectMalformedAndForgedRecords(t *testing.T) {
	for _, test := range []string{"unknown binding", "wrong task", "wrong role", "wrong identity", "unknown field", "duplicate key", "unfinished result"} {
		t.Run(test, func(t *testing.T) {
			root := t.TempDir()
			recordsBind(t, root, "small", recordsConfig())
			pending := recordsBegin(t, root, "review", "before-commit")
			rel, err := Location(root, "sample-task")
			if err != nil {
				t.Fatal(err)
			}
			requestFile := filepath.Join(root, filepath.FromSlash(rel), "exchanges", pending.ID, "request.json")
			switch test {
			case "unknown binding":
				pending.BindingID = "missing"
			case "wrong task":
				pending.Task = "other-task"
			case "wrong role":
				pending.Role = "developer"
			case "wrong identity":
				pending.Identity.Model = "other-model"
			case "unfinished result":
				recordsWriteJSON(t, filepath.Join(filepath.Dir(requestFile), "result.json"), pending.Execution)
			}
			recordsWriteJSON(t, requestFile, pending)
			if test == "unknown field" || test == "duplicate key" {
				data, err := os.ReadFile(requestFile)
				if err != nil {
					t.Fatal(err)
				}
				field := `"unrecognized":true,`
				if test == "duplicate key" {
					field = `"version":1,`
				}
				if err := os.WriteFile(requestFile, []byte("{"+field+string(data[1:])), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			problems, err := ValidateRecords(root, "sample-task")
			if err != nil || len(problems) == 0 {
				t.Fatalf("malformed record accepted: %v, %v", problems, err)
			}
			if _, err = Evaluate(root, "sample-task", pending.Content, "", ""); err == nil {
				t.Fatal("checkpoint evaluation accepted malformed records")
			}
		})
	}
}

func TestRecordsRejectSymlinkHistoriesAndFiles(t *testing.T) {
	for _, component := range []string{"history", "request", "result", "binding"} {
		t.Run(component, func(t *testing.T) {
			root, outside := t.TempDir(), t.TempDir()
			recordsBind(t, root, "small", recordsConfig())
			pending := recordsBegin(t, root, "review", "before-commit")
			rel, err := Location(root, "sample-task")
			if err != nil {
				t.Fatal(err)
			}
			target := filepath.Join(root, filepath.FromSlash(rel))
			switch component {
			case "request", "result":
				target = filepath.Join(target, "exchanges", pending.ID, component+".json")
			case "binding":
				target = filepath.Join(target, "binding.json")
			}
			if err = os.RemoveAll(target); err != nil {
				t.Fatal(err)
			}
			if err = os.Symlink(outside, target); err != nil {
				t.Skipf("symlinks unavailable: %v", err)
			}
			problems, err := ValidateRecords(root, "sample-task")
			if err != nil || len(problems) == 0 {
				t.Fatalf("symlink accepted: %v, %v", problems, err)
			}
			if _, err = Begin(root, recordsRequest("review", "before-commit")); err == nil && component != "request" && component != "result" {
				t.Fatal("unsafe history accepted a write")
			}
			entries, err := os.ReadDir(outside)
			if err != nil || len(entries) != 0 {
				t.Fatalf("history escaped the repository: %v, %v", entries, err)
			}
		})
	}
}
