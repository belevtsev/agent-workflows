package taskflow

import (
	"maps"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/belevtsev/agent-workflows/internal/consult"
)

// limitationNaming reports whether one limitation line contains every word,
// case-insensitively.
func limitationNaming(limitations []string, words ...string) bool {
	return slices.ContainsFunc(limitations, func(line string) bool {
		lower := strings.ToLower(line)
		for _, word := range words {
			if !strings.Contains(lower, strings.ToLower(word)) {
				return false
			}
		}
		return true
	})
}

func implementationBrief() WorkerBrief {
	return workerBrief("implement", "/root/implementation", "implementation", []string{"source.txt"}, "focused")
}

func pendingDecision(draft *checkpointDraft) { draft.Pending = true }

func unresolvedDecision(draft *checkpointDraft) {
	draft.Items = map[string]decisionItem{"claim": fixtureUnresolvedItem()}
}

// D5 flow: for a substantial task, AssignWorker is refused with the D5 message
// until the before checkpoint is attached and resolved; the refusal writes
// nothing. A resolved checkpoint (with or without advisers) allows dispatch.
func TestSubstantialAssignWaitsForResolvedBeforeCheckpoint(t *testing.T) {
	for name, mutate := range map[string]func(*checkpointDraft){
		"not_attached": nil,
		"pending":      pendingDecision,
		"unresolved":   unresolvedDecision,
	} {
		t.Run(name, func(t *testing.T) {
			record, _ := fixtureTask(t, substantialPlan(t))
			if name != "not_attached" {
				attachCheckpoint(t, record, "before", record.Contexts["before"], mutate)
			}
			before := productionTaskBytes(t, record.Directory)
			_, err := AssignWorker(t.Context(), record.Directory, implementationBrief())
			if err == nil || !strings.Contains(err.Error(), fixtureAssignRefusal) {
				t.Fatalf("assignment before a resolved before checkpoint: %v", err)
			}
			if !reflect.DeepEqual(productionTaskBytes(t, record.Directory), before) {
				t.Fatal("refused assignment changed task files")
			}
		})
	}
	for name, binding := range map[string]map[string]string{"bound_advisers": fixtureBinding(), "no_advisers": {}} {
		t.Run("resolved_"+name, func(t *testing.T) {
			record, _ := fixtureTaskBound(t, substantialPlan(t), binding)
			attachCheckpoint(t, record, "before", record.Contexts["before"], nil)
			updated := mustAssignWorker(t, record, implementationBrief())
			if len(updated.Assignments) != 1 {
				t.Fatalf("assignment not recorded: %d", len(updated.Assignments))
			}
		})
	}
}

// D5 flow: small tasks skip checkpoints. The binding is stored but has no
// effect: dispatch needs no checkpoint, verify completes without consultations,
// and limitations never mention the bound providers.
func TestSmallTaskSkipsCheckpointsAndBindingHasNoEffect(t *testing.T) {
	for name, binding := range map[string]map[string]string{"bound_advisers": fixtureBinding(), "no_advisers": {}} {
		t.Run(name, func(t *testing.T) {
			expected := maps.Clone(binding)
			record, ref := fixtureTaskBound(t, helperPlan(t, "small", "pass"), binding)
			record = mustAssignWorker(t, record, workerBrief("reader", "/root/reader", "evidence", []string{"source.txt"}))
			mustAssess(t, record, ref)
			record = mustSubmitWorker(t, record, workerResult(t, record, "reader", "completed", ref))
			mustAcceptWorker(t, record, workerAcceptance(t, record, "reader", "accepted", ref))
			verified, err := Verify(t.Context(), record.Directory)
			if err != nil || !verified.Complete {
				t.Fatalf("small task without checkpoints: %+v %v", verified, err)
			}
			for provider := range expected {
				if limitationNaming(verified.Limitations, provider) {
					t.Fatalf("small task limitations use the binding (%s): %q", provider, verified.Limitations)
				}
			}
			stored, err := Load(record.Directory)
			if err != nil || len(stored.Consultations) != 0 || !maps.Equal(stored.Advisers, expected) {
				t.Fatalf("small task: consultations=%d advisers=%v err=%v", len(stored.Consultations), stored.Advisers, err)
			}
		})
	}
}

// D5 flow: the final context needs the before checkpoint attached and resolved.
func TestFinalContextNeedsResolvedBeforeCheckpoint(t *testing.T) {
	for name, mutate := range map[string]func(*checkpointDraft){
		"not_attached": nil,
		"pending":      pendingDecision,
		"unresolved":   unresolvedDecision,
	} {
		t.Run(name, func(t *testing.T) {
			record, ref := fixtureTask(t, substantialPlan(t))
			if name != "not_attached" {
				attachCheckpoint(t, record, "before", record.Contexts["before"], mutate)
			}
			mustAssess(t, record, ref)
			if _, err := PrepareContext(t.Context(), record.Directory, "final"); err == nil {
				t.Fatal("final context emitted without a resolved before checkpoint")
			}
		})
	}
	t.Run("resolved", func(t *testing.T) {
		record, ref := fixtureTask(t, substantialPlan(t))
		attachCheckpoint(t, record, "before", record.Contexts["before"], nil)
		mustAssess(t, record, ref)
		if _, err := PrepareContext(t.Context(), record.Directory, "final"); err != nil {
			t.Fatalf("resolved before checkpoint did not open the final context: %v", err)
		}
	})
}

// D5 checkpoint completeness: verify reports incomplete while the attached
// final decision is pending or unresolved, and completes once it is resolved.
func TestVerifyRequiresResolvedFinalDecision(t *testing.T) {
	for name, mutate := range map[string]func(*checkpointDraft){
		"pending":    pendingDecision,
		"unresolved": unresolvedDecision,
		"resolved":   nil,
	} {
		t.Run(name, func(t *testing.T) {
			record, ref := fixtureTask(t, substantialPlan(t))
			attachCheckpoint(t, record, "before", record.Contexts["before"], nil)
			mustAssess(t, record, ref)
			final, err := PrepareContext(t.Context(), record.Directory, "final")
			if err != nil {
				t.Fatal(err)
			}
			attachCheckpoint(t, record, "final", final, mutate)
			verified, err := Verify(t.Context(), record.Directory)
			if name == "resolved" {
				if err != nil || !verified.Complete {
					t.Fatalf("resolved final checkpoint: %+v %v", verified, err)
				}
				return
			}
			if err == nil || verified.Complete {
				t.Fatalf("%s final decision verified complete: %+v %v", name, verified, err)
			}
		})
	}
}

// D5: a before record can never satisfy final, whether the same artifact, a
// fresh artifact for the before context, or a before context relabelled final.
func TestBeforeRecordCannotSatisfyFinal(t *testing.T) {
	record, ref := fixtureTask(t, substantialPlan(t))
	beforeContext := record.Contexts["before"]
	beforePath := writeConsultation(t, beforeContext, nil)
	if _, err := AttachConsultation(t.Context(), record.Directory, "before", beforePath); err != nil {
		t.Fatal(err)
	}
	mustAssess(t, record, ref)
	final, err := PrepareContext(t.Context(), record.Directory, "final")
	if err != nil {
		t.Fatal(err)
	}
	relabelled := beforeContext
	relabelled.Phase = "final"
	for name, path := range map[string]string{
		"same_artifact":         beforePath,
		"fresh_before_artifact": writeConsultation(t, beforeContext, nil),
		"relabelled_before":     writeConsultation(t, relabelled, nil),
	} {
		if _, err := AttachConsultation(t.Context(), record.Directory, "final", path); err == nil {
			t.Fatalf("%s satisfied the final checkpoint", name)
		}
	}
	if verified, err := Verify(t.Context(), record.Directory); err == nil || verified.Complete {
		t.Fatal("before record completed the task")
	}
	attachCheckpoint(t, record, "final", final, nil)
	if verified, err := Verify(t.Context(), record.Directory); err != nil || !verified.Complete {
		t.Fatalf("final checkpoint: %+v %v", verified, err)
	}
}

// completedLimitations completes a substantial task with the given checkpoint
// outcomes and returns verify's limitations.
func completedLimitations(t *testing.T, binding map[string]string, before, final func(*checkpointDraft)) []string {
	t.Helper()
	record, ref := fixtureTaskBound(t, substantialPlan(t), binding)
	attachCheckpoint(t, record, "before", record.Contexts["before"], before)
	mustAssess(t, record, ref)
	finalContext, err := PrepareContext(t.Context(), record.Directory, "final")
	if err != nil {
		t.Fatal(err)
	}
	attachCheckpoint(t, record, "final", finalContext, final)
	verified, err := Verify(t.Context(), record.Directory)
	if err != nil || !verified.Complete {
		t.Fatalf("checkpointed task did not complete: %+v %v", verified, err)
	}
	return verified.Limitations
}

// D5 verify limitations: non-success coverage is listed per bound provider
// with no hardcoded provider IDs; an empty binding states the D5 sentence.
func TestVerifyLimitationsNameNonSuccessCoveragePerBoundProvider(t *testing.T) {
	agree := func(providers ...string) func(*checkpointDraft) {
		outcomes := map[string]adviserOutcome{}
		for _, provider := range providers {
			outcomes[provider] = adviserOutcome{Status: "success", Choices: map[string]string{"claim": "supported"}}
		}
		return withOutcomes(outcomes)
	}
	t.Run("shipped_default", func(t *testing.T) {
		limitations := completedLimitations(t, fixtureBinding(),
			withOutcomes(map[string]adviserOutcome{"subq_snap": {Status: "unavailable", Category: "timeout"}, "openai_decisions": {Status: "refusal"}}),
			agree("subq_snap", "openai_decisions"))
		if !limitationNaming(limitations, "subq_snap", "unavailable") {
			t.Fatalf("limitations omit subq_snap unavailability: %q", limitations)
		}
		if !limitationNaming(limitations, "openai_decisions", "refus") {
			t.Fatalf("limitations omit openai_decisions refusal: %q", limitations)
		}
		if limitationNaming(limitations, "jev") {
			t.Fatalf("limitations name the unbound jev (hardcoded IDs): %q", limitations)
		}
		if limitationNaming(limitations, fixtureNoAdvisersLimitation) {
			t.Fatalf("bound task reports no advisers: %q", limitations)
		}
	})
	t.Run("opt_in_jev_only", func(t *testing.T) {
		limitations := completedLimitations(t, map[string]string{"jev": "jev-1.14.0"},
			withOutcomes(map[string]adviserOutcome{"jev": {Status: "unavailable", Category: "http_error", HTTPStatus: 503}}),
			agree("jev"))
		if !limitationNaming(limitations, "jev", "unavailable") {
			t.Fatalf("limitations omit jev unavailability: %q", limitations)
		}
		for _, unbound := range []string{"subq_snap", "openai_decisions"} {
			if limitationNaming(limitations, unbound) {
				t.Fatalf("limitations name the unbound %s (hardcoded IDs): %q", unbound, limitations)
			}
		}
	})
	t.Run("no_advisers", func(t *testing.T) {
		limitations := completedLimitations(t, map[string]string{}, nil, nil)
		if !slices.ContainsFunc(limitations, func(line string) bool { return strings.Contains(line, fixtureNoAdvisersLimitation) }) {
			t.Fatalf("limitations omit the D5 no-advisers statement: %q", limitations)
		}
		for _, provider := range fixtureProviderOrder {
			if limitationNaming(limitations, provider) {
				t.Fatalf("no-advisers limitations name %s: %q", provider, limitations)
			}
		}
	})
}

// D4 through attach for a no-advisers checkpoint: a resolved decision still
// needs typed source/check/observation evidence and verification; untyped or
// adviser-only entries are rejected by consult.ValidateRecord and at attach,
// and nothing is attached until a valid record arrives.
func TestNoAdvisersCheckpointStillNeedsTypedEvidenceAndVerification(t *testing.T) {
	record, _ := fixtureTaskBound(t, substantialPlan(t), map[string]string{})
	context := record.Contexts["before"]
	source := fixtureSourceEvidence()
	invalid := map[string]func(*decisionItem){
		"untyped_evidence":       func(item *decisionItem) { item.Evidence = []string{"source.txt:1 at exact snapshot"} },
		"untyped_verification":   func(item *decisionItem) { item.Verification = []string{"focused check at exact snapshot"} },
		"adviser_observation":    func(item *decisionItem) { item.Evidence = []string{"observation:Snap agreed with this claim"} },
		"adviser_id_observation": func(item *decisionItem) { item.Verification = []string{"observation:subq_snap chose supported"} },
		"advisor_word_observation": func(item *decisionItem) {
			item.Verification = []string{"observation:The Advisor output was convincing"}
		},
		"openai_decisions_phrase":    func(item *decisionItem) { item.Evidence = []string{"observation:OpenAI Decisions chose supported"} },
		"typesafe_observation":       func(item *decisionItem) { item.Evidence = []string{"observation:TypeSafe agreed"} },
		"blank_observation":          func(item *decisionItem) { item.Evidence = []string{"observation: "} },
		"no_evidence":                func(item *decisionItem) { item.Evidence = []string{} },
		"no_verification":            func(item *decisionItem) { item.Verification = []string{} },
		"insufficient_evidence":      func(item *decisionItem) { item.Choice = "insufficient_evidence" },
		"unknown_option":             func(item *decisionItem) { item.Choice = "maybe" },
		"blank_reason":               func(item *decisionItem) { item.Reason = " \t" },
		"escaping_source_path":       func(item *decisionItem) { item.Evidence = []string{"source:../outside.txt:1@0123abc"} },
		"symbolic_source_revision":   func(item *decisionItem) { item.Evidence = []string{"source:source.txt:1@HEAD"} },
		"short_source_revision":      func(item *decisionItem) { item.Evidence = []string{"source:source.txt:1@abc123"} },
		"reversed_source_lines":      func(item *decisionItem) { item.Evidence = []string{strings.Replace(source, ":1-2@", ":2-1@", 1)} },
		"zero_source_line":           func(item *decisionItem) { item.Evidence = []string{strings.Replace(source, ":1-2@", ":0@", 1)} },
		"blank_check":                func(item *decisionItem) { item.Verification = []string{"check: "} },
		"check_id_with_whitespace":   func(item *decisionItem) { item.Verification = []string{"check:focused run"} },
		"unresolved_with_choice":     func(item *decisionItem) { item.Status = "unresolved" },
		"unresolved_not_independent": func(item *decisionItem) { item.Status, item.Choice, item.NotIndependent = "unresolved", "", true },
	}
	for name, change := range invalid {
		t.Run(name, func(t *testing.T) {
			path, verr := writeUncheckedConsultation(t, context, func(draft *checkpointDraft) {
				item := fixtureResolvedItem()
				change(&item)
				draft.Items = map[string]decisionItem{"claim": item}
			})
			if verr == nil {
				t.Errorf("consult.ValidateRecord accepted %s", name)
			}
			if _, err := AttachConsultation(t.Context(), record.Directory, "before", path); err == nil {
				t.Fatalf("%s attached", name)
			}
		})
	}
	stored, err := Load(record.Directory)
	if err != nil || len(stored.Consultations) != 0 {
		t.Fatalf("rejected records left a checkpoint: %d %v", len(stored.Consultations), err)
	}
	if _, err := AssignWorker(t.Context(), record.Directory, implementationBrief()); err == nil || !strings.Contains(err.Error(), fixtureAssignRefusal) {
		t.Fatalf("dispatch unlocked by rejected records: %v", err)
	}
	path := writeConsultation(t, context, func(draft *checkpointDraft) {
		item := fixtureResolvedItem()
		item.Evidence = []string{source, "source:source.txt:2@0123abc", "check:focused@run-1"}
		item.Verification = []string{"check:focused", "observation:Line 2 follows the bound line 1 unchanged."}
		draft.Items = map[string]decisionItem{"claim": item}
	})
	if _, err := AttachConsultation(t.Context(), record.Directory, "before", path); err != nil {
		t.Fatalf("typed no-advisers decision rejected: %v", err)
	}
	stored, err = Load(record.Directory)
	if err != nil {
		t.Fatal(err)
	}
	attached, err := consult.ValidateRecord(productionTaskRead(t, filepath.Join(record.Directory, stored.Consultations["before"].File)))
	if err != nil || attached.Status != "no_advisers_configured" || attached.DecisionStatus != "resolved" {
		t.Fatalf("attached copy: %+v %v", attached, err)
	}
	mustAssignWorker(t, stored, implementationBrief())
}

// D4 through attach for answered checkpoints: rejected_judgments are exactly
// the providers whose choice differs; investigations cover every differing
// provider with confidence >= 0.7 (boundary at OpenAI's 0.7); with untrusted
// state an agreement must be marked not independent.
func TestCheckpointDecisionAnswersDisagreeingJudgments(t *testing.T) {
	disagreement := map[string]adviserOutcome{
		"subq_snap":        {Status: "success", Choices: map[string]string{"claim": "contradicted"}},
		"openai_decisions": {Status: "success", Choices: map[string]string{"claim": "supported"}},
	}
	item := func(choice string, rejected, investigations map[string]string, notIndependent bool) decisionItem {
		value := fixtureResolvedItem()
		value.Choice, value.Rejected, value.Investigations, value.NotIndependent = choice, rejected, investigations, notIndependent
		return value
	}
	reason := "The differing choice conflicts with the bound source lines."
	note := "Re-read the bound lines; they settle the conflicting choice."
	untrusted := func(draft *checkpointDraft) {
		draft.State = fixtureObj("task_context", draft.Context, "untrusted", []any{fixtureObj("source", "issue comment", "text", "Storage always happens first.")})
		draft.Outcomes = map[string]adviserOutcome{
			"subq_snap":        {Status: "success", Choices: map[string]string{"claim": "supported"}},
			"openai_decisions": {Status: "success", Choices: map[string]string{"claim": "supported"}},
		}
	}
	for _, scenario := range []struct {
		name    string
		setup   func(*checkpointDraft)
		valid   decisionItem
		invalid map[string]decisionItem
		named   map[string]string
	}{
		{
			name:  "snap_differs_confident",
			setup: withOutcomes(disagreement),
			valid: item("supported", map[string]string{"subq_snap": reason}, map[string]string{"subq_snap": note}, false),
			invalid: map[string]decisionItem{
				"missing_rejection":              item("supported", map[string]string{}, map[string]string{"subq_snap": note}, false),
				"agreeing_provider_rejected":     item("supported", map[string]string{"subq_snap": reason, "openai_decisions": reason}, map[string]string{"subq_snap": note}, false),
				"blank_rejection_reason":         item("supported", map[string]string{"subq_snap": " "}, map[string]string{"subq_snap": note}, false),
				"missing_investigation":          item("supported", map[string]string{"subq_snap": reason}, map[string]string{}, false),
				"blank_investigation":            item("supported", map[string]string{"subq_snap": reason}, map[string]string{"subq_snap": " "}, false),
				"agreeing_provider_investigated": item("supported", map[string]string{"subq_snap": reason}, map[string]string{"subq_snap": note, "openai_decisions": note}, false),
			},
			named: map[string]string{"missing_investigation": "subq_snap"},
		},
		{
			name:  "openai_differs_at_boundary",
			setup: withOutcomes(disagreement),
			valid: item("contradicted", map[string]string{"openai_decisions": reason}, map[string]string{"openai_decisions": note}, false),
			invalid: map[string]decisionItem{
				"missing_investigation": item("contradicted", map[string]string{"openai_decisions": reason}, map[string]string{}, false),
			},
			named: map[string]string{"missing_investigation": "openai_decisions"},
		},
		{
			name:  "untrusted_agreement",
			setup: untrusted,
			valid: item("supported", map[string]string{}, map[string]string{}, true),
			invalid: map[string]decisionItem{
				"marked_independent": item("supported", map[string]string{}, map[string]string{}, false),
			},
		},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			record, _ := fixtureTask(t, substantialPlan(t))
			context := record.Contexts["before"]
			for name, decision := range scenario.invalid {
				path, verr := writeUncheckedConsultation(t, context, func(draft *checkpointDraft) {
					scenario.setup(draft)
					draft.Items = map[string]decisionItem{"claim": decision}
				})
				if verr == nil {
					t.Errorf("%s: consult.ValidateRecord accepted it", name)
				} else if provider, ok := scenario.named[name]; ok && !strings.Contains(verr.Error(), provider) {
					t.Errorf("%s: rejection does not name %s: %v", name, provider, verr)
				}
				if _, err := AttachConsultation(t.Context(), record.Directory, "before", path); err == nil {
					t.Fatalf("%s attached", name)
				}
			}
			attachCheckpoint(t, record, "before", context, func(draft *checkpointDraft) {
				scenario.setup(draft)
				draft.Items = map[string]decisionItem{"claim": scenario.valid}
			})
		})
	}
}
