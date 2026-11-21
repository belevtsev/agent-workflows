package repocheck

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/belevtsev/kitsu/v3/internal/kitsutest"
)

// usageSkill is the skill that teaches an agent to work with kitsu.
const usageSkill = "skills/kitsu"

// A skillFixture is the small repository an example of the skill starts
// from. Its files are committed first. hooks says that `kitsu init` ran after
// that commit, verified that `kitsu run` ran too, and clone that the example
// works in a new clone of the repository, where nothing of kitsu is installed.
type skillFixture struct {
	files                  map[string]string
	hooks, verified, clone bool
}

const (
	shopTotal = `# total prints the sum of two prices.
total() {
  echo $(( $1 + $2 ))
}
`
	shopTests = `. src/total.sh
check() {
  [ "$2" = "$3" ] || { echo "$1: want $2, got $3"; exit 1; }
}
check "two prices" 5 "$(total 2 3)"
echo "tests passed"
`
	shopChecks = `version = 1

[[check]]
id = "test"
run = ["sh", "tests/run.sh"]
paths = ["src/**", "tests/**"]

[[check]]
id = "lint"
run = ["sh", "-n", "src/total.sh"]
paths = ["src/**"]
`
	serviceGreet = `greet() {
  echo "hello $1"
}
`
	serviceCheck = `. app/greet.sh
[ "$(greet ann)" = "hello ann" ] || { echo "greet ann: got $(greet ann)"; exit 1; }
echo "1 test, 1 passed" > report.txt
echo "unit tests passed"
`
	serviceChecks = `version = 1

[[check]]
id = "unit"
run = ["sh", "check.sh"]

[[check]]
id = "lint"
run = ["service-lint", "app"]
`
	plainTests = `. app/greet.sh
[ "$(greet ann)" = "hello ann" ] || { echo "greet ann: got $(greet ann)"; exit 1; }
echo "greet tests passed"
`
	discountProposal = `# Add a discount

## Why

A buyer of ten items pays the full price of each.

## What changes

- An order of ten items or more costs a tenth less.
`
)

var shopFiles = map[string]string{"src/total.sh": shopTotal, "tests/run.sh": shopTests, "kitsu.toml": shopChecks}

var skillFixtures = map[string]skillFixture{
	"shop":  {files: shopFiles, hooks: true, verified: true},
	"clone": {files: shopFiles, clone: true},
	"task": {files: map[string]string{
		"src/total.sh": shopTotal, "tests/run.sh": shopTests, "kitsu.toml": shopChecks,
		"openspec/changes/add-discount/proposal.md": discountProposal,
	}, hooks: true, verified: true},
	"service": {files: map[string]string{"app/greet.sh": serviceGreet, "check.sh": serviceCheck, "kitsu.toml": serviceChecks}, hooks: true},
	"plain":   {files: map[string]string{"app/greet.sh": serviceGreet, "tests/greet_test.sh": plainTests}},
}

// skillDocuments returns SKILL.md of the usage skill and its examples, by
// path from the root of the repository.
func skillDocuments(t *testing.T) map[string]string {
	t.Helper()
	documents := map[string]string{}
	paths, err := filepath.Glob(filepath.Join(repoRoot, filepath.FromSlash(usageSkill), "examples", "*.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range append(paths, filepath.Join(repoRoot, filepath.FromSlash(usageSkill), "SKILL.md")) {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		relative, err := filepath.Rel(repoRoot, path)
		if err != nil {
			t.Fatal(err)
		}
		documents[filepath.ToSlash(relative)] = strings.ReplaceAll(string(data), "\r\n", "\n")
	}
	return documents
}

// A skillBlock is one fenced block of a document: its language and lines.
type skillBlock struct {
	language string
	lines    []string
}

func fencedBlocks(text string) []skillBlock {
	var blocks []skillBlock
	inside := false
	for line := range strings.SplitSeq(text, "\n") {
		switch {
		case !inside && strings.HasPrefix(line, "```"):
			inside = true
			blocks = append(blocks, skillBlock{language: strings.TrimPrefix(line, "```")})
		case inside && line == "```":
			inside = false
		case inside:
			last := &blocks[len(blocks)-1]
			last.lines = append(last.lines, line)
		}
	}
	return blocks
}

var (
	skillName   = regexp.MustCompile(`^[a-z0-9-]{1,64}$`)
	headKey     = regexp.MustCompile(`^([A-Za-z_-]+):`)
	namedInCode = regexp.MustCompile("`kitsu ([a-z]+)")
	helpCommand = regexp.MustCompile(`(?m)^  (?:kitsu )?([a-z]+)\b`)
	fixtureNote = regexp.MustCompile(`fixture "([a-z]+)"`)
	exitNote    = regexp.MustCompile(`^\[exit (\d+)\]$`)
	runID       = regexp.MustCompile(`\d{8}T\d{6}Z-[0-9a-f]{8}`)
	contentID   = regexp.MustCompile(`\b[0-9a-f]{12}\b`)
	duration    = regexp.MustCompile(`passed in \S+`)
	// A version number is three numbers with dots, as in 3.0.0.
	versionNumber = regexp.MustCompile(`\b\d+\.\d+\.\d+\b`)
)

// Every skill has a head that both hosts and the Claude apps accept: a name
// that is the name of its folder, a description, and nothing else but
// metadata. The whole head stays far below the limit of a host's listing.
func TestSkillHeads(t *testing.T) {
	paths, err := filepath.Glob(filepath.Join(repoRoot, "skills", "*", "SKILL.md"))
	opted, _ := filepath.Glob(filepath.Join(repoRoot, "plugins", "*", "skills", "*", "SKILL.md"))
	if paths = append(paths, opted...); err != nil || len(paths) < 3 {
		t.Fatalf("want the skills task and kitsu under skills/ and managed in the opt-in plugin, got %v (%v)", paths, err)
	}
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		head, _, found := strings.Cut(strings.TrimPrefix(strings.ReplaceAll(string(data), "\r\n", "\n"), "---\n"), "\n---\n")
		if !found {
			t.Errorf("%s has no head between two lines of ---", path)
			continue
		}
		values := map[string]string{}
		for line := range strings.SplitSeq(head, "\n") {
			if match := headKey.FindStringSubmatch(line); match != nil {
				values[match[1]] = strings.TrimSpace(strings.TrimPrefix(line, match[0]))
			}
		}
		for key, value := range values {
			if !slices.Contains([]string{"name", "description", "metadata"}, key) {
				t.Errorf("%s: the head holds `%s`; only name, description and metadata work on every host", path, key)
			}
			// The head is YAML. A text that holds `: ` or ` #` is a text only
			// inside quotes; without them a strict reader refuses the head.
			quoted := len(value) > 1 && value[0] == '\'' && value[len(value)-1] == '\'' && !strings.Contains(value[1:len(value)-1], "'")
			if !quoted && (strings.Contains(value, ": ") || strings.Contains(value, " #") || strings.HasPrefix(value, "'") || strings.HasPrefix(value, `"`)) {
				t.Errorf("%s: the value of `%s` is not one YAML text; put it in single quotes, with no single quote inside", path, key)
			}
		}
		folder := filepath.Base(filepath.Dir(path))
		if name := values["name"]; name != folder || !skillName.MatchString(name) || strings.Contains(name, "claude") {
			t.Errorf("%s: the name %q must be the folder name %q, in lower-case letters, digits and hyphens", path, name, folder)
		}
		description := strings.Trim(values["description"], "'")
		if len(description) < 80 || len(description) > 700 || strings.ContainsAny(description, "<>") {
			t.Errorf("%s: the description has %d characters; want 80 to 700 and no angle bracket", path, len(description))
		}
		if !strings.Contains(description, "Use ") {
			t.Errorf("%s: the description does not say when to use the skill", path)
		}
	}
}

// The skill is read whole each time it loads, and an example when its case
// comes up, so both stay short. Every example is named in SKILL.md with the
// case it is for. The skill names no version of kitsu: the replay below, not a
// number, keeps it in step with the program.
func TestSkillSizeAndExamples(t *testing.T) {
	documents := skillDocuments(t)
	skill := documents[usageSkill+"/SKILL.md"]
	if lines := strings.Count(skill, "\n"); lines > 200 {
		t.Errorf("SKILL.md has %d lines; want at most 200", lines)
	}
	examples := 0
	for path, text := range documents {
		if path == usageSkill+"/SKILL.md" {
			continue
		}
		examples++
		if lines := strings.Count(text, "\n"); lines > 100 {
			t.Errorf("%s has %d lines; want at most 100", path, lines)
		}
		if link := "](examples/" + filepath.Base(path) + ")"; !strings.Contains(skill, link) {
			t.Errorf("SKILL.md does not link %s", path)
		}
	}
	if examples < 5 {
		t.Errorf("the skill has %d examples; want at least 5", examples)
	}
	for path, text := range documents {
		if number := versionNumber.FindString(text); number != "" {
			t.Errorf("%s holds the version number %s; the skill names no version of kitsu", path, number)
		}
	}
}

// skillEnvironment is a new home with the kitsu under test first on the PATH.
func skillEnvironment(t *testing.T) []string {
	t.Helper()
	home := t.TempDir()
	return append(os.Environ(),
		"HOME="+home, "XDG_CONFIG_HOME="+filepath.Join(home, ".config"),
		"GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+filepath.Join(home, ".gitconfig"),
		"GIT_AUTHOR_NAME=Example", "GIT_AUTHOR_EMAIL=example@example.com",
		"GIT_COMMITTER_NAME=Example", "GIT_COMMITTER_EMAIL=example@example.com",
		"PATH="+kitsutest.Binary(t)+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// shell runs one command line in dir and returns what it printed and its
// exit status.
func shell(t *testing.T, dir string, env []string, line, input string) (string, int) {
	t.Helper()
	command := exec.CommandContext(t.Context(), "sh", "-c", line)
	command.Dir, command.Env, command.Stdin = dir, env, strings.NewReader(input)
	output, err := command.CombinedOutput()
	if exit, failed := err.(*exec.ExitError); failed {
		return string(output), exit.ExitCode()
	} else if err != nil {
		t.Fatalf("`%s` could not be started: %v", line, err)
	}
	return string(output), 0
}

// prepare makes the repository of a fixture and returns its directory.
func (f skillFixture) prepare(t *testing.T, env []string) string {
	t.Helper()
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(base, "origin")
	for name, text := range f.files {
		writeFixture(t, filepath.Join(dir, filepath.FromSlash(name)), []byte(text))
	}
	steps := []string{"git init -q -b main", "git add -A", "git commit -q -m start"}
	if f.hooks {
		steps = append(steps, "kitsu init")
	}
	if f.verified {
		steps = append(steps, "kitsu run")
	}
	for _, step := range steps {
		if output, status := shell(t, dir, env, step, ""); status != 0 {
			t.Fatalf("the fixture step `%s` exited %d:\n%s", step, status, output)
		}
	}
	if !f.clone {
		return dir
	}
	if output, status := shell(t, base, env, "git clone -q origin work", ""); status != 0 {
		t.Fatalf("the fixture cannot be cloned:\n%s", output)
	}
	return filepath.Join(base, "work")
}

// printed is what a command printed, with the parts that differ from run to
// run replaced by the names the examples use for them.
func printed(output, dir string) string {
	output = strings.ReplaceAll(output, dir, "<repo>")
	output = runID.ReplaceAllString(output, "<run>")
	output = contentID.ReplaceAllString(output, "<id>")
	output = duration.ReplaceAllString(output, "passed in 0s")
	return strings.TrimRight(output, "\n")
}

// replay executes the blocks of one example in order: a `diff` block is
// applied as the edit it shows, and each command of a `console` block must
// print what the block shows and exit with the status it shows.
func replay(t *testing.T, text string, env []string) {
	t.Helper()
	match := fixtureNote.FindStringSubmatch(text)
	if match == nil {
		t.Fatal(`the example names no fixture "<name>"`)
	}
	fixture, known := skillFixtures[match[1]]
	if !known {
		t.Fatalf("the example names the unknown fixture %q", match[1])
	}
	dir := fixture.prepare(t, env)
	for _, block := range fencedBlocks(text) {
		switch block.language {
		case "diff":
			patch := strings.Join(block.lines, "\n") + "\n"
			if output, status := shell(t, dir, env, "git apply -", patch); status != 0 {
				t.Fatalf("the edit cannot be applied:\n%s\n%s", patch, output)
			}
		case "console":
			for start := 0; start < len(block.lines); {
				end := start + 1
				for end < len(block.lines) && !strings.HasPrefix(block.lines[end], "$ ") {
					end++
				}
				line, found := strings.CutPrefix(block.lines[start], "$ ")
				if !found {
					t.Fatalf("a console block must start with a command, not with %q", block.lines[start])
				}
				want, wantStatus := block.lines[start+1:end], "0"
				if n := len(want); n > 0 && exitNote.MatchString(want[n-1]) {
					want, wantStatus = want[:n-1], exitNote.FindStringSubmatch(want[n-1])[1]
				}
				output, status := shell(t, dir, env, line, "")
				got := printed(output, dir)
				if got != strings.Join(want, "\n") || wantStatus != strconv.Itoa(status) {
					t.Errorf("`%s` exited %d and printed:\n%s\n\nthe example shows exit %s and:\n%s", line, status, got, wantStatus, strings.Join(want, "\n"))
				}
				start = end
			}
		}
	}
}

// Each example of the skill is replayed as it is written, against the kitsu
// under test, so an example cannot show output the program does not print.
func TestSkillExamplesAreReplayed(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the examples use sh, which Windows does not always have")
	}
	for path, text := range skillDocuments(t) {
		if path == usageSkill+"/SKILL.md" {
			continue
		}
		t.Run(filepath.Base(path), func(t *testing.T) {
			t.Parallel()
			replay(t, text, skillEnvironment(t))
		})
	}
}

// The skill names only commands that `kitsu help --all` lists, and each piece of
// output it explains is printed in an example that the test replays.
func TestSkillNamesRealCommandsAndOutput(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the kitsu under test is asked for its help through sh")
	}
	help, status := shell(t, t.TempDir(), skillEnvironment(t), "kitsu help --all", "")
	if status != 0 {
		t.Fatalf("`kitsu help --all` exited %d:\n%s", status, help)
	}
	commands := map[string]bool{"help": true}
	for _, match := range helpCommand.FindAllStringSubmatch(help, -1) {
		commands[match[1]] = true
	}
	documents := skillDocuments(t)
	var replayed strings.Builder
	for path, text := range documents {
		for _, match := range namedInCode.FindAllStringSubmatch(text, -1) {
			if !commands[match[1]] {
				t.Errorf("%s names `kitsu %s`, which `kitsu help --all` does not list", path, match[1])
			}
		}
		for _, block := range fencedBlocks(text) {
			if block.language != "console" {
				continue
			}
			for _, line := range block.lines {
				if command, found := strings.CutPrefix(line, "$ kitsu "); found && !commands[strings.Fields(command)[0]] {
					t.Errorf("%s runs `kitsu %s`, which `kitsu help --all` does not list", path, command)
				}
				replayed.WriteString(line + "\n")
			}
		}
	}
	skill := documents[usageSkill+"/SKILL.md"]
	for _, phrase := range []string{
		"this commit is denied", "not run on this content", "satisfied, not run again",
		"failed with exit status", "source_changed", "could not be started",
		"the tree has no declaration", "are not installed in this clone",
	} {
		if !strings.Contains(skill, phrase) {
			t.Errorf("SKILL.md does not explain the output %q", phrase)
		}
		if !strings.Contains(replayed.String(), phrase) {
			t.Errorf("no replayed example prints %q, which SKILL.md explains", phrase)
		}
	}
}
