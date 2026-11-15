package advisers

import (
	"testing"
)

func TestCheckpointsDistinguishAttemptsFreshnessAndDisposition(t *testing.T) {
	for _, test := range []struct {
		name, outcome, answer, disposition, current, historical, state string
	}{
		{name: "missing", current: "code-a", state: "missing"},
		{name: "pending", outcome: "pending", current: "code-a", state: "pending"},
		{name: "failed", outcome: "failed", answer: "Partial answer", disposition: "defer", current: "code-a", state: "failed"},
		{name: "timed out", outcome: "timed_out", current: "code-a", state: "timed_out"},
		{name: "cancelled", outcome: "cancelled", current: "code-a", state: "cancelled"},
		{name: "overflow", outcome: "overflow", answer: "Truncated answer", current: "code-a", state: "overflow"},
		{name: "undisposed", outcome: "success", answer: "Advice", current: "code-a", state: "undisposed"},
		{name: "accepted", outcome: "success", answer: "Advice", disposition: "accept", current: "code-a", state: "satisfied"},
		{name: "rejected", outcome: "success", answer: "Advice", disposition: "reject", current: "code-a", state: "satisfied"},
		{name: "deferred", outcome: "success", answer: "Advice", disposition: "defer", current: "code-a", state: "satisfied"},
		{name: "stale", outcome: "success", answer: "Advice", disposition: "accept", current: "code-b", state: "stale"},
		{name: "committed content", outcome: "success", answer: "Advice", disposition: "accept", current: "code-b", historical: "code-a", state: "satisfied"},
		{name: "unrelated commit", outcome: "success", answer: "Advice", disposition: "accept", current: "code-b", historical: "code-c", state: "stale"},
		{name: "missing current", outcome: "success", answer: "Advice", disposition: "accept", state: "stale"},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			recordsBind(t, root, "small", recordsConfig())
			if test.outcome != "" {
				pending := recordsBegin(t, root, "review", "before-commit")
				if test.outcome != "pending" {
					if err := Finish(root, recordsResult(*pending, test.outcome, test.answer)); err != nil {
						t.Fatal(err)
					}
				}
				if test.disposition != "" {
					if _, err := Resolve(root, "sample-task", pending.ID, test.disposition, "Coordinator rationale", ""); err != nil {
						t.Fatal(err)
					}
				}
			}
			status, err := Evaluate(root, "sample-task", Content{Code: test.current}, test.historical, "before-commit")
			if err != nil {
				t.Fatal(err)
			}
			if len(status.Requirements) != 1 || status.Requirements[0].State != test.state {
				t.Fatalf("status = %+v; want %q", status, test.state)
			}
			if status.Ready != (test.state == "satisfied") {
				t.Fatalf("ready = %v for %q", status.Ready, test.state)
			}
		})
	}
}

func TestCheckpointsSmallSkipsSpecAndDesignButFullRequiresEveryAdviser(t *testing.T) {
	root := t.TempDir()
	cfg := recordsConfig()
	cfg.Advisers["second"] = cfg.Advisers["wise"]
	cfg.Access["default"] = []string{"wise", "second"}
	cfg.Checkpoints = []Checkpoint{
		{ID: "spec-choice", Stage: "spec", Role: "coordinator", Advisers: []string{"wise"}},
		{ID: "design-choice", Stage: "design", Role: "coordinator", Advisers: []string{"wise"}},
		{ID: "before-commit", Stage: "review", Role: "coordinator", Advisers: []string{"wise", "second"}},
	}
	recordsBind(t, root, "small", cfg)
	status, err := Evaluate(root, "sample-task", Content{}, "", "")
	if err != nil || len(status.Requirements) != 2 {
		t.Fatalf("small requirements = %+v, %v", status, err)
	}
	status, err = Evaluate(root, "sample-task", Content{}, "", "spec-choice")
	if err != nil || len(status.Requirements) != 0 || !status.Ready {
		t.Fatalf("small skipped checkpoint = %+v, %v", status, err)
	}
	if _, err = Bind(root, "sample-task", "full", cfg, true); err != nil {
		t.Fatal(err)
	}
	status, err = Evaluate(root, "sample-task", Content{}, "", "")
	if err != nil || len(status.Requirements) != 4 || status.Ready {
		t.Fatalf("full requirements = %+v, %v", status, err)
	}
	if problems, err := ValidateRecords(root, "sample-task"); err != nil || len(problems) != 0 {
		t.Fatalf("future checkpoints should not break record validation: %v, %v", problems, err)
	}
	for _, request := range []Consultation{
		recordsRequest("spec-review", "spec-choice"),
		recordsRequest("design-review", "design-choice"),
		recordsRequest("review", "before-commit"),
	} {
		pending, err := Begin(root, request)
		if err != nil {
			t.Fatal(err)
		}
		if err = Finish(root, recordsResult(*pending, "success", "Stage-specific advice")); err != nil {
			t.Fatal(err)
		}
		if _, err = Resolve(root, "sample-task", pending.ID, "accept", "Matches the task constraints", ""); err != nil {
			t.Fatal(err)
		}
	}
	content := Content{Spec: "spec-a", Design: "design-a", Code: "code-a"}
	status, err = Evaluate(root, "sample-task", content, "", "")
	if err != nil || status.Ready || status.Requirements[3].Adviser != "second" || status.Requirements[3].State != "missing" {
		t.Fatalf("each adviser must answer separately: %+v, %v", status, err)
	}
	status, err = Evaluate(root, "sample-task", Content{Spec: "spec-b", Design: "design-a", Code: "code-a"}, "", "spec-choice")
	if err != nil || status.Requirements[0].State != "stale" {
		t.Fatalf("spec freshness = %+v, %v", status, err)
	}
	status, err = Evaluate(root, "sample-task", Content{Spec: "spec-a", Design: "design-b", Code: "code-a"}, "", "design-choice")
	if err != nil || status.Requirements[0].State != "stale" {
		t.Fatalf("design freshness = %+v, %v", status, err)
	}
	if _, err = Evaluate(root, "sample-task", content, "", "unknown"); err == nil {
		t.Fatal("unknown checkpoint filter was accepted")
	}
}

func TestCheckpointsRequireExactRequesterStageAndAdviser(t *testing.T) {
	root := t.TempDir()
	cfg := recordsConfig()
	cfg.Access["developer"] = []string{}
	recordsBind(t, root, "small", cfg)
	for _, change := range []func(*Consultation){
		func(c *Consultation) { c.Role = "developer" },
		func(c *Consultation) { c.Stage = "code" },
		func(c *Consultation) { c.Stage = "spec-review" },
		func(c *Consultation) { c.Checkpoint = "unknown" },
		func(c *Consultation) { c.Adviser = "unknown" },
		func(c *Consultation) { c.Identity = Identity{Model: "forged", Fingerprint: "forged"} },
		func(c *Consultation) { c.BindingID = "unknown" },
	} {
		request := recordsRequest("review", "before-commit")
		change(&request)
		if _, err := Begin(root, request); err == nil {
			t.Fatalf("mismatched checkpoint request was accepted: %+v", request)
		}
	}
	if _, err := Begin(root, recordsRequest("lookup", "")); err != nil {
		t.Fatalf("optional role-allowed advice was rejected: %v", err)
	}
	request := recordsRequest("review", "")
	request.Role = "developer"
	if _, err := Begin(root, request); err == nil {
		t.Fatal("an explicit empty access override inherited default access")
	}
}

func TestCheckpointsRefreshReusesOnlyMatchingIdentityAndRequester(t *testing.T) {
	for _, test := range []string{"unchanged", "command", "model", "role", "stage", "checkpoint"} {
		t.Run(test, func(t *testing.T) {
			root := t.TempDir()
			cfg := recordsConfig()
			recordsBind(t, root, "small", cfg)
			pending := recordsBegin(t, root, "review", "before-commit")
			if err := Finish(root, recordsResult(*pending, "success", "Original advice")); err != nil {
				t.Fatal(err)
			}
			if _, err := Resolve(root, "sample-task", pending.ID, "accept", "Meets the constraints", ""); err != nil {
				t.Fatal(err)
			}
			expected := "missing"
			definition := cfg.Advisers["wise"]
			switch test {
			case "unchanged":
				expected = "satisfied"
			case "command":
				definition.Command = []string{"other-program", "{model}"}
				expected = "stale"
			case "model":
				definition.Model = "other-model"
				expected = "stale"
			case "role":
				cfg.Checkpoints[0].Role = "developer"
			case "stage":
				cfg.Checkpoints[0].Stage = "design"
			case "checkpoint":
				cfg.Checkpoints[0].ID = "other-checkpoint"
			}
			cfg.Advisers["wise"] = definition
			workflow := "small"
			if test == "stage" {
				workflow = "full"
			}
			if _, err := Bind(root, "sample-task", workflow, cfg, true); err != nil {
				t.Fatal(err)
			}
			status, err := Evaluate(root, "sample-task", pending.Content, "", "")
			if err != nil || len(status.Requirements) != 1 || status.Requirements[0].State != expected {
				t.Fatalf("refresh reused mismatched evidence: %+v, %v; want %q", status, err, expected)
			}
		})
	}
}

func TestCheckpointsKeepSuccessfulEvidenceAfterFailedRetries(t *testing.T) {
	root := t.TempDir()
	recordsBind(t, root, "small", recordsConfig())
	successful := recordsBegin(t, root, "review", "before-commit")
	if err := Finish(root, recordsResult(*successful, "success", "Actionable advice")); err != nil {
		t.Fatal(err)
	}
	if _, err := Resolve(root, "sample-task", successful.ID, "accept", "Implemented with a focused change", ""); err != nil {
		t.Fatal(err)
	}
	failed := recordsBegin(t, root, "review", "before-commit")
	if err := Finish(root, recordsResult(*failed, "failed", "")); err != nil {
		t.Fatal(err)
	}
	status, err := Evaluate(root, "sample-task", successful.Content, "", "")
	if err != nil || !status.Ready || status.Requirements[0].Consultation != successful.ID {
		t.Fatalf("failed retry hid successful evidence: %+v, %v", status, err)
	}
	consultations, err := ReadConsultations(root, "sample-task")
	if err != nil || len(consultations) != 2 {
		t.Fatalf("a retry removed an earlier attempt: %+v, %v", consultations, err)
	}
}

func TestCheckpointsDoNotTreatEmptyContentAsHistoricalEvidence(t *testing.T) {
	root := t.TempDir()
	recordsBind(t, root, "small", recordsConfig())
	request := recordsRequest("review", "before-commit")
	request.Content = Content{}
	pending, err := Begin(root, request)
	if err != nil {
		t.Fatal(err)
	}
	if err = Finish(root, recordsResult(*pending, "success", "Advice without identified content")); err != nil {
		t.Fatal(err)
	}
	if _, err = Resolve(root, "sample-task", pending.ID, "defer", "Wait for actual content", ""); err != nil {
		t.Fatal(err)
	}
	status, err := Evaluate(root, "sample-task", Content{}, "", "")
	if err != nil || status.Ready || status.Requirements[0].State != "stale" {
		t.Fatalf("empty historical identity counted as fresh: %+v, %v", status, err)
	}
}
