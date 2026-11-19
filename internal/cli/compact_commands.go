package cli

import (
	"fmt"
	"strings"
)

func compactCommands() []uxCommand {
	json := uxOption("json", "", "print JSON; collection, show and brief always use JSON")
	budget := uxOption("max-bytes", "bytes", "maximum JSON response bytes including metadata; default 16384, range 512 to 1048576")
	return []uxCommand{
		{path: "context read", args: "<path>", summary: "Read bounded original source without writing a JSON request", operands: [][2]string{{"path", "repository-relative source file; use -- before a path beginning with -"}}, flags: []uxFlag{uxOption("start", "line", "first inclusive line; omitted starts at 1"), uxOption("end", "line", "last inclusive line; omitted reads through the end"), uxOption("task", "task", "include the saved task state"), budget, json}, examples: []string{"kitsu context read internal/server.go --start 10 --end 60"}, run: runContextRead},
		{path: "context search", args: "<pattern> [<path>]", summary: "Search eligible source literally, or with an explicit Go regular expression", operands: [][2]string{{"pattern", "literal text by default; use -- before a pattern beginning with -"}, {"path", "optional repository-relative file or directory; omitted searches all eligible sources"}}, flags: []uxFlag{uxOption("regex", "", "interpret the pattern as a Go regular expression"), uxOption("task", "task", "include the saved task state"), budget, json}, examples: []string{"kitsu context search register internal", "kitsu context search -- -flag"}, run: runContextSearch},
		{path: "context collect", args: "--request <file|->", summary: "Batch repository reads, searches and Git queries in the built-in Go engine", flags: []uxFlag{uxOption("request", "file|-", "required version 1 JSON request; - reads standard input"), uxOption("task", "task", "include the saved task state"), budget, json}, examples: []string{"kitsu context collect --request context.json --json"}, run: runContextCollect},
		{path: "context show", args: "<artifact-id>", summary: "Inspect retained evidence without rerunning operations or checks", operands: [][2]string{{"artifact-id", "private artifact id returned by collect, brief or compact checks"}}, flags: []uxFlag{uxOption("item", "item-id", "show one named operation or check log"), uxOption("cursor", "cursor", "next_cursor from this artifact and item"), budget, json}, examples: []string{"kitsu context show <artifact-id> --item source --json"}, run: runContextShow},
		{path: "task brief", args: "<task>", summary: "Prepare a fresh worker brief and record skill selection before its baseline", flags: []uxFlag{uxOption("host", "codex|claude", "host if the repository does not configure this role", "codex", "claude"), uxOption("role", "role", "required specialist role"), uxOption("stage", "stage", "required explicit worker stage", "lookup", "spec", "spec-review", "design", "design-review", "tests", "code", "review", "commit"), uxOption("round", "number", "required positive worker round"), uxOption("workflow", "full|small", "full requires a task folder; small cannot infer saved workflow state", "full", "small"), uxOption("assignment", "file|-", "UTF-8 assignment file or standard input; required with --save"), uxOption("save", "", "save complete instructions once and return only dispatch metadata; requires --assignment"), budget, json}, examples: []string{"kitsu task brief fix-login --role developer --stage code --round 1 --host codex --workflow small --assignment assignment.txt --save"}, run: runTaskBrief},
	}
}

func compactCheckFlags() []uxFlag {
	return []uxFlag{uxOption("compact", "", "retain logs privately and return bounded diagnostics"), uxOption("json", "", "select compact JSON output automatically"), uxOption("max-bytes", "bytes", "compact JSON byte limit; default 16384")}
}

func compactRunHelp() string {
	var out strings.Builder
	out.WriteString("\n")
	for _, option := range [][2]string{
		{"<id>...", "consider only these checks"},
		{"--force", "execute even when passing evidence exists"},
		{"--base <rev>", "count unchanged checked content as passed on this revision"},
		{"--compact", "retain logs privately and return bounded JSON diagnostics"},
		{"--json", "select compact JSON output automatically"},
		{"--max-bytes <bytes>", "compact JSON limit including metadata; default 16384, range 512 to 1048576"},
	} {
		fmt.Fprintf(&out, "  %-21s%s\n", option[0], option[1])
	}
	return out.String()
}
