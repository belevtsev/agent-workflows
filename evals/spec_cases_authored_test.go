package evals

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Authored tests for the spec writer's change folder (S1-S10) and nested
// counted repeats in compileJS (N1-N8). Contract v2, test-author-3-rev.

// authored3Plugin lists the agents the spec fixtures dispatch.
func authored3Plugin() pluginInfo {
	return pluginInfo{
		Name: "agent-workflows",
		Agents: map[string]bool{
			"spec-writer":   true,
			"spec-reviewer": true,
			"test-engineer": true,
		},
	}
}

const authored3Dispatch = `subagent_type: "agent-workflows:spec-writer"`

const authored3DispatchGrader = `---
type: tool_used
tool: Agent
input_match: '"subagent_type"\s*:\s*"(?:[\w-]+:)?spec-writer"'
---
`

// authored3Body builds a spec-writer brief. folder is the backticked change
// folder (omitted when empty); owned is the backticked owned file (the
// Owned paths bullet is omitted when empty).
func authored3Body(folder, owned string) string {
	var bullets []string
	if folder != "" {
		bullets = append(bullets, "- Change folder: ^"+folder+"^.")
	}
	return authored3BodyLines(owned, bullets...)
}

// authored3BodyLines builds a spec-writer brief with the given extra bullet
// lines (^ stands for a backtick) after the optional Owned paths bullet.
func authored3BodyLines(owned string, bullets ...string) string {
	var b strings.Builder
	b.WriteString("You are the coordinator. Dispatch the ^spec-writer^ agent once through the\n")
	b.WriteString("Agent tool (^DISPATCH^) with the brief below.\n\n")
	b.WriteString("Brief for the spec writer:\n")
	b.WriteString("- Goal: write the OpenSpec change for exporting demos.\n")
	b.WriteString("- Source documents: ^testdata/brief.md^.\n")
	if owned != "" {
		b.WriteString("- Owned paths: ^" + owned + "^.\n")
	}
	for _, l := range bullets {
		b.WriteString(l + "\n")
	}
	b.WriteString("- Stop condition: every requirement has a scenario.\n")
	return b.String()
}

// authored3Files is a valid spec-writer case with the given brief body and
// extra graders (name -> grader file content).
func authored3Files(body string, graders map[string]string) map[string]string {
	files := map[string]string{
		"case.yaml":                     authoredCaseYAML,
		"prompt.md":                     authoredPrompt(authored3Dispatch, body),
		"scaffold.sh":                   "#!/bin/sh\nset -eu\n",
		"graders/dispatched-writer.md":  authored3DispatchGrader,
		"graders/no-secret-in-trace.md": authoredGrader("type: regex\npattern: 'secretToken'\nmatch: not_contains\ntarget: trace\n"),
	}
	for k, v := range graders {
		files["graders/"+k] = v
	}
	return files
}

func authored3Validate(t *testing.T, folder, owned string, graders map[string]string) []error {
	t.Helper()
	return authored3ValidateBody(t, authored3Body(folder, owned), graders)
}

func authored3ValidateBody(t *testing.T, body string, graders map[string]string) []error {
	t.Helper()
	root, caseDir := authoredWriteCase(t, authored3Files(body, graders))
	return validateCase(root, caseDir, authored3Plugin())
}

// authored3WantUnclean checks that grader file name is rejected as not a
// clean relative path and that the owned-path rule was not consulted.
func authored3WantUnclean(t *testing.T, errs []error, name string) {
	t.Helper()
	authoredWantError(t, errs, "must be a relative path", authoredCaseRel+"/graders/"+name)
	authoredWantNoErrorContaining(t, errs, "is not an owned path")
}

// authored3Target is a regex grader reading file p.
func authored3Target(p string) string {
	return authoredGrader("type: regex\npattern: 'Requirement'\ntarget: { source: file, path: " + p + " }\n")
}

// authored3Exists is a file_exists grader for p.
func authored3Exists(p string) string {
	return authoredGrader("type: file_exists\npath: " + p + "\n")
}

// authored3Focus is an llm grader focused on file p.
func authored3Focus(p string) string {
	return authoredGrader("type: llm\ncriteria: PASS if the file is complete.\nfocus: { source: file, path: " + p + " }\n")
}

const authored3Folder = "testdata/demo/openspec/changes/add-demo-export/"

// Baseline: the spec-writer fixture with no file graders is valid, so every
// error below comes from the path under test.
func TestAuthored3SpecFixtureIsValid(t *testing.T) {
	authoredWantNoError(t, authored3Validate(t, authored3Folder, "", nil))
	authoredWantNoError(t, authored3Validate(t, authored3Folder, "artefacts/handoff.json", nil))
}

// S1: paths inside the change folder are accepted when the brief has only a
// change folder.
func TestAuthored3ChangeFolderAccepted(t *testing.T) {
	errs := authored3Validate(t, authored3Folder, "", map[string]string{
		"proposal-sections.md": authored3Target(authored3Folder + "proposal.md"),
		"spec-written.md":      authored3Exists(authored3Folder + "specs/x/spec.md"),
		"spec-format.md":       authored3Target(authored3Folder + "specs/x/spec.md"),
		"proposal-written.md":  authored3Exists(authored3Folder + "proposal.md"),
	})
	authoredWantNoError(t, errs)
}

// S2: a path outside the change folder is rejected and the error names the
// change folder.
func TestAuthored3OutsideChangeFolderRejected(t *testing.T) {
	cases := []struct{ name, grader string }{
		{"regex target", authored3Target("testdata/other.md")},
		{"file_exists", authored3Exists("testdata/other.md")},
		{"parent of folder", authored3Exists("testdata/demo/openspec/changes/proposal.md")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			errs := authored3Validate(t, authored3Folder, "", map[string]string{"other.md": tc.grader})
			authoredWantError(t, errs, "is not an owned path", "the change folder "+authored3Folder, authoredCaseRel+"/graders/other.md")
		})
	}
}

// S3: with no Owned paths bullet, artefacts/ paths stay accepted beside the
// change folder.
func TestAuthored3ArtefactsAcceptedWithoutOwnedPaths(t *testing.T) {
	errs := authored3Validate(t, authored3Folder, "", map[string]string{
		"handoff-written.md": authored3Exists("artefacts/handoff.json"),
		"handoff-reports.md": authored3Target("artefacts/handoff.json"),
		"proposal.md":        authored3Exists(authored3Folder + "proposal.md"),
	})
	authoredWantNoError(t, errs)
}

// S4 (v2): a graded path with .. segments is not a clean relative path and is
// rejected with "must be a relative path", even when it would clean to a path
// inside the change folder; its clean form is judged by the owned-path rule.
func TestAuthored3DotDotEscapeRejected(t *testing.T) {
	unclean := []string{
		authored3Folder + "../escape.md",
		authored3Folder + "specs/../../escape.md",
		authored3Folder + "specs/../proposal.md",
	}
	for _, p := range unclean {
		t.Run(p, func(t *testing.T) {
			errs := authored3Validate(t, authored3Folder, "", map[string]string{"escape.md": authored3Exists(p)})
			authored3WantUnclean(t, errs, "escape.md")
		})
	}
	t.Run("clean form of the first", func(t *testing.T) {
		errs := authored3Validate(t, authored3Folder, "", map[string]string{
			"escape.md": authored3Exists("testdata/demo/openspec/changes/escape.md"),
		})
		authoredWantError(t, errs, "is not an owned path", "the change folder "+authored3Folder, authoredCaseRel+"/graders/escape.md")
		authoredWantNoErrorContaining(t, errs, "must be a relative path")
	})
}

// Clean-path rule (v2 amendment): every other non-clean form is rejected with
// "must be a relative path", for each way a grader names a file. The brief has
// no Owned paths bullet, so the artefacts/ default would otherwise accept the
// path.
func TestAuthored3GradedPathMustBeClean(t *testing.T) {
	paths := []string{
		"/tmp/artefacts/x.md",
		`artefacts\x.md`,
		"./artefacts/x.md",
		"artefacts/./x.md",
		"artefacts//x.md",
		"artefacts/x.md/",
		"artefacts/../artefacts/x.md",
		".",
		"../artefacts/x.md",
	}
	kinds := []struct {
		name string
		mk   func(string) string
	}{
		{"file_exists", authored3Exists},
		{"target", authored3Target},
		{"focus", authored3Focus},
	}
	for _, p := range paths {
		for _, k := range kinds {
			t.Run(k.name+" "+p, func(t *testing.T) {
				errs := authored3Validate(t, "", "", map[string]string{"unclean.md": k.mk(p)})
				authored3WantUnclean(t, errs, "unclean.md")
			})
		}
	}
	t.Run("clean control accepted", func(t *testing.T) {
		errs := authored3Validate(t, "", "", map[string]string{
			"a.md": authored3Exists("artefacts/x.md"),
			"b.md": authored3Target("artefacts/x.md"),
			"c.md": authored3Focus("artefacts/x.md"),
		})
		authoredWantNoError(t, errs)
	})
}

// S5: a sibling folder sharing the change folder's prefix is rejected, as is
// the folder itself (the path must lie strictly inside).
func TestAuthored3SiblingPrefixRejected(t *testing.T) {
	t.Run("sibling", func(t *testing.T) {
		errs := authored3Validate(t, "a/b/", "", map[string]string{"sibling.md": authored3Exists("a/bc/file.md")})
		authoredWantError(t, errs, "is not an owned path", "the change folder a/b/", authoredCaseRel+"/graders/sibling.md")
	})
	t.Run("folder itself", func(t *testing.T) {
		errs := authored3Validate(t, "a/b/", "", map[string]string{"self.md": authored3Exists("a/b")})
		authoredWantError(t, errs, "is not an owned path", authoredCaseRel+"/graders/self.md")
	})
	t.Run("inside accepted", func(t *testing.T) {
		errs := authored3Validate(t, "a/b/", "", map[string]string{"inside.md": authored3Exists("a/b/file.md")})
		authoredWantNoError(t, errs)
	})
}

// S6: with both an Owned paths bullet and a change folder, the folder and the
// owned file are accepted and artefacts/ is no longer accepted by default.
func TestAuthored3OwnedPathsAndChangeFolder(t *testing.T) {
	const owned = "artefacts/handoff.json"
	t.Run("folder and owned accepted", func(t *testing.T) {
		errs := authored3Validate(t, authored3Folder, owned, map[string]string{
			"proposal.md": authored3Target(authored3Folder + "proposal.md"),
			"tasks.md":    authored3Exists(authored3Folder + "tasks.md"),
			"handoff.md":  authored3Exists(owned),
			"reports.md":  authored3Target(owned),
		})
		authoredWantNoError(t, errs)
	})
	t.Run("other artefact rejected", func(t *testing.T) {
		errs := authored3Validate(t, authored3Folder, owned, map[string]string{
			"x.md": authored3Exists("artefacts/x.md"),
		})
		authoredWantError(t, errs, "is not an owned path", "the change folder "+authored3Folder, authoredCaseRel+"/graders/x.md")
	})
}

// S7: a change folder without its trailing slash behaves like S1 and S5.
func TestAuthored3ChangeFolderWithoutSlash(t *testing.T) {
	folder := strings.TrimSuffix(authored3Folder, "/")
	t.Run("S1 inside accepted", func(t *testing.T) {
		errs := authored3Validate(t, folder, "", map[string]string{
			"proposal.md": authored3Target(folder + "/proposal.md"),
			"spec.md":     authored3Exists(folder + "/specs/x/spec.md"),
		})
		authoredWantNoError(t, errs)
	})
	t.Run("outside rejected naming cleaned folder", func(t *testing.T) {
		errs := authored3Validate(t, folder, "", map[string]string{"other.md": authored3Exists("testdata/other.md")})
		authoredWantError(t, errs, "is not an owned path", "the change folder "+folder+"/", authoredCaseRel+"/graders/other.md")
	})
	t.Run("S5 sibling rejected", func(t *testing.T) {
		errs := authored3Validate(t, "a/b", "", map[string]string{"sibling.md": authored3Exists("a/bc/file.md")})
		authoredWantError(t, errs, "is not an owned path", "the change folder a/b/", authoredCaseRel+"/graders/sibling.md")
	})
	t.Run("S5 inside accepted", func(t *testing.T) {
		errs := authored3Validate(t, "a/b", "", map[string]string{"inside.md": authored3Exists("a/b/file.md")})
		authoredWantNoError(t, errs)
	})
}

// S8 (v2): a change folder that cleans to an absolute path, to . or .., or to
// a path starting ../ is ignored, as if absent. Absolute and ../ graded paths
// are rejected as not relative whatever the folder.
func TestAuthored3BadChangeFolderIgnored(t *testing.T) {
	for _, folder := range []string{"/tmp/x/", "../x/", "..", "a/../../x/"} {
		t.Run(folder+" other path names no change folder", func(t *testing.T) {
			errs := authored3Validate(t, folder, "", map[string]string{"other.md": authored3Exists("testdata/other.md")})
			authoredWantError(t, errs, "is not an owned path", authoredCaseRel+"/graders/other.md")
			authoredWantNoErrorContaining(t, errs, "the change folder")
		})
		t.Run(folder+" artefacts still accepted", func(t *testing.T) {
			errs := authored3Validate(t, folder, "", map[string]string{"art.md": authored3Exists("artefacts/x.md")})
			authoredWantNoError(t, errs)
		})
	}
	// a/../x/ cleans to x, a valid folder: the cleaned folder is what counts.
	t.Run("a/../x/ cleans to a valid folder", func(t *testing.T) {
		errs := authored3Validate(t, "a/../x/", "", map[string]string{"in.md": authored3Exists("x/file.md")})
		authoredWantNoError(t, errs)
		errs = authored3Validate(t, "a/../x/", "", map[string]string{"other.md": authored3Exists("testdata/other.md")})
		authoredWantError(t, errs, "is not an owned path", "the change folder x/", authoredCaseRel+"/graders/other.md")
	})
	folders := []string{"/tmp/x/", "../x/", "..", "a/../../x/", authored3Folder, ""}
	for _, folder := range folders {
		for _, p := range []string{"/tmp/x/file.md", "../x/file.md", "../file.md"} {
			t.Run("folder "+folder+" path "+p, func(t *testing.T) {
				errs := authored3Validate(t, folder, "", map[string]string{"under.md": authored3Exists(p)})
				authored3WantUnclean(t, errs, "under.md")
			})
		}
	}
}

// S9 (v2): an llm grader's focus file path follows the same rules as target.
func TestAuthored3FocusPaths(t *testing.T) {
	t.Run("inside folder accepted", func(t *testing.T) {
		errs := authored3Validate(t, authored3Folder, "", map[string]string{
			"proposal-quality.md": authored3Focus(authored3Folder + "proposal.md"),
		})
		authoredWantNoError(t, errs)
	})
	t.Run("outside folder rejected", func(t *testing.T) {
		errs := authored3Validate(t, authored3Folder, "", map[string]string{
			"other-quality.md": authored3Focus("testdata/other.md"),
		})
		authoredWantError(t, errs, "is not an owned path", "the change folder "+authored3Folder, authoredCaseRel+"/graders/other-quality.md")
	})
}

// S10 (v2): only the first Change folder bullet, and only its first backticked
// path, counts.
func TestAuthored3FirstChangeFolderWins(t *testing.T) {
	cases := []struct {
		name    string
		bullets []string
	}{
		{"two bullets", []string{"- Change folder: ^a/^.", "- Change folder: ^b/^."}},
		{"two paths in one bullet", []string{"- Change folder: ^a/^ (not ^b/^)."}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := authored3BodyLines("", tc.bullets...)
			errs := authored3ValidateBody(t, body, map[string]string{"a.md": authored3Exists("a/file.md")})
			authoredWantNoError(t, errs)
			errs = authored3ValidateBody(t, body, map[string]string{"b.md": authored3Exists("b/file.md")})
			authoredWantError(t, errs, "is not an owned path", "the change folder a/", authoredCaseRel+"/graders/b.md")
		})
	}
}

// N1-N8: nested counted repeats past RE2's 1000 limit.
func TestAuthored3NestedRepeatsCompile(t *testing.T) {
	t.Run("N1", func(t *testing.T) {
		re, err := compileJS(`(?:a{50}){50}`, "")
		if err != nil {
			t.Fatalf("compileJS: %v", err)
		}
		if !re.approximate {
			t.Fatalf("approximate = false, want true")
		}
		if !re.MatchString(strings.Repeat("a", 2500)) {
			t.Fatalf("does not match 2500 a")
		}
	})
	t.Run("N2", func(t *testing.T) {
		re, err := compileJS(`(?:a{2,3}){0,600}`, "")
		if err != nil {
			t.Fatalf("compileJS: %v", err)
		}
		if !re.approximate {
			t.Fatalf("approximate = false, want true")
		}
	})
	t.Run("N3", func(t *testing.T) {
		re, err := compileJS(`^(?:a{10}){10}$`, "")
		if err != nil {
			t.Fatalf("compileJS: %v", err)
		}
		if re.approximate {
			t.Fatalf("approximate = true, want false")
		}
		if !re.MatchString(strings.Repeat("a", 100)) {
			t.Fatalf("does not match 100 a")
		}
		for _, n := range []int{99, 101} {
			if re.MatchString(strings.Repeat("a", n)) {
				t.Fatalf("matches %d a", n)
			}
		}
	})
	t.Run("N4", func(t *testing.T) {
		pattern, flags := authored3GraderPattern(t, filepath.Join(authoredRealRoot(t), "evals", "spec-writer", "bookmark-export-brief", "graders", "requirement-length.md"))
		const want = `^### Requirement:[^\n]*\n\s*(?=\S)(?:(?!#{2,4} )[\s\S]){500}(?:(?!#{2,4} )[\s\S])*?(?!#{2,4} )\S`
		if pattern != want || flags != "m" {
			t.Fatalf("grader pattern/flags changed: %q / %q", pattern, flags)
		}
		re, err := compileJS(pattern, flags)
		if err != nil {
			t.Fatalf("compileJS: %v", err)
		}
		if !re.approximate {
			t.Fatalf("approximate = false, want true")
		}
	})
	invalid := []struct{ req, pattern, flags string }{
		{"N5", `(?:a{5,2}){500}`, ""},
		{"N6", `(?:a{2}){500}(`, ""},
		{"N7", `(?:a{2}){500}{`, "u"},
		{"N8", `(?:a{2}){500})`, ""},
	}
	for _, tc := range invalid {
		t.Run(tc.req, func(t *testing.T) {
			_, err := compileJS(tc.pattern, tc.flags)
			if err == nil || !strings.Contains(err.Error(), "does not compile") {
				t.Fatalf("compileJS(%q, %q) error = %v, want one containing %q", tc.pattern, tc.flags, err, "does not compile")
			}
		})
	}
}

// authored3GraderPattern reads the single-quoted pattern and the flags from a
// grader's frontmatter.
func authored3GraderPattern(t *testing.T, file string) (string, string) {
	t.Helper()
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	var pattern, flags string
	for _, line := range strings.Split(string(data), "\n") {
		if v, ok := strings.CutPrefix(line, "pattern: '"); ok {
			pattern = strings.ReplaceAll(strings.TrimSuffix(v, "'"), "''", "'")
		}
		if v, ok := strings.CutPrefix(line, "flags: "); ok {
			flags = strings.TrimSpace(v)
		}
	}
	if pattern == "" {
		t.Fatalf("no pattern in %s", file)
	}
	return pattern, flags
}
