package cli

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/belevtsev/kitsu/v3/internal/contextpack"
	"github.com/belevtsev/kitsu/v3/internal/kitsutest"
)

// The retained high-pilot traces reported items/reads, op/type, glob, paths,
// start_line, args/base, omitted operation ids and missing operations. Keep the
// observed failures at the public CLI boundary as well as testing the decoder.
func TestCompactCollectorRequestErrorsDoNotWriteAndTypedCommandsRecover(t *testing.T) {
	t.Parallel()
	r, _ := skillRepo(t)
	const source = "first\nneedle λ\nlast\n"
	r.Write("source.txt", source)
	before := r.Git("status", "--porcelain")
	cases := []struct {
		name, request, diagnostic string
	}{
		{"missing operations", `{"version":1}`, "1 to 32 operations"},
		{"empty operations", `{"version":1,"operations":[]}`, "1 to 32 operations"},
		{"null operations", `{"version":1,"operations":null}`, "1 to 32 operations"},
		{"object operations", `{"version":1,"operations":{"id":"source","kind":"read","path":"source.txt"}}`, "cannot unmarshal object"},
		{"string operations", `{"version":1,"operations":"source.txt"}`, "cannot unmarshal string"},
		{"wrong operation shape", `{"version":1,"operations":["source.txt"]}`, "cannot unmarshal string"},
		{"missing id", `{"version":1,"operations":[{"kind":"read","path":"source.txt"}]}`, "operation ids must be unique, 1 to 64"},
		{"empty id", `{"version":1,"operations":[{"id":"","kind":"git"}]}`, "operation ids must be unique, 1 to 64"},
		{"path as id", `{"version":1,"operations":[{"id":"src/main.go","kind":"git"}]}`, "letters, digits, hyphens or underscores"},
		{"unicode id", `{"version":1,"operations":[{"id":"λ","kind":"git"}]}`, "letters, digits, hyphens or underscores"},
		{"duplicate ids", `{"version":1,"operations":[{"id":"same","kind":"git"},{"id":"same","kind":"read","path":"source.txt"}]}`, "operation ids must be unique"},
		{"oversized id", fmt.Sprintf(`{"version":1,"operations":[{"id":%q,"kind":"git"}]}`, strings.Repeat("x", 65)), "1 to 64"},
		{"wrong version", `{"version":2,"operations":[{"id":"state","kind":"git"}]}`, "version 1"},
		{"second document", `{"version":1,"operations":[{"id":"state","kind":"git"}]} {}`, "one JSON object"},
		{"empty input", "", "EOF"},
	}
	for _, field := range []string{"items", "reads", "max_tokens"} {
		cases = append(cases, struct{ name, request, diagnostic string }{
			"unknown root " + field,
			fmt.Sprintf(`{"version":1,"operations":[{"id":"source","kind":"read","path":"source.txt"}],%q:[]}`, field),
			fmt.Sprintf("unknown field %q", field),
		})
	}
	for _, field := range []string{"op", "type", "glob", "paths", "start_line", "args", "base", "max_tokens"} {
		cases = append(cases, struct{ name, request, diagnostic string }{
			"unknown operation " + field,
			fmt.Sprintf(`{"version":1,"operations":[{"id":"source","kind":"read","path":"source.txt",%q:1}]}`, field),
			fmt.Sprintf("unknown field %q", field),
		})
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			result := r.Run(test.request, filepath.Join(kitsutest.Binary(t), compactBinaryName()), "context", "collect", "--request", "-", "--task", "fix-login", "--json")
			if result.Code != ExitUsage || result.Stdout != "" || !strings.Contains(result.Stderr, "context request:") || !strings.Contains(result.Stderr, test.diagnostic) || !strings.Contains(result.Stderr, "usage: kitsu context collect --request") {
				t.Fatalf("request error lacks its actionable reason/usage: code=%d; %s", result.Code, result.Output())
			}
			for _, path := range []string{".git/kitsu", ".git/kitsu-context", ".git/kitsu-stages", "openspec/changes/.skills"} {
				if r.Exists(path) {
					t.Errorf("invalid request created private state or a selection receipt: %s", path)
				}
			}
			if r.Read("source.txt") != source || r.Git("status", "--porcelain") != before {
				t.Fatal("invalid request changed source or task records")
			}
		})
	}
	read := compactResult(t, r.Kitsu("context", "read", "source.txt", "--start", "2", "--end", "2", "--json"), 0)
	search := compactResult(t, r.Kitsu("context", "search", "needle λ", "source.txt", "--json"), 0)
	for _, page := range []struct {
		name string
		body contextpack.Page
	}{
		{"read", read},
		{"search", search},
	} {
		if len(page.body.Items) != 1 || page.body.Items[0].Status != "ok" || len(page.body.Items[0].Lines) != 1 || page.body.Items[0].Lines[0].Number != 2 || page.body.Items[0].Lines[0].Text != "needle λ\n" {
			t.Errorf("typed %s failed to recover after rejected raw requests: %+v", page.name, page.body)
		}
	}
	if r.Exists("openspec/changes/.skills") || r.Read("source.txt") != source || r.Git("status", "--porcelain") != before {
		t.Fatal("typed recovery changed source or selection records")
	}
}
