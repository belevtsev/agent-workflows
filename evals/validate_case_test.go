package evals

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// Authored tests for loadPluginInfo, findCases and validateCase (V1-V10).

const (
	authoredSuite    = "demo-suite"
	authoredCaseName = "demo-case"
	authoredCaseRel  = "evals/" + authoredSuite + "/" + authoredCaseName
)

// authoredRealRoot is the repository root: tests run in R/evals.
func authoredRealRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	return root
}

// authoredPlugin mirrors the real plugin manifest without reading it.
func authoredPlugin() pluginInfo {
	return pluginInfo{
		Name: "agent-workflows",
		Agents: map[string]bool{
			"architect":        true,
			"implementer":      true,
			"test-engineer":    true,
			"cw-reviewer":      true,
			"cw-test-author":   true,
			"cw-github-worker": true,
		},
	}
}

// authoredBT turns ^ into a backtick so prompt bodies can sit in raw strings.
func authoredBT(s string) string { return strings.ReplaceAll(s, "^", "`") }

const authoredCaseYAML = `schema_version: "1.1"
name: demo-case
context:
  scaffold_script: scaffold.sh
`

const authoredFrontmatter = `---
description: Demo case for the authored validator tests.
tags: [demo-suite, extra]
runs: 1
max_turns: 40
timeout_seconds: 1800
allowed_tools: [Read, Agent, Write]
---
`

const authoredDispatch = `subagent_type: "agent-workflows:test-engineer"`

// The Owned paths bullet wraps onto two indented lines; the next bullet names
// a path that is not owned.
const authoredBody = `You are the coordinator. Dispatch the ^test-engineer^ agent once through the
Agent tool (^DISPATCH^) with the brief below. The inputs live under
^testdata/contract/interface-contract.md^.

Brief:
- Goal: tests for package ^demo^.
- Owned paths: ^demo/demo_test.go^ (external test package ^demo_test^),
  ^artefacts/test-matrix.md^,
  ^artefacts/requirement-map.md^.
- Approved inputs: ^docs/inputs.md^.
- Stop condition: every requirement is covered.
`

// authoredPrompt builds prompt.md with the given dispatch text and body.
func authoredPrompt(dispatch, body string) string {
	return authoredFrontmatter + authoredBT(strings.ReplaceAll(body, "DISPATCH", dispatch))
}

func authoredBaseFiles() map[string]string {
	return map[string]string{
		"case.yaml":   authoredCaseYAML,
		"prompt.md":   authoredPrompt(authoredDispatch, authoredBody),
		"scaffold.sh": "#!/bin/sh\nset -eu\ncp -R \"$(dirname \"$0\")/testdata\" ./testdata\n",
		"graders/dispatched.md": `---
type: tool_used
tool: Agent
input_match: '"subagent_type"\s*:\s*"(?:[\w-]+:)?test-engineer"'
---
`,
		"graders/impl-not-read.md": `---
type: tool_used
tool: Read
input_match: '[/\\]impl[/\\]'
min: 0
max: 0
---
`,
		"graders/matrix-written.md": `---
type: file_exists
path: artefacts/test-matrix.md
---
`,
		"graders/map-written.md": `---
type: file_exists
path: artefacts/requirement-map.md
---
`,
		"graders/tests-written.md": `---
type: file_exists
path: demo/demo_test.go
---
`,
		"graders/matrix-categories.md": `---
type: regex
pattern: '(?=[\s\S]*happy)(?=[\s\S]*boundar)(?=[\s\S]*failure)'
flags: i
weight: 2
arm: both
target: { source: file, path: artefacts/test-matrix.md }
---
`,
		"graders/not-in-trace.md": `---
type: regex
pattern: 'secretToken'
match: not_contains
target: trace
---
`,
		"graders/review.md": `---
type: llm
focus: { source: file, path: demo/demo_test.go }
---

PASS if every test names its requirement.
`,
	}
}

// authoredWriteCase writes files into <tmp>/evals/demo-suite/demo-case and
// returns the temporary repository root and the case directory.
func authoredWriteCase(t *testing.T, files map[string]string) (string, string) {
	t.Helper()
	root := t.TempDir()
	caseDir := filepath.Join(root, "evals", authoredSuite, authoredCaseName)
	if err := os.MkdirAll(caseDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for rel, body := range files {
		p := filepath.Join(caseDir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root, caseDir
}

// authoredValidate writes the base case with overrides (an empty string
// deletes the file) and validates it with the given plugin.
func authoredValidate(t *testing.T, p pluginInfo, overrides map[string]string) []error {
	t.Helper()
	files := authoredBaseFiles()
	for k, v := range overrides {
		if v == "" {
			delete(files, k)
			continue
		}
		files[k] = v
	}
	root, caseDir := authoredWriteCase(t, files)
	return validateCase(root, caseDir, p)
}

func authoredErrText(errs []error) string {
	var b strings.Builder
	for _, e := range errs {
		b.WriteString("\n  ")
		b.WriteString(e.Error())
	}
	return b.String()
}

// authoredFind reports whether one error's text contains every part.
func authoredFind(errs []error, parts ...string) bool {
	for _, e := range errs {
		ok := true
		for _, p := range parts {
			if !strings.Contains(e.Error(), p) {
				ok = false
				break
			}
		}
		if ok {
			return true
		}
	}
	return false
}

func authoredWantError(t *testing.T, errs []error, parts ...string) {
	t.Helper()
	if !authoredFind(errs, parts...) {
		t.Fatalf("want an error containing %q; got:%s", parts, authoredErrText(errs))
	}
}

func authoredWantNoError(t *testing.T, errs []error) {
	t.Helper()
	if len(errs) != 0 {
		t.Fatalf("want no errors; got:%s", authoredErrText(errs))
	}
}

func authoredWantNoErrorContaining(t *testing.T, errs []error, part string) {
	t.Helper()
	for _, e := range errs {
		if strings.Contains(e.Error(), part) {
			t.Fatalf("want no error containing %q; got:%s", part, authoredErrText(errs))
		}
	}
}

func authoredGrader(fm string) string { return "---\n" + fm + "---\n" }

// Baseline for every other validator test: the fixture is valid.
func TestAuthoredValidateBaseCaseIsValid(t *testing.T) {
	authoredWantNoError(t, authoredValidate(t, authoredPlugin(), nil))
}

// V1: missing case.yaml, prompt.md or grader Markdown.
func TestAuthoredValidateMissingFiles(t *testing.T) {
	t.Run("case.yaml", func(t *testing.T) {
		errs := authoredValidate(t, authoredPlugin(), map[string]string{"case.yaml": ""})
		authoredWantError(t, errs, "missing", authoredCaseRel, "case.yaml")
	})
	t.Run("prompt.md", func(t *testing.T) {
		errs := authoredValidate(t, authoredPlugin(), map[string]string{"prompt.md": ""})
		authoredWantError(t, errs, "missing", authoredCaseRel, "prompt.md")
	})
	t.Run("graders without md", func(t *testing.T) {
		files := authoredBaseFiles()
		for k := range files {
			if strings.HasPrefix(k, "graders/") {
				delete(files, k)
			}
		}
		files["graders/README.txt"] = "not a grader\n"
		root, caseDir := authoredWriteCase(t, files)
		errs := validateCase(root, caseDir, authoredPlugin())
		authoredWantError(t, errs, "missing", authoredCaseRel, "graders")
	})
}

// V2: invalid YAML in case.yaml, prompt frontmatter or grader frontmatter.
func TestAuthoredValidateParseErrors(t *testing.T) {
	cases := []struct {
		name, file, body string
	}{
		{"case.yaml", "case.yaml", "schema_version: \"1.1\"\nname: [demo-case\n"},
		{"prompt frontmatter", "prompt.md", "---\ntags: [demo-suite\nruns: 1\n---\n" + authoredBT(strings.ReplaceAll(authoredBody, "DISPATCH", authoredDispatch))},
		{"grader frontmatter", "graders/not-in-trace.md", authoredGrader("type: regex\npattern: [unclosed\n")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			errs := authoredValidate(t, authoredPlugin(), map[string]string{tc.file: tc.body})
			authoredWantError(t, errs, "parse", authoredCaseRel+"/"+tc.file)
		})
	}
}

// V3: unknown keys at every level are reported by name.
func TestAuthoredValidateUnknownKeys(t *testing.T) {
	cases := []struct {
		name, file, body, key string
	}{
		{"case.yaml top level", "case.yaml", authoredCaseYAML + "owner: me\n", "owner"},
		{"case.yaml context", "case.yaml", "schema_version: \"1.1\"\nname: demo-case\ncontext:\n  scaffold_script: scaffold.sh\n  scaffold_dir: x\n", "scaffold_dir"},
		{"prompt frontmatter", "prompt.md", strings.Replace(authoredPrompt(authoredDispatch, authoredBody), "runs: 1\n", "runs: 1\ntemperature: 0\n", 1), "temperature"},
		{"grader common", "graders/not-in-trace.md", authoredGrader("type: regex\npattern: 'x'\nseverity: high\n"), "severity"},
		{"file_exists with regex key", "graders/matrix-written.md", authoredGrader("type: file_exists\npath: artefacts/test-matrix.md\npattern: 'x'\n"), "pattern"},
		{"llm with tool_used key", "graders/review.md", authoredGrader("type: llm\ncriteria: PASS always.\ntool: Read\n"), "tool"},
		{"tool_used with regex key", "graders/impl-not-read.md", authoredGrader("type: tool_used\ntool: Read\ntarget: trace\n"), "target"},
		{"regex with llm key", "graders/not-in-trace.md", authoredGrader("type: regex\npattern: 'x'\nfocus: trace\n"), "focus"},
		{"tool_order with file_exists key", "graders/order.md", authoredGrader("type: tool_order\nbefore: Read\nafter: Write\npath: artefacts/test-matrix.md\n"), "path"},
		{"baseline with regex key", "graders/baseline.md", authoredGrader("type: baseline\nbaseline_file: baseline.md\nflags: i\n"), "flags"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			errs := authoredValidate(t, authoredPlugin(), map[string]string{tc.file: tc.body})
			authoredWantError(t, errs, `unknown key "`+tc.key+`"`, authoredCaseRel+"/"+tc.file)
		})
	}
}

// V3: every allowed key is accepted (no unknown-key error).
func TestAuthoredValidateAllowedKeysAccepted(t *testing.T) {
	caseYAML := `schema_version: "1.1"
name: demo-case
description: All keys.
tags: [demo-suite]
plugins: [agent-workflows]
runs: 1
expected_outcome: pass
execution: {}
graders: []
context:
  scaffold_script: scaffold.sh
  history_file: history.json
  add_dirs: [extra]
`
	prompt := `---
schema_version: "1.1"
name: demo-case
description: All keys.
tags: [demo-suite]
plugins: [agent-workflows]
runs: 1
expected_outcome: pass
model: sonnet
max_turns: 40
timeout_seconds: 1800
allowed_tools: [Read, Agent]
append_system_prompt: Be brief.
env: {DEMO: "1"}
---
` + authoredBT(strings.ReplaceAll(authoredBody, "DISPATCH", authoredDispatch))
	overrides := map[string]string{
		"case.yaml":                caseYAML,
		"history.json":             "[]\n",
		"baseline.md":              "baseline\n",
		"prompt.md":                prompt,
		"graders/not-in-trace.md":  authoredGrader("type: regex\nweight: 1\narm: both\npattern: 'x'\nflags: i\nmatch: contains\ntarget: trace\n"),
		"graders/impl-not-read.md": authoredGrader("type: tool_used\nweight: 1\narm: with-only\ntool: Read\ninput_match: 'impl'\nmin: 0\nmax: 0\n"),
		"graders/order.md":         authoredGrader("type: tool_order\nweight: 1\narm: both\nbefore: Read\nafter: Write\n"),
		"graders/map-written.md":   authoredGrader("type: file_exists\nweight: 1\narm: both\npath: artefacts/requirement-map.md\nexists: true\n"),
		"graders/review.md":        authoredGrader("type: llm\nweight: 1\narm: both\ncriteria: PASS if tests name requirements.\nfocus: last_message\n"),
		"graders/baseline.md":      authoredGrader("type: baseline\nweight: 1\narm: both\nbaseline_file: baseline.md\ncriteria: PASS if better than baseline.\n"),
	}
	errs := authoredValidate(t, authoredPlugin(), overrides)
	authoredWantNoErrorContaining(t, errs, "unknown key")
}

// V4: missing required keys are reported by name.
func TestAuthoredValidateMissingRequiredKeys(t *testing.T) {
	cases := []struct {
		name, file, body, key string
	}{
		{"case.yaml schema_version", "case.yaml", "name: demo-case\ncontext:\n  scaffold_script: scaffold.sh\n", "schema_version"},
		{"case.yaml name", "case.yaml", "schema_version: \"1.1\"\ncontext:\n  scaffold_script: scaffold.sh\n", "name"},
		{"grader type", "graders/matrix-written.md", authoredGrader("path: artefacts/test-matrix.md\n"), "type"},
		{"regex pattern", "graders/not-in-trace.md", authoredGrader("type: regex\nmatch: not_contains\ntarget: trace\n"), "pattern"},
		{"tool_used tool", "graders/impl-not-read.md", authoredGrader("type: tool_used\ninput_match: 'impl'\nmin: 0\nmax: 0\n"), "tool"},
		{"tool_order before", "graders/order.md", authoredGrader("type: tool_order\nafter: Write\n"), "before"},
		{"tool_order after", "graders/order.md", authoredGrader("type: tool_order\nbefore: Read\n"), "after"},
		{"file_exists path", "graders/matrix-written.md", authoredGrader("type: file_exists\nexists: true\n"), "path"},
		{"baseline baseline_file", "graders/baseline.md", authoredGrader("type: baseline\ncriteria: PASS if better.\n"), "baseline_file"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			errs := authoredValidate(t, authoredPlugin(), map[string]string{tc.file: tc.body})
			authoredWantError(t, errs, `missing required key "`+tc.key+`"`, authoredCaseRel+"/"+tc.file)
		})
	}
}

// V5: value rules; each invalid value yields an error naming the file.
func TestAuthoredValidateValueRules(t *testing.T) {
	prompt := authoredPrompt(authoredDispatch, authoredBody)
	withFM := func(old, repl string) string {
		if !strings.Contains(prompt, old) {
			t.Fatalf("fixture prompt lacks %q", old)
		}
		return strings.Replace(prompt, old, repl, 1)
	}
	cases := []struct {
		name, file, body, part string
	}{
		{"schema_version 1.0", "case.yaml", strings.Replace(authoredCaseYAML, `"1.1"`, `"1.0"`, 1), ""},
		{"schema_version not a string", "case.yaml", strings.Replace(authoredCaseYAML, `"1.1"`, `1.1`, 1), ""},
		{"name differs from directory", "case.yaml", strings.Replace(authoredCaseYAML, "name: demo-case", "name: other-case", 1), ""},
		{"scaffold_script missing", "case.yaml", strings.Replace(authoredCaseYAML, "scaffold.sh", "missing.sh", 1), ""},
		{"unknown grader type", "graders/not-in-trace.md", authoredGrader("type: shell\npattern: 'x'\n"), ""},
		{"runs 0", "prompt.md", withFM("runs: 1\n", "runs: 0\n"), ""},
		{"runs 51", "prompt.md", withFM("runs: 1\n", "runs: 51\n"), ""},
		{"max_turns 0", "prompt.md", withFM("max_turns: 40\n", "max_turns: 0\n"), ""},
		{"max_turns 201", "prompt.md", withFM("max_turns: 40\n", "max_turns: 201\n"), ""},
		{"timeout_seconds 0", "prompt.md", withFM("timeout_seconds: 1800\n", "timeout_seconds: 0\n"), ""},
		{"timeout_seconds 3601", "prompt.md", withFM("timeout_seconds: 1800\n", "timeout_seconds: 3601\n"), ""},
		{"tags without suite", "prompt.md", withFM("tags: [demo-suite, extra]\n", "tags: [other]\n"), "tag"},
		{"flags unknown character", "graders/not-in-trace.md", authoredGrader("type: regex\npattern: 'x'\nflags: ix\ntarget: trace\n"), ""},
		{"flags repeated", "graders/not-in-trace.md", authoredGrader("type: regex\npattern: 'x'\nflags: ii\ntarget: trace\n"), ""},
		{"match unknown", "graders/not-in-trace.md", authoredGrader("type: regex\npattern: 'x'\nmatch: maybe\ntarget: trace\n"), ""},
		{"match count without digits", "graders/not-in-trace.md", authoredGrader("type: regex\npattern: 'x'\nmatch: 'count:'\ntarget: trace\n"), ""},
		{"match count with letters", "graders/not-in-trace.md", authoredGrader("type: regex\npattern: 'x'\nmatch: 'count:x'\ntarget: trace\n"), ""},
		{"min negative", "graders/impl-not-read.md", authoredGrader("type: tool_used\ntool: Read\nmin: -1\n"), ""},
		{"max negative", "graders/impl-not-read.md", authoredGrader("type: tool_used\ntool: Read\nmax: -1\n"), ""},
		{"min above max", "graders/impl-not-read.md", authoredGrader("type: tool_used\ntool: Read\nmin: 3\nmax: 1\n"), ""},
		{"max 0 without min (min defaults to 1)", "graders/impl-not-read.md", authoredGrader("type: tool_used\ntool: Read\ninput_match: 'impl'\nmax: 0\n"), ""},
		{"weight zero", "graders/matrix-written.md", authoredGrader("type: file_exists\npath: artefacts/test-matrix.md\nweight: 0\n"), ""},
		{"weight negative", "graders/matrix-written.md", authoredGrader("type: file_exists\npath: artefacts/test-matrix.md\nweight: -1\n"), ""},
		{"weight not a number", "graders/matrix-written.md", authoredGrader("type: file_exists\npath: artefacts/test-matrix.md\nweight: heavy\n"), ""},
		{"arm unknown", "graders/matrix-written.md", authoredGrader("type: file_exists\npath: artefacts/test-matrix.md\narm: without\n"), ""},
		{"llm without body or criteria", "graders/review.md", authoredGrader("type: llm\nfocus: { source: file, path: demo/demo_test.go }\n"), ""},
		{"target unknown string", "graders/not-in-trace.md", authoredGrader("type: regex\npattern: 'x'\ntarget: stdout\n"), ""},
		{"target mapping with other source", "graders/not-in-trace.md", authoredGrader("type: regex\npattern: 'x'\ntarget: { source: url, path: artefacts/test-matrix.md }\n"), ""},
		{"target mapping without path", "graders/not-in-trace.md", authoredGrader("type: regex\npattern: 'x'\ntarget: { source: file }\n"), ""},
		{"focus unknown string", "graders/review.md", authoredGrader("type: llm\ncriteria: PASS always.\nfocus: nowhere\n"), ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			errs := authoredValidate(t, authoredPlugin(), map[string]string{tc.file: tc.body})
			authoredWantError(t, errs, tc.part, authoredCaseRel+"/"+tc.file)
		})
	}
}

// V5: an empty prompt body is an error of its own, besides dispatching no agent.
func TestAuthoredValidateEmptyPromptBody(t *testing.T) {
	errs := authoredValidate(t, authoredPlugin(), map[string]string{"prompt.md": authoredFrontmatter + "\n  \n"})
	for _, e := range errs {
		if strings.Contains(e.Error(), authoredCaseRel+"/prompt.md") && !strings.Contains(e.Error(), "dispatches no agent") {
			return
		}
	}
	t.Fatalf("want an empty-body error for prompt.md other than the dispatch error; got:%s", authoredErrText(errs))
}

// V5: values on the valid side of each boundary are accepted.
func TestAuthoredValidateValueBoundariesAccepted(t *testing.T) {
	prompt := authoredPrompt(authoredDispatch, authoredBody)
	cases := []struct {
		name      string
		overrides map[string]string
	}{
		{"runs 1 max_turns 1 timeout 1", map[string]string{"prompt.md": strings.NewReplacer("max_turns: 40\n", "max_turns: 1\n", "timeout_seconds: 1800\n", "timeout_seconds: 1\n").Replace(prompt)}},
		{"runs 50 max_turns 200 timeout 3600", map[string]string{"prompt.md": strings.NewReplacer("runs: 1\n", "runs: 50\n", "max_turns: 40\n", "max_turns: 200\n", "timeout_seconds: 1800\n", "timeout_seconds: 3600\n").Replace(prompt)}},
		{"all flags once", map[string]string{"graders/not-in-trace.md": authoredGrader("type: regex\npattern: 'x'\nflags: dgimsy\ntarget: trace\n")}},
		{"flag v", map[string]string{"graders/not-in-trace.md": authoredGrader("type: regex\npattern: 'x'\nflags: v\ntarget: trace\n")}},
		{"flag u", map[string]string{"graders/not-in-trace.md": authoredGrader("type: regex\npattern: 'x'\nflags: u\ntarget: trace\n")}},
		{"match contains", map[string]string{"graders/not-in-trace.md": authoredGrader("type: regex\npattern: 'x'\nmatch: contains\ntarget: last_message\n")}},
		{"match count", map[string]string{"graders/not-in-trace.md": authoredGrader("type: regex\npattern: 'x'\nmatch: 'count:12'\ntarget: files\n")}},
		{"target mock_calls", map[string]string{"graders/not-in-trace.md": authoredGrader("type: regex\npattern: 'x'\ntarget: mock_calls\n")}},
		{"min equals max", map[string]string{"graders/impl-not-read.md": authoredGrader("type: tool_used\ntool: Read\nmin: 2\nmax: 2\n")}},
		{"max 1 without min", map[string]string{"graders/impl-not-read.md": authoredGrader("type: tool_used\ntool: Read\nmax: 1\n")}},
		{"fractional weight with-only arm", map[string]string{"graders/matrix-written.md": authoredGrader("type: file_exists\npath: artefacts/test-matrix.md\nweight: 0.5\narm: with-only\n")}},
		{"llm criteria without body", map[string]string{"graders/review.md": authoredGrader("type: llm\ncriteria: PASS always.\nfocus: trace\n")}},
		{"scaffold_script absent", map[string]string{"case.yaml": "schema_version: \"1.1\"\nname: demo-case\n"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			authoredWantNoError(t, authoredValidate(t, authoredPlugin(), tc.overrides))
		})
	}
}

// V6: regex patterns and input_match values must compile as JavaScript.
func TestAuthoredValidateRegexCompile(t *testing.T) {
	bad := []struct {
		name, file, body, part string
	}{
		{"unbalanced open paren", "graders/not-in-trace.md", authoredGrader("type: regex\npattern: '(unclosed'\ntarget: trace\n"), "does not compile"},
		{"unbalanced bracket", "graders/not-in-trace.md", authoredGrader("type: regex\npattern: '[abc'\ntarget: trace\n"), "does not compile"},
		{"unbalanced close paren", "graders/not-in-trace.md", authoredGrader("type: regex\npattern: 'a)'\ntarget: trace\n"), "does not compile"},
		{"broken group inside lookahead", "graders/not-in-trace.md", authoredGrader("type: regex\npattern: '(?=(abc)'\ntarget: trace\n"), "does not compile"},
		{"unclosed lookbehind", "graders/not-in-trace.md", authoredGrader("type: regex\npattern: '(?<=a'\ntarget: trace\n"), "does not compile"},
		{"input_match unbalanced", "graders/impl-not-read.md", authoredGrader("type: tool_used\ntool: Read\ninput_match: '(['\n"), "does not compile"},
		{"inline flag in pattern", "graders/not-in-trace.md", authoredGrader("type: regex\npattern: '(?i)secret'\ntarget: trace\n"), "inline flag"},
		{"inline flag in input_match", "graders/impl-not-read.md", authoredGrader("type: tool_used\ntool: Read\ninput_match: '(?s)impl.*'\n"), "inline flag"},
	}
	for _, tc := range bad {
		t.Run(tc.name, func(t *testing.T) {
			errs := authoredValidate(t, authoredPlugin(), map[string]string{tc.file: tc.body})
			authoredWantError(t, errs, tc.part, authoredCaseRel+"/"+tc.file)
		})
	}
	good := []string{
		`(?=a)b`,
		`(?!a)b`,
		`(?<=a)b`,
		`(?<!a)b`,
		`(?=[\s\S]*\bR1\b)(?=[\s\S]*\bR2\b)(?!x)`,
		`^#+ *Decision\d{0,3}$`,
		`func prune\(`,
	}
	for _, pattern := range good {
		t.Run("valid "+pattern, func(t *testing.T) {
			errs := authoredValidate(t, authoredPlugin(), map[string]string{
				"graders/not-in-trace.md":  authoredGrader("type: regex\npattern: '" + pattern + "'\nflags: m\ntarget: trace\n"),
				"graders/impl-not-read.md": authoredGrader("type: tool_used\ntool: Read\ninput_match: '" + pattern + "'\nmin: 0\nmax: 0\n"),
			})
			authoredWantNoError(t, errs)
		})
	}
}

// V7: file_exists paths and file targets/focus must be owned paths.
func TestAuthoredValidateOwnedPaths(t *testing.T) {
	bad := []struct {
		name, file, body string
	}{
		{"file_exists unowned", "graders/matrix-written.md", authoredGrader("type: file_exists\npath: src/other.go\n")},
		{"file_exists input path", "graders/matrix-written.md", authoredGrader("type: file_exists\npath: testdata/contract/interface-contract.md\n")},
		{"file_exists next bullet path", "graders/matrix-written.md", authoredGrader("type: file_exists\npath: docs/inputs.md\n")},
		{"regex target unowned", "graders/not-in-trace.md", authoredGrader("type: regex\npattern: 'x'\ntarget: { source: file, path: artefacts/other.md }\n")},
		{"llm focus unowned", "graders/review.md", authoredGrader("type: llm\ncriteria: PASS always.\nfocus: { source: file, path: demo/other.go }\n")},
	}
	for _, tc := range bad {
		t.Run(tc.name, func(t *testing.T) {
			errs := authoredValidate(t, authoredPlugin(), map[string]string{tc.file: tc.body})
			authoredWantError(t, errs, "not an owned path", authoredCaseRel+"/"+tc.file)
		})
	}
}

// V7: without an Owned paths bullet, artefacts/ paths are accepted and
// others rejected.
func TestAuthoredValidateOwnedPathsDefaultToArtefacts(t *testing.T) {
	body := `Dispatch the ^architect^ agent once (^DISPATCH^) with this brief:
- Request: design something in ^testdata/svc^.
- Out of scope: billing.
`
	files := map[string]string{
		"case.yaml":   authoredCaseYAML,
		"prompt.md":   authoredPrompt(`subagent_type: "agent-workflows:architect"`, body),
		"scaffold.sh": "#!/bin/sh\n",
		"graders/dispatched.md": authoredGrader(`type: tool_used
tool: Agent
input_match: '"subagent_type"\s*:\s*"(?:[\w-]+:)?architect"'
`),
		"graders/design-written.md": authoredGrader("type: file_exists\npath: artefacts/design.md\n"),
		"graders/design-quality.md": authoredGrader("type: llm\ncriteria: PASS if good.\nfocus: { source: file, path: artefacts/design.md }\n"),
		"graders/sections.md":       authoredGrader("type: regex\npattern: 'Decision'\ntarget: { source: file, path: artefacts/interface-contract.md }\n"),
	}
	root, caseDir := authoredWriteCase(t, files)
	authoredWantNoError(t, validateCase(root, caseDir, authoredPlugin()))

	files["graders/code-written.md"] = authoredGrader("type: file_exists\npath: svc/limiter.go\n")
	files["graders/code-quality.md"] = authoredGrader("type: llm\ncriteria: PASS if good.\nfocus: { source: file, path: testdata/svc/main.go }\n")
	root, caseDir = authoredWriteCase(t, files)
	errs := validateCase(root, caseDir, authoredPlugin())
	authoredWantError(t, errs, "not an owned path", authoredCaseRel+"/graders/code-written.md")
	authoredWantError(t, errs, "not an owned path", authoredCaseRel+"/graders/code-quality.md")
}

// V8: dispatched agents must be listed in plugin.json under its name.
func TestAuthoredValidateDispatchedAgent(t *testing.T) {
	bad := []struct {
		name, dispatch, part string
	}{
		{"unknown agent with prefix", `subagent_type: "agent-workflows:ghost"`, "not listed in plugin.json"},
		{"other plugin prefix", `subagent_type: "other-plugin:test-engineer"`, "not listed in plugin.json"},
		{"unknown bare agent", `subagent_type: "ghost"`, "not listed in plugin.json"},
		{"no dispatch", `no dispatch here`, "dispatches no agent"},
	}
	for _, tc := range bad {
		t.Run(tc.name, func(t *testing.T) {
			errs := authoredValidate(t, authoredPlugin(), map[string]string{"prompt.md": authoredPrompt(tc.dispatch, authoredBody)})
			authoredWantError(t, errs, tc.part, authoredCaseRel+"/prompt.md")
		})
	}
	t.Run("bare listed agent", func(t *testing.T) {
		errs := authoredValidate(t, authoredPlugin(), map[string]string{"prompt.md": authoredPrompt(`subagent_type: "test-engineer"`, authoredBody)})
		authoredWantNoError(t, errs)
	})
	t.Run("two listed agents", func(t *testing.T) {
		dispatch := `subagent_type: "agent-workflows:test-engineer"^, then (^subagent_type: "agent-workflows:cw-reviewer"`
		errs := authoredValidate(t, authoredPlugin(), map[string]string{"prompt.md": authoredPrompt(dispatch, authoredBody)})
		authoredWantNoError(t, errs)
	})
	t.Run("second agent unlisted", func(t *testing.T) {
		dispatch := `subagent_type: "agent-workflows:test-engineer"^, then (^subagent_type: "agent-workflows:ghost"`
		errs := authoredValidate(t, authoredPlugin(), map[string]string{"prompt.md": authoredPrompt(dispatch, authoredBody)})
		authoredWantError(t, errs, "not listed in plugin.json", authoredCaseRel+"/prompt.md")
	})
}

// V8: the plugin name and agent set come from pluginInfo, not constants.
func TestAuthoredValidateUsesGivenPluginInfo(t *testing.T) {
	p := pluginInfo{Name: "demo-plugin", Agents: map[string]bool{"test-engineer": true}}
	errs := authoredValidate(t, p, map[string]string{"prompt.md": authoredPrompt(`subagent_type: "demo-plugin:test-engineer"`, authoredBody)})
	authoredWantNoError(t, errs)

	errs = authoredValidate(t, p, nil) // dispatches agent-workflows:test-engineer
	authoredWantError(t, errs, "not listed in plugin.json", authoredCaseRel+"/prompt.md")

	errs = authoredValidate(t, pluginInfo{Name: "agent-workflows", Agents: map[string]bool{"architect": true}}, nil)
	authoredWantError(t, errs, "not listed in plugin.json", authoredCaseRel+"/prompt.md")
}

// V9: a tool_used Agent grader must match the dispatched agent's JSON input.
func TestAuthoredValidateAgentGraderMatchesDispatch(t *testing.T) {
	cases := []struct {
		name, dispatch, inputMatch string
		wantErr                    bool
	}{
		{"other agent", authoredDispatch, `'"subagent_type"\s*:\s*"agent-workflows:architect"'`, true},
		{"prefix required but bare dispatch", `subagent_type: "test-engineer"`, `'"subagent_type":"agent-workflows:test-engineer"'`, true},
		{"spacing in JSON assumed", authoredDispatch, `'"subagent_type": "agent-workflows:test-engineer"'`, true},
		{"plain name", authoredDispatch, `'test-engineer'`, false},
		{"optional prefix bare dispatch", `subagent_type: "test-engineer"`, `'"subagent_type"\s*:\s*"(?:[\w-]+:)?test-engineer"'`, false},
		{"exact JSON", authoredDispatch, `'\{"subagent_type":"agent-workflows:test-engineer"\}'`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			errs := authoredValidate(t, authoredPlugin(), map[string]string{
				"prompt.md":             authoredPrompt(tc.dispatch, authoredBody),
				"graders/dispatched.md": authoredGrader("type: tool_used\ntool: Agent\ninput_match: " + tc.inputMatch + "\n"),
			})
			if tc.wantErr {
				authoredWantError(t, errs, "does not match the dispatched agent", authoredCaseRel+"/graders/dispatched.md")
			} else {
				authoredWantNoError(t, errs)
			}
		})
	}
	t.Run("non-Agent tool is not checked", func(t *testing.T) {
		errs := authoredValidate(t, authoredPlugin(), map[string]string{
			"graders/impl-not-read.md": authoredGrader("type: tool_used\ntool: Read\ninput_match: 'architect'\nmin: 0\nmax: 0\n"),
		})
		authoredWantNoError(t, errs)
	})
}

// loadPluginInfo on a synthetic plugin: name from the manifest, agent names
// from each listed file's frontmatter (V8 input).
func TestAuthoredLoadPluginInfoSynthetic(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{
		".claude-plugin/plugin.json": `{"name": "demo-plugin", "version": "1.0.0", "agents": ["./agents/one.md", "./other/two.md"]}`,
		"agents/one.md":              "---\nname: alpha\ndescription: First.\n---\nBody mentioning name: wrong\n",
		"other/two.md":               "---\ndescription: Second.\nname: beta\nmodel: opus\n---\nBody.\n",
		"agents/unlisted.md":         "---\nname: gamma\n---\n",
	}
	for rel, body := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	p, err := loadPluginInfo(root)
	if err != nil {
		t.Fatalf("loadPluginInfo: %v", err)
	}
	if p.Name != "demo-plugin" {
		t.Fatalf("Name = %q, want demo-plugin", p.Name)
	}
	if len(p.Agents) != 2 || !p.Agents["alpha"] || !p.Agents["beta"] {
		t.Fatalf("Agents = %v, want exactly alpha and beta", p.Agents)
	}
}

// findCases: sorted case directories, skipping results, testdata, mocks and
// hidden directories.
func TestAuthoredFindCases(t *testing.T) {
	evalsDir := t.TempDir()
	files := []string{
		"suite-b/case-2/case.yaml",
		"suite-a/case-1/prompt.md",
		"suite-a/case-3/prompt.md",
		"suite-a/case-3/case.yaml",
		"suite-a/not-a-case/graders/x.md",
		"suite-a/case-1/testdata/nested/prompt.md",
		"results/run-1/prompt.md",
		"suite-a/mocks/fake/case.yaml",
		".hidden/case-h/prompt.md",
		"suite-a/.cache/case-c/case.yaml",
	}
	for _, rel := range files {
		p := filepath.Join(evalsDir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got, err := findCases(evalsDir)
	if err != nil {
		t.Fatalf("findCases: %v", err)
	}
	want := []string{
		filepath.Join(evalsDir, "suite-a", "case-1"),
		filepath.Join(evalsDir, "suite-a", "case-3"),
		filepath.Join(evalsDir, "suite-b", "case-2"),
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("findCases = %q, want %q", got, want)
	}
}

// authoredRealCases are the 15 cases the repository ships today.
var authoredRealCases = []string{
	"architect/config-hot-reload",
	"architect/outbox-publisher",
	"architect/rate-limiter-boundary",
	"implementer/bounded-worker-pool",
	"implementer/idempotency-claims",
	"implementer/parse-limits-config",
	"spec-reviewer/audit-export-tasks",
	"spec-reviewer/invoice-reminders-review",
	"spec-reviewer/team-invites-scope",
	"spec-writer/avatar-upload-gaps",
	"spec-writer/bookmark-export-brief",
	"spec-writer/link-expiry-delta",
	"test-engineer/notes-init-e2e",
	"test-engineer/retry-budget-matrix",
	"test-engineer/ttl-cache-matrix",
}

// V10: the real plugin manifest loads and every real case validates.
func TestAuthoredRealCasesValidate(t *testing.T) {
	root := authoredRealRoot(t)
	p, err := loadPluginInfo(root)
	if err != nil {
		t.Fatalf("loadPluginInfo(%s): %v", root, err)
	}
	if p.Name != "agent-workflows" {
		t.Fatalf("plugin Name = %q, want agent-workflows", p.Name)
	}
	want := []string{
		"architect", "implementer", "test-engineer", "spec-writer",
		"spec-reviewer", "cw-github-worker", "cw-reviewer", "cw-test-author",
	}
	if len(p.Agents) != len(want) {
		t.Fatalf("plugin Agents = %v, want exactly %v", p.Agents, want)
	}
	for _, a := range want {
		if !p.Agents[a] {
			t.Fatalf("plugin Agents = %v, missing %q", p.Agents, a)
		}
	}
	if len(authoredRealCases) != 15 {
		t.Fatalf("authoredRealCases lists %d cases, want 15", len(authoredRealCases))
	}

	dirs, err := findCases(filepath.Join(root, "evals"))
	if err != nil {
		t.Fatalf("findCases: %v", err)
	}
	if !sort.StringsAreSorted(dirs) {
		t.Fatalf("findCases result is not sorted: %q", dirs)
	}
	found := map[string]bool{}
	for _, d := range dirs {
		rel, err := filepath.Rel(filepath.Join(root, "evals"), d)
		if err != nil {
			t.Fatal(err)
		}
		found[filepath.ToSlash(rel)] = true
	}
	for _, c := range authoredRealCases {
		if !found[c] {
			t.Errorf("findCases did not return %s; got %q", c, dirs)
		}
	}
	if len(dirs) != len(authoredRealCases) {
		t.Errorf("findCases returned %d cases, want the %d in authoredRealCases: %q", len(dirs), len(authoredRealCases), dirs)
	}
	for _, d := range dirs {
		if errs := validateCase(root, d, p); len(errs) != 0 {
			t.Errorf("validateCase(%s):%s", d, authoredErrText(errs))
		}
	}
}

// V10: a copy of each real case under a temporary repository root validates
// against the real plugin's pluginInfo.
func TestAuthoredRealCaseCopiesValidate(t *testing.T) {
	root := authoredRealRoot(t)
	p, err := loadPluginInfo(root)
	if err != nil {
		t.Fatalf("loadPluginInfo: %v", err)
	}
	for _, c := range authoredRealCases {
		t.Run(c, func(t *testing.T) {
			src := filepath.Join(root, "evals", filepath.FromSlash(c))
			tmpRoot := t.TempDir()
			dst := filepath.Join(tmpRoot, "evals", filepath.FromSlash(c))
			names := []string{"case.yaml", "prompt.md", "scaffold.sh"}
			graders, err := filepath.Glob(filepath.Join(src, "graders", "*.md"))
			if err != nil {
				t.Fatal(err)
			}
			for _, g := range graders {
				names = append(names, filepath.Join("graders", filepath.Base(g)))
			}
			for _, n := range names {
				data, err := os.ReadFile(filepath.Join(src, n))
				if err != nil {
					t.Fatal(err)
				}
				out := filepath.Join(dst, n)
				if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(out, data, 0o644); err != nil {
					t.Fatal(err)
				}
			}
			if errs := validateCase(tmpRoot, dst, p); len(errs) != 0 {
				t.Fatalf("validateCase of copy:%s", authoredErrText(errs))
			}
		})
	}
}
