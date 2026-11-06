package workflow

import (
	"encoding/json/v2"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// Tests for design D1: the version-3 model policy, its configured adviser
// providers and references/question-patterns.json.

// questionPatternsName is where a suite fixture keeps the D1 question patterns.
const questionPatternsName = "skills/task-orchestration/references/question-patterns.json"

// D1 example Choice questions: each names its options and offers
// insufficient_evidence.
const (
	v3RolloutExample   = `{"type":"choice","instructions":"Choose how the change reaches users.","criteria":{"staged":"Roll out in stages behind a flag.","direct":"Roll out to everyone at once.","insufficient_evidence":"The evidence does not decide this."}}`
	v3OwnershipExample = `{"type":"choice","instructions":"Choose which component owns the record.","criteria":{"caller":"The caller owns it.","callee":"The callee owns it.","insufficient_evidence":"The evidence does not decide this."}}`
	v3RiskExample      = `{"type":"choice","instructions":"Choose the failure behavior.","criteria":{"refuse":"Refuse before any write.","continue":"Continue and report.","insufficient_evidence":"The evidence does not decide this."}}`
)

// v3QuestionPatterns renders a D1 question-patterns document with the given
// examples (ID -> Choice JSON) and one valid rule.
func v3QuestionPatterns(examples map[string]string) string {
	return v3QuestionDocument(`1`, `["Ask about one decision per question.","Offer insufficient_evidence unless the answer is mechanically decidable."]`, examples)
}

func v3QuestionDocument(version, rules string, examples map[string]string) string {
	ids := make([]string, 0, len(examples))
	for id := range examples {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	entries := make([]string, 0, len(ids))
	for _, id := range ids {
		key, err := json.Marshal(id)
		if err != nil {
			panic(err)
		}
		entries = append(entries, string(key)+":"+examples[id])
	}
	return `{"version":` + version + `,"rules":` + rules + `,"examples":{` + strings.Join(entries, ",") + `}}`
}

// v3Consultation is the D1 consultation section with the given providers.
func v3Consultation(providers any) Object {
	return Object{
		"deadline_seconds":            30,
		"attempts_per_evidence_batch": 1,
		"automatic_retries":           false,
		"questions":                   "references/question-patterns.json",
		"ownership":                   "task_coordinator",
		"reuse":                       "unchanged_evidence_decisions_question_meaning_and_model",
		"sharing":                     "minimal_evidence_within_task_authorization",
		"comparison":                  "coordinator_evidence_and_verification",
		"providers":                   providers,
	}
}

// v3Policy is the shipped policy with its version, adviser fallback and
// consultation section written per D1, enabling Snap and OpenAI.
func v3Policy(t *testing.T) Object {
	t.Helper()
	data, err := os.ReadFile(shippedPolicy())
	if err != nil {
		t.Fatal(err)
	}
	var policy Object
	if err := yaml.Unmarshal(data, &policy); err != nil {
		t.Fatal(err)
	}
	policy["version"] = 3
	fallbacks := policy["fallbacks"].(Object)
	delete(fallbacks, "jev_unavailable_or_inconclusive")
	fallbacks["advisers_unavailable_or_inconclusive"] = "continue_with_source_evidence_and_conservative_routing"
	policy["consultation"] = v3Consultation(Object{"subq_snap": "subq-snap-preview", "openai_decisions": "gpt-6-luna"})
	return policy
}

// v3PolicyFixture is a suite fixture whose task-orchestration skill holds a
// valid D1 policy and question patterns written by the test.
type v3PolicyFixture struct {
	suite *suiteFixture
	path  string
}

func newV3PolicyFixture(t *testing.T, edit func(Object)) *v3PolicyFixture {
	t.Helper()
	suite := newSuiteFixture(t)
	fixture := &v3PolicyFixture{suite: suite, path: filepath.Join(suite.root, filepath.FromSlash(suite.manifest["model_policy"].(string)))}
	suite.write(t, questionPatternsName, v3QuestionPatterns(map[string]string{"rollout": v3RolloutExample, "ownership": v3OwnershipExample}))
	policy := v3Policy(t)
	if edit != nil {
		edit(policy)
	}
	fixture.writePolicy(t, policy)
	return fixture
}

func (fixture *v3PolicyFixture) writePolicy(t *testing.T, policy Object) {
	t.Helper()
	data, err := yaml.Marshal(policy)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fixture.path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func v3ConsultationOf(policy Object) Object {
	return policy["consultation"].(Object)
}

// D1: providers may be empty, any single provider or all three, each with a
// pinned model. The returned policy exposes providers as a mapping and the
// deadline as an int.
func TestV3PolicyAcceptsEveryConfiguredProviderSet(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name      string
		providers Object
	}{
		{"no providers", Object{}},
		{"only Jev", Object{"jev": "jev-1.14.0"}},
		{"only Jev at another pinned version", Object{"jev": "jev-2.10.123"}},
		{"only Snap", Object{"subq_snap": "subq-snap-preview"}},
		{"only OpenAI", Object{"openai_decisions": "gpt-6-luna"}},
		{"all three", Object{"jev": "jev-1.14.0", "subq_snap": "subq-snap-preview", "openai_decisions": "gpt-6-luna"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			fixture := newV3PolicyFixture(t, func(policy Object) { v3ConsultationOf(policy)["providers"] = test.providers })
			policy, err := ValidateModelPolicy(fixture.path)
			if err != nil {
				t.Fatalf("valid D1 policy rejected: %v", err)
			}
			consultation, ok := policy["consultation"].(Object)
			if !ok {
				t.Fatalf("consultation = %#v; want a mapping", policy["consultation"])
			}
			providers, ok := consultation["providers"].(Object)
			if !ok {
				t.Fatalf("consultation.providers = %#v (%T); want a mapping", consultation["providers"], consultation["providers"])
			}
			if len(providers) != len(test.providers) {
				t.Fatalf("providers = %#v; want %#v", providers, test.providers)
			}
			for id, model := range test.providers {
				if got, ok := providers[id].(string); !ok || got != model {
					t.Fatalf("providers[%q] = %#v; want %q", id, providers[id], model)
				}
			}
			if deadline, ok := consultation["deadline_seconds"].(int); !ok || deadline != 30 {
				t.Fatalf("deadline_seconds = %#v (%T); want int 30", consultation["deadline_seconds"], consultation["deadline_seconds"])
			}
			if _, err := ValidateSuite(fixture.suite.root); err != nil {
				t.Fatalf("suite rejected a valid D1 policy: %v", err)
			}
			if _, err := ModelDefaults(fixture.suite.root, fixture.suite.manifest); err != nil {
				t.Fatalf("model defaults rejected a valid D1 policy: %v", err)
			}
		})
	}
}

// D1: the policy validator rejects unknown providers, unpinned or alias
// models, malformed providers, removed keys and the old fallback name.
func TestV3PolicyRejectsInvalidConsultation(t *testing.T) {
	t.Parallel()
	providers := func(value any) func(Object) {
		return func(policy Object) { v3ConsultationOf(policy)["providers"] = value }
	}
	for _, test := range []struct {
		name string
		edit func(Object)
		// want, when set, is text the error must contain because D1 says the
		// error names the problem.
		want string
	}{
		{"unknown provider", providers(Object{"subq_snap": "subq-snap-preview", "other_adviser": "other-model"}), "other_adviser"},
		{"unknown provider alone", providers(Object{"typesafe": "jev-1.14.0"}), "typesafe"},
		{"Jev latest alias", providers(Object{"jev": "jev-latest"}), ""},
		{"Jev unpinned patch", providers(Object{"jev": "jev-1.14"}), ""},
		{"Jev bare name", providers(Object{"jev": "jev"}), ""},
		{"Jev prerelease suffix", providers(Object{"jev": "jev-1.14.0-rc1"}), ""},
		{"Jev prefix text", providers(Object{"jev": "x-jev-1.14.0"}), ""},
		{"Jev trailing newline", providers(Object{"jev": "jev-1.14.0\n"}), ""},
		{"Snap alias", providers(Object{"subq_snap": "subq-snap"}), ""},
		{"Snap latest alias", providers(Object{"subq_snap": "subq-latest"}), ""},
		{"OpenAI alias", providers(Object{"openai_decisions": "gpt-6"}), ""},
		{"OpenAI other model", providers(Object{"openai_decisions": "gpt-6.1-sol"}), ""},
		{"Snap model for OpenAI", providers(Object{"openai_decisions": "subq-snap-preview"}), ""},
		{"null providers", providers(nil), "providers"},
		{"provider sequence", providers([]any{"subq_snap", "openai_decisions"}), "providers"},
		{"provider string", providers("subq_snap"), "providers"},
		{"missing providers", func(policy Object) { delete(v3ConsultationOf(policy), "providers") }, ""},
		{"numeric Jev model", providers(Object{"jev": 1.14}), "jev"},
		{"boolean Snap model", providers(Object{"subq_snap": true}), "subq_snap"},
		{"null OpenAI model", providers(Object{"openai_decisions": nil}), "openai_decisions"},
		{"OpenAI model mapping", providers(Object{"openai_decisions": Object{"model": "gpt-6-luna"}}), "openai_decisions"},
		{"Snap model sequence", providers(Object{"subq_snap": []any{"subq-snap-preview"}}), "subq_snap"},
		{"removed consultation.model", func(policy Object) { v3ConsultationOf(policy)["model"] = "jev-1.14.0" }, ""},
		{"removed consultation.model beside Jev", func(policy Object) {
			consultation := v3ConsultationOf(policy)
			consultation["model"] = "jev-1.14.0"
			consultation["providers"] = Object{"jev": "jev-1.14.0"}
		}, ""},
		{"removed optional providers", func(policy Object) {
			consultation := v3ConsultationOf(policy)
			consultation["providers"] = Object{"subq_snap": "subq-snap-preview"}
			consultation["optional_providers"] = Object{"openai_decisions": "gpt-6-luna"}
		}, ""},
		{"removed empty optional providers", func(policy Object) { v3ConsultationOf(policy)["optional_providers"] = Object{} }, ""},
		{"removed historical comparison alias", func(policy Object) {
			v3ConsultationOf(policy)["comparison"] = "codex_evidence_and_verification"
		}, ""},
		{"comparison votes", func(policy Object) { v3ConsultationOf(policy)["comparison"] = "majority_vote" }, ""},
		{"missing comparison", func(policy Object) { delete(v3ConsultationOf(policy), "comparison") }, ""},
		{"unknown consultation field", func(policy Object) { v3ConsultationOf(policy)["required_providers"] = Object{} }, ""},
		{"old Jev fallback name", func(policy Object) {
			fallbacks := policy["fallbacks"].(Object)
			delete(fallbacks, "advisers_unavailable_or_inconclusive")
			fallbacks["jev_unavailable_or_inconclusive"] = "continue_with_source_evidence_and_conservative_routing"
		}, ""},
		{"old Jev fallback beside the renamed key", func(policy Object) {
			policy["fallbacks"].(Object)["jev_unavailable_or_inconclusive"] = "continue_with_source_evidence_and_conservative_routing"
		}, ""},
		{"escalating adviser fallback", func(policy Object) {
			policy["fallbacks"].(Object)["advisers_unavailable_or_inconclusive"] = "automatic_escalation"
		}, ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			fixture := newV3PolicyFixture(t, test.edit)
			policy, err := ValidateModelPolicy(fixture.path)
			if err == nil {
				t.Fatalf("invalid D1 policy accepted: %#v", policy["consultation"])
			}
			if test.want != "" && !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v; want it to name %q", err, test.want)
			}
			if _, err := ValidateSuite(fixture.suite.root); err == nil {
				t.Fatal("suite accepted an invalid D1 policy")
			}
			if _, err := ModelDefaults(fixture.suite.root, fixture.suite.manifest); err == nil {
				t.Fatal("model defaults accepted an invalid D1 policy")
			}
		})
	}
}

// D1: only version 3 is supported, and the error names version 3.
func TestV3PolicyRejectsOtherVersions(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name string
		edit func(Object)
		want string
	}{
		{"version 2 with a version-3 body", func(policy Object) { policy["version"] = 2 },
			"model policy version 2 is not supported; this cw requires version 3"},
		{"version 2 in the previous shipped shape", func(policy Object) {
			version2Policy(policy)
		}, "model policy version 2 is not supported; this cw requires version 3"},
		{"version 1", func(policy Object) { policy["version"] = 1 },
			"model policy version 1 is not supported; this cw requires version 3"},
		{"version 4", func(policy Object) { policy["version"] = 4 },
			"model policy version 4 is not supported; this cw requires version 3"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			fixture := newV3PolicyFixture(t, test.edit)
			if _, err := ValidateModelPolicy(fixture.path); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("ValidateModelPolicy() error = %v; want containing %q", err, test.want)
			}
			if _, err := ValidateSuite(fixture.suite.root); err == nil {
				t.Fatal("suite accepted an unsupported policy version")
			}
		})
	}
}

// version2Policy rewrites a D1 policy into the version-2 shape the previous
// release shipped: a required Jev, Snap and OpenAI triple, consultation.model,
// jev-questions.json and the old fallback name.
func version2Policy(policy Object) {
	policy["version"] = 2
	fallbacks := policy["fallbacks"].(Object)
	delete(fallbacks, "advisers_unavailable_or_inconclusive")
	fallbacks["jev_unavailable_or_inconclusive"] = "continue_with_source_evidence_and_conservative_routing"
	consultation := v3Consultation(Object{"jev": "jev-1.14.0", "subq_snap": "subq-snap-preview", "openai_decisions": "gpt-6-luna"})
	consultation["model"] = "jev-1.14.0"
	consultation["questions"] = "references/jev-questions.json"
	policy["consultation"] = consultation
}

// D1: the question-patterns file must exist and hold version 1, non-empty
// string rules and 2 or 3 Choice examples that each offer
// insufficient_evidence.
func TestV3PolicyRejectsInvalidQuestionPatterns(t *testing.T) {
	t.Parallel()
	two := map[string]string{"rollout": v3RolloutExample, "ownership": v3OwnershipExample}
	rules := `["Ask about one decision per question."]`
	for _, test := range []struct {
		name     string
		contents string // empty removes the file
	}{
		{"missing file", ""},
		{"malformed JSON", `{"version":1,"rules":[`},
		{"JSON array", `[]`},
		{"wrong version", v3QuestionDocument(`2`, rules, two)},
		{"missing version", `{"rules":` + rules + `,"examples":{"rollout":` + v3RolloutExample + `,"ownership":` + v3OwnershipExample + `}}`},
		{"no examples", v3QuestionDocument(`1`, rules, map[string]string{})},
		{"one example", v3QuestionDocument(`1`, rules, map[string]string{"rollout": v3RolloutExample})},
		{"four examples", v3QuestionDocument(`1`, rules, map[string]string{
			"rollout": v3RolloutExample, "ownership": v3OwnershipExample, "risk": v3RiskExample,
			"scope": `{"type":"choice","instructions":"Choose the scope.","criteria":{"narrow":"Keep it narrow.","wide":"Widen it.","insufficient_evidence":"The evidence does not decide this."}}`,
		})},
		{"example without insufficient_evidence", v3QuestionDocument(`1`, rules, map[string]string{
			"rollout": v3RolloutExample,
			"plain":   `{"type":"choice","instructions":"Choose one.","criteria":{"a":"First.","b":"Second."}}`,
		})},
		{"non-Choice example", v3QuestionDocument(`1`, rules, map[string]string{
			"rollout": v3RolloutExample,
			"text":    `{"type":"text","instructions":"Describe the risk."}`,
		})},
		{"example without a type", v3QuestionDocument(`1`, rules, map[string]string{
			"rollout": v3RolloutExample,
			"untyped": `{"instructions":"Choose one.","criteria":{"a":"First.","insufficient_evidence":"Undecided."}}`,
		})},
		{"previous jev-questions shape", `{"version":1,"questions":{"rollout":` + v3RolloutExample + `,"ownership":` + v3OwnershipExample + `}}`},
		{"missing rules", `{"version":1,"examples":{"rollout":` + v3RolloutExample + `,"ownership":` + v3OwnershipExample + `}}`},
		{"blank rule", v3QuestionDocument(`1`, `[" "]`, two)},
		{"non-string rule", v3QuestionDocument(`1`, `[1]`, two)},
		{"unknown top-level field", strings.TrimSuffix(v3QuestionPatterns(two), "}") + `,"questions":{}}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			fixture := newV3PolicyFixture(t, nil)
			target := filepath.Join(fixture.suite.root, filepath.FromSlash(questionPatternsName))
			if test.contents == "" {
				if err := os.Remove(target); err != nil {
					t.Fatal(err)
				}
			} else {
				fixture.suite.write(t, questionPatternsName, test.contents)
			}
			if _, err := ValidateModelPolicy(fixture.path); err == nil {
				t.Fatal("policy with invalid question patterns accepted")
			}
			if _, err := ValidateSuite(fixture.suite.root); err == nil {
				t.Fatal("suite accepted invalid question patterns")
			}
		})
	}
	t.Run("three examples", func(t *testing.T) {
		t.Parallel()
		fixture := newV3PolicyFixture(t, nil)
		fixture.suite.write(t, questionPatternsName, v3QuestionPatterns(map[string]string{"rollout": v3RolloutExample, "ownership": v3OwnershipExample, "risk": v3RiskExample}))
		if _, err := ValidateModelPolicy(fixture.path); err != nil {
			t.Fatalf("three valid examples rejected: %v", err)
		}
	})
}

// D1 shipped default: version 3, Snap and OpenAI enabled, Jev opt-in, no
// removed keys, the renamed fallback and question-patterns.json in place of
// jev-questions.json.
func TestShippedPolicyEnablesSnapAndOpenAIButNotJev(t *testing.T) {
	t.Parallel()
	policy, err := ValidateModelPolicy(shippedPolicy())
	if err != nil {
		t.Fatal(err)
	}
	if version, ok := policy["version"].(int); !ok || version != 3 {
		t.Fatalf("shipped policy version = %#v", policy["version"])
	}
	consultation := v3ConsultationOf(policy)
	providers, ok := consultation["providers"].(Object)
	if !ok {
		t.Fatalf("shipped providers = %#v; want a mapping", consultation["providers"])
	}
	if len(providers) != 2 || providers["subq_snap"] != "subq-snap-preview" || providers["openai_decisions"] != "gpt-6-luna" {
		t.Fatalf("shipped providers = %#v; want exactly subq_snap and openai_decisions", providers)
	}
	if _, enabled := providers["jev"]; enabled {
		t.Fatal("the shipped policy enables Jev; it must be opt-in")
	}
	for _, removed := range []string{"model", "optional_providers"} {
		if _, present := consultation[removed]; present {
			t.Fatalf("shipped consultation keeps the removed %q", removed)
		}
	}
	if consultation["questions"] != "references/question-patterns.json" || consultation["comparison"] != "coordinator_evidence_and_verification" {
		t.Fatalf("shipped consultation = %#v", consultation)
	}
	fallbacks := policy["fallbacks"].(Object)
	if fallbacks["advisers_unavailable_or_inconclusive"] != "continue_with_source_evidence_and_conservative_routing" {
		t.Fatalf("shipped adviser fallback = %#v", fallbacks["advisers_unavailable_or_inconclusive"])
	}
	if _, present := fallbacks["jev_unavailable_or_inconclusive"]; present {
		t.Fatal("shipped policy keeps the old Jev fallback name")
	}
	references := filepath.Join(filepath.Dir(shippedPolicy()), "references")
	if _, err := os.Stat(filepath.Join(references, "question-patterns.json")); err != nil {
		t.Fatalf("shipped question patterns: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(references, "jev-questions.json")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("jev-questions.json must be deleted: %v", err)
	}
}

// D1: the Claude policy has no consultation section.
func TestShippedClaudePolicyHasNoConsultation(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile(filepath.Join(filepath.Dir(shippedPolicy()), "model-policy.claude.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var policy Object
	if err := yaml.Unmarshal(data, &policy); err != nil {
		t.Fatal(err)
	}
	if _, present := policy["consultation"]; present {
		t.Fatal("model-policy.claude.yaml must not carry a consultation section")
	}
}
