// Package roles_test holds the scenarios of the specification
// role-definitions
// (openspec/changes/add-role-workflow/specs/role-definitions/spec.md) that
// are not marked "(evaluation)".
package roles_test

import (
	"bufio"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/belevtsev/agent-workflows/v3/internal/cwtest"
)

// roleIDs are the seven roles of the requirement "Role set", in its order.
var roleIDs = []string{"lookup", "spec-writer", "architect", "quality-engineer", "developer", "reviewer", "git"}

// rolesTOML is the cw-roles.toml of the shared fixture.
const rolesTOML = "version = 1\n\n[paths]\ntests = [\"tests/**\"]\n"

// demo is the shared fixture of role-definitions: the repository demo of
// check-evidence with tests/app_test.txt, cw-roles.toml and the change
// folder openspec/changes/add-greeting/, all in the first commit's history.
func demo(t *testing.T) *cwtest.Repo {
	t.Helper()
	repo := cwtest.Demo(t)
	repo.Write("tests/app_test.txt", "app test\n")
	repo.Write("cw-roles.toml", rolesTOML)
	repo.Write("openspec/changes/add-greeting/proposal.md", "# add-greeting\n")
	repo.Git("add", "-A")
	repo.Git("commit", "-q", "-m", "fixture of role-definitions")
	return repo
}

// tree is the value of tree that cw status --json prints: T0 when it is
// taken before the edits of a scenario.
func tree(t *testing.T, repo *cwtest.Repo) string {
	t.Helper()
	result := repo.CW("status", "--json")
	var out struct {
		Tree string `json:"tree"`
	}
	if err := json.Unmarshal([]byte(result.Stdout), &out); err != nil {
		t.Fatalf("cw status --json (exit %d) did not print one JSON object: %v\n%s", result.Code, err, result.Output())
	}
	if out.Tree == "" {
		t.Fatalf("cw status --json printed no tree:\n%s", result.Output())
	}
	return out.Tree
}

// hostModel is the model and effort of a role on one host.
type hostModel struct {
	Model  string `json:"model"`
	Effort string `json:"effort"`
}

// role is one entry of roles in the output of cw roles --json.
type role struct {
	ID        string               `json:"id"`
	Job       string               `json:"job"`
	Writes    string               `json:"writes"`
	TextChars *int                 `json:"text_chars"`
	Models    map[string]hostModel `json:"models"`
}

// rolesJSON runs cw roles --json, requires exit 0 and returns the roles by
// id.
func rolesJSON(t *testing.T, repo *cwtest.Repo) map[string]role {
	t.Helper()
	result := repo.CW("roles", "--json")
	if result.Code != 0 {
		t.Fatalf("cw roles --json exited %d, want 0:\n%s", result.Code, result.Output())
	}
	var out struct {
		Roles []role `json:"roles"`
	}
	if err := json.Unmarshal([]byte(result.Stdout), &out); err != nil {
		t.Fatalf("cw roles --json did not print one JSON object: %v\n%s", err, result.Output())
	}
	byID := map[string]role{}
	for _, r := range out.Roles {
		byID[r.ID] = r
	}
	return byID
}

// lineWith says whether one line of text holds every one of the words.
func lineWith(text string, words ...string) bool {
	for _, line := range strings.Split(text, "\n") {
		all := true
		for _, word := range words {
			if !strings.Contains(line, word) {
				all = false
				break
			}
		}
		if all {
			return true
		}
	}
	return false
}

// namesWord says whether text names a word on its own, such as a path or a
// role id.
func namesWord(text, word string) bool {
	return regexp.MustCompile(`(^|[^A-Za-z0-9_./-])` + regexp.QuoteMeta(word) + `($|[^A-Za-z0-9_./-])`).MatchString(text)
}

// moduleRoot is the root of this module: two levels up from scenarios/roles.
func moduleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Dir(filepath.Dir(dir))
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("no go.mod at %s: %v", root, err)
	}
	return root
}

// readModuleFile reads a file of the module by its slash path.
func readModuleFile(t *testing.T, rel string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(moduleRoot(t), filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// frontMatter is the YAML front matter of a Markdown file as its keys and
// raw values; it reads only the flat `key: value` lines the role files use.
func frontMatter(t *testing.T, rel string) map[string]string {
	t.Helper()
	scanner := bufio.NewScanner(strings.NewReader(readModuleFile(t, rel)))
	if !scanner.Scan() || strings.TrimSpace(scanner.Text()) != "---" {
		t.Fatalf("%s does not start with front matter", rel)
	}
	fields := map[string]string{}
	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) == "---" {
			return fields
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok || strings.HasPrefix(line, " ") {
			continue
		}
		fields[strings.TrimSpace(key)] = strings.TrimSpace(value)
	}
	t.Fatalf("the front matter of %s does not end", rel)
	return nil
}

// toolList is the list of tool names of a front matter value, written as
// `A, B` or `[A, B]`. Each name loses a specifier such as `Bash(git *)`.
func toolList(value string) []string {
	value = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(value), "["), "]"))
	var out []string
	for _, item := range strings.Split(value, ",") {
		item = strings.Trim(strings.TrimSpace(item), `"'`)
		if name, _, ok := strings.Cut(item, "("); ok {
			item = name
		}
		if item != "" {
			out = append(out, item)
		}
	}
	return out
}

// contains says whether a list holds a name.
func contains(list []string, name string) bool {
	for _, item := range list {
		if item == name {
			return true
		}
	}
	return false
}

// copyModule copies the module root into a new temporary directory, without
// .git, .claude and scenarios, and returns the copy's root.
func copyModule(t *testing.T) string {
	t.Helper()
	src := moduleRoot(t)
	dst := filepath.Join(t.TempDir(), "module")
	skip := map[string]bool{".git": true, ".claude": true, "scenarios": true}
	err := filepath.WalkDir(src, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if top, _, _ := strings.Cut(filepath.ToSlash(rel), "/"); skip[top] {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		target := filepath.Join(dst, rel)
		info, err := entry.Info()
		if err != nil {
			return err
		}
		switch {
		case entry.IsDir():
			return os.MkdirAll(target, 0o755)
		case info.Mode()&fs.ModeSymlink != 0:
			link, err := os.Readlink(path)
			if err != nil {
				return err
			}
			return os.Symlink(link, target)
		case info.Mode().IsRegular():
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			return os.WriteFile(target, data, info.Mode().Perm())
		}
		return nil
	})
	if err != nil {
		t.Fatalf("copying the module: %v", err)
	}
	return dst
}

// goTestRoles runs `go test ./internal/roles` in a copy of the module, with
// no network and no change to go.mod, and returns its exit status and what
// it printed.
func goTestRoles(t *testing.T, root string) (int, string) {
	t.Helper()
	command := exec.Command("go", "test", "-count=1", "./internal/roles")
	command.Dir = root
	var env []string
	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		switch name {
		case "GOFLAGS", "GOPROXY", "GOWORK", "GOTOOLCHAIN":
			continue
		}
		env = append(env, kv)
	}
	command.Env = append(env, "GOFLAGS=-mod=readonly", "GOPROXY=off", "GOWORK=off", "GOTOOLCHAIN=local")
	output, err := command.CombinedOutput()
	if err == nil {
		return 0, string(output)
	}
	var exit *exec.ExitError
	if !errors.As(err, &exit) {
		t.Fatalf("go test ./internal/roles could not start: %v\n%s", err, output)
	}
	return exit.ExitCode(), string(output)
}
