package cli

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"unicode"

	"github.com/belevtsev/kitsu/v3/internal/kitsutest"
)

func TestUXCompletionCommandMetadata(t *testing.T) {
	tests := []struct {
		name         string
		words        []string
		want, absent []string
	}{
		{name: "root", words: []string{""}, want: []string{"role", "skill", "task", "completion", "help", "version", "--help", "-h"}, absent: []string{"roles", "__complete", "consult"}},
		{name: "group", words: []string{"role", "s"}, want: []string{"set", "show"}, absent: []string{"list"}},
		{name: "nested help", words: []string{"help", "skill", "a"}, want: []string{"attach"}},
		{name: "group help", words: []string{"role", "help", "s"}, want: []string{"set", "show"}},
		{name: "flag", words: []string{"role", "set", "developer", "--mo"}, want: []string{"--model"}},
		{name: "flag value", words: []string{"role", "set", "--host", "co"}, want: []string{"codex"}, absent: []string{"--host"}},
		{name: "equal flag value", words: []string{"role", "set", "--host=cl"}, want: []string{"--host=claude"}},
		{name: "value is not flag", words: []string{"role", "set", "--host", "--"}, absent: []string{"--help", "--model"}},
		{name: "role after flag value", words: []string{"role", "set", "--host", "codex", "dev"}, want: []string{"developer"}},
		{name: "role after bool", words: []string{"role", "show", "--json", "co"}, want: []string{"coordinator"}},
		{name: "literal marker", words: []string{"role", "text", "--", "--"}, absent: []string{"--help", "--host"}},
		{name: "key", words: []string{"config", "get", "h"}, want: []string{"host"}},
		{name: "key value", words: []string{"config", "set", "host", "cl"}, want: []string{"claude"}},
		{name: "shell", words: []string{"completion", "p"}, want: []string{"powershell"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			values := uxCompletionTestValues(uxCompleteCandidates(context.Background(), test.words))
			uxRequireSuggestions(t, values, test.want, test.absent)
		})
	}
	for _, command := range uxCommands() {
		if command.hidden {
			continue
		}
		words := append(strings.Fields(command.path), "--")
		values := uxCompletionTestValues(uxCompleteCandidates(context.Background(), words))
		if !slices.Contains(values, "--help") {
			t.Errorf("%s omits --help: %v", command.path, values)
		}
	}
}

func TestUXCompletionShellWords(t *testing.T) {
	tests := []struct {
		line string
		want []string
	}{
		{line: "kitsu role set --host=co", want: []string{"role", "set", "--host=co"}},
		{line: "kitsu skill show codex:repo:.agents/skills/re", want: []string{"skill", "show", "codex:repo:.agents/skills/re"}},
		{line: "kitsu adviser ask --request 'my file", want: []string{"adviser", "ask", "--request", "my file"}},
		{line: "kitsu adviser ask --request 'my file ", want: []string{"adviser", "ask", "--request", "my file "}},
		{line: "kitsu adviser ask --request my\\ file", want: []string{"adviser", "ask", "--request", "my file"}},
		{line: "kitsu adviser ask --request \"$(touch marker);literal\"", want: []string{"adviser", "ask", "--request", "$(touch marker);literal"}},
		{line: "kitsu role list | kitsu help role ", want: []string{"help", "role", ""}},
		{line: "kitsu role set --model '' ", want: []string{"role", "set", "--model", "", ""}},
	}
	for _, test := range tests {
		if got := uxCompletionWords(test.line, "bash"); !reflect.DeepEqual(got, test.want) {
			t.Errorf("%q -> %q, want %q", test.line, got, test.want)
		}
	}
}

func TestUXCompletionCallbackProtocolAndWordbreaks(t *testing.T) {
	encoded := base64.StdEncoding.EncodeToString([]byte(`["role","text","co"]`))
	tests := []struct {
		args []string
		want string
	}{
		{args: []string{"--format", "lines", "--", "role", "text", "co"}, want: "coordinator\n"},
		{args: []string{"--format", "lines", "--words-base64", encoded}, want: "coordinator\n"},
		{args: []string{"--shell", "bash", "--format", "lines", "--line", "kitsu role set --host=co", "--word", "co"}, want: "codex\n"},
		{args: []string{"--shell", "bash", "--format", "lines", "--line", "kitsu role set --host=", "--word", ""}, want: "claude\ncodex\n"},
		{args: []string{"--shell", "unknown", "--", ""}},
		{args: []string{"--format", "unknown", "--", ""}},
		{args: []string{"--unexpected", "value", "--", ""}},
		{args: []string{"--words-base64", "not base64"}},
	}
	for _, test := range tests {
		var out, diagnostic bytes.Buffer
		if err := runUXComplete(context.Background(), Config{Out: &out, Err: &diagnostic}, test.args); err != nil {
			t.Fatal(err)
		}
		if out.String() != test.want || diagnostic.Len() != 0 {
			t.Fatalf("%q -> stdout %q stderr %q, want %q", test.args, out.String(), diagnostic.String(), test.want)
		}
	}
}

func TestUXCompletionUsesLocalModelsSkillsAndReferences(t *testing.T) {
	repo, personal := skillRepo(t)
	repo.Setenv("KITSU_HOST", "codex")
	repo.Write("kitsu-roles.toml", "version = 1\n[codex.developer]\nmodel = 'private-custom-model'\nskills = ['missing-reference']\n[claude.developer]\nmodel = 'claude-private-model'\n")
	if err := os.MkdirAll(filepath.Dir(personal), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(personal, []byte("version = 1\n[codex.developer]\nskills = ['global-missing-reference']\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	writeSkillFixture(t, repo.Path(".agents/skills/available/SKILL.md"), "available")
	repo.Write(".agents/skills/available/SKILL.md", "---\nname: available\ndescription: \"Readable \\u001b[31mmetadata\\ttext\"\n---\n")
	writeSkillFixture(t, repo.Path(".agents/skills/blocked/SKILL.md"), "blocked")
	repo.Write(".agents/skills/manual/SKILL.md", "---\nname: manual\ndescription: Manual only\ndisable-model-invocation: true\n---\n")
	repo.Write(".codex/config.toml", "[[skills.config]]\nname = 'blocked'\nenabled = false\n")
	repo.Write(".agents/skills/available/scripts/forbidden.sh", "#!/bin/sh\ntouch completion-executed\n")
	queries := []struct{ words, want, absent []string }{
		{words: []string{"role", "set", "developer", "--model", ""}, want: []string{"private-custom-model", "gpt-6.1-sol"}, absent: []string{"claude-private-model", "opus"}},
		{words: []string{"role", "set", "developer", "--host=claude", "--model", "claude"}, want: []string{"claude-private-model"}, absent: []string{"private-custom-model"}},
		{words: []string{"skill", "attach", "--role", "developer", ""}, want: []string{"available", "codex:repo:.agents/skills/available"}, absent: []string{"blocked", "manual", "missing-reference"}},
		{words: []string{"skill", "show", ""}, want: []string{"available", "blocked", "manual"}},
		{words: []string{"skill", "detach", "--role", "developer", ""}, want: []string{"missing-reference"}, absent: []string{"available", "global-missing-reference"}},
		{words: []string{"skill", "detach", "--global", "--role", "developer", ""}, want: []string{"global-missing-reference"}, absent: []string{"missing-reference"}},
		{words: []string{"role", "set", "developer", "--skills=first,av"}, want: []string{"--skills=first,available"}},
	}
	before := repo.Read("kitsu-roles.toml")
	for _, query := range queries {
		uxRequireSuggestions(t, uxCompletionTestValues(uxQueryRepoCompletion(t, repo, query.words)), query.want, query.absent)
	}
	if repo.Read("kitsu-roles.toml") != before || repo.Exists("completion-executed") || repo.Exists("openspec/changes/.skills") {
		t.Fatal("completion wrote configuration, receipts, or executed skill code")
	}
}

func TestUXCompletionTasksAdvisersChecksAndPaths(t *testing.T) {
	repo, _ := skillRepo(t)
	repo.Write("openspec/changes/active-task/proposal.md", "task")
	repo.Write("openspec/changes/archive/archived-task/proposal.md", "archive")
	repo.Write("openspec/changes/.advice/small-task/record", "record")
	repo.Write("openspec/changes/.skills/skill-task/record", "record")
	repo.Write("kitsu.toml", "version = 1\n[[check]]\nid = 'unit'\nrun = ['never-execute-check']\n")
	repo.Write("kitsu-advisers.toml", "version = 1\n[advisers.local]\nmodel = 'custom-adviser-model'\ncommand = ['never-execute-adviser', '{model}']\n[access]\ncoordinator = ['local']\n[[checkpoints]]\nid = 'before-review'\nstage = 'review'\nrole = 'coordinator'\nadvisers = ['local']\n")
	repo.Write("my request $(touch completion-executed).json", "{}")
	repo.Write("my directory/file", "content")
	repo.Write("my directory/nested/file", "content")
	directory := filepath.FromSlash("my directory/")
	nestedDirectory := filepath.FromSlash("my directory/nested/")
	file := filepath.FromSlash("my directory/file")
	queries := []struct {
		name                string
		words, want, absent []string
	}{
		{name: "tasks", words: []string{"task", "show", ""}, want: []string{"active-task", "small-task", "skill-task"}, absent: []string{"archive", "archived-task"}},
		{name: "advisers", words: []string{"adviser", "ask", "l"}, want: []string{"local"}},
		{name: "checkpoints", words: []string{"adviser", "ask", "local", "--checkpoint", "b"}, want: []string{"before-review"}},
		{name: "checks", words: []string{"check", "run", "u"}, want: []string{"unit"}},
		{name: "request paths", words: []string{"adviser", "ask", "local", "--request", "my "}, want: []string{directory, "my request $(touch completion-executed).json"}, absent: []string{"my directory"}},
		{name: "root directories", words: []string{"skill", "list", "--root", "my "}, want: []string{directory}, absent: []string{"my request $(touch completion-executed).json"}},
		{name: "literal paths", words: []string{"check", "add", "new", "--", "my "}, want: []string{directory, "my request $(touch completion-executed).json"}, absent: []string{"--help", "--timeout"}},
		{name: "nested paths", words: []string{"adviser", "ask", "local", "--request", directory}, want: []string{file, nestedDirectory}, absent: []string{file + string(filepath.Separator)}},
		{name: "nested directories", words: []string{"skill", "list", "--root", directory}, want: []string{nestedDirectory}, absent: []string{file}},
	}
	for _, query := range queries {
		t.Run(query.name, func(t *testing.T) {
			uxRequireSuggestions(t, uxCompletionTestValues(uxQueryRepoCompletion(t, repo, query.words)), query.want, query.absent)
		})
	}
	if repo.Exists("completion-executed") {
		t.Fatal("completion executed a path or provider")
	}
}

func TestUXCompletionMalformedConfigurationIsQuiet(t *testing.T) {
	repo, _ := skillRepo(t)
	repo.Setenv("KITSU_HOST", "codex")
	repo.Write("kitsu-roles.toml", "secret-completion-token = [broken")
	repo.Write("kitsu-advisers.toml", "secret-completion-token = [broken")
	repo.Write("kitsu.toml", "secret-completion-token = [broken")
	for _, words := range [][]string{{"role", "set", "developer", "--model", ""}, {"adviser", "ask", ""}, {"check", "run", ""}} {
		candidates := uxQueryRepoCompletion(t, repo, words)
		if strings.Contains(strings.Join(uxCompletionTestValues(candidates), " "), "secret-completion-token") {
			t.Fatal("completion exposed malformed configuration contents")
		}
	}
}

func uxQueryRepoCompletion(t *testing.T, repo *kitsutest.Repo, words []string) []uxSuggestion {
	t.Helper()
	result := repo.Kitsu(append([]string{"__complete", "--format", "json", "--"}, words...)...)
	if result.Code != 0 || result.Stderr != "" {
		t.Fatalf("completion %q: %s", words, result.Output())
	}
	decoder, candidates := json.NewDecoder(strings.NewReader(result.Stdout)), []uxSuggestion{}
	for {
		var candidate uxSuggestion
		if err := decoder.Decode(&candidate); err == io.EOF {
			return candidates
		} else if err != nil {
			t.Fatalf("completion emitted non-JSON output %q: %v", result.Stdout, err)
		}
		if strings.ContainsFunc(candidate.Value+candidate.Description, unicode.IsControl) {
			t.Fatalf("completion emitted terminal control characters: %+v", candidate)
		}
		candidates = append(candidates, candidate)
	}
}

func uxCompletionTestValues(candidates []uxSuggestion) []string {
	values := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		values = append(values, candidate.Value)
	}
	return values
}

func uxRequireSuggestions(t *testing.T, values, want, absent []string) {
	t.Helper()
	for _, value := range want {
		if !slices.Contains(values, value) {
			t.Errorf("missing %q in %q", value, values)
		}
	}
	for _, value := range absent {
		if slices.Contains(values, value) {
			t.Errorf("unexpected %q in %q", value, values)
		}
	}
}
