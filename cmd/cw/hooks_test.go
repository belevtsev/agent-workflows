package main_test

import (
	"bytes"
	json "encoding/json/v2"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/belevtsev/agent-workflows/v3/internal/taskflow"
)

// Tests for the plugin hook file (R-JSON) and for running its inline commands
// against the built binary (R-E2E).

type hooksFile struct {
	Description string                  `json:"description"`
	Hooks       map[string][]hooksGroup `json:"hooks"`
}

type hooksGroup struct {
	Matcher string       `json:"matcher"`
	Hooks   []hooksEntry `json:"hooks"`
}

type hooksEntry struct {
	Type    string `json:"type"`
	If      string `json:"if"`
	Command string `json:"command"`
	Timeout int    `json:"timeout"`
}

// hooksRead reads ROOT/hooks/hooks.json and decodes it strictly.
func hooksRead(t *testing.T) ([]byte, hooksFile) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "hooks", "hooks.json"))
	if err != nil {
		t.Fatal(err)
	}
	var file hooksFile
	if err := json.Unmarshal(data, &file, json.RejectUnknownMembers(true)); err != nil {
		t.Fatalf("hooks.json does not match the agreed shape: %v", err)
	}
	return data, file
}

func TestHooksJSONShape(t *testing.T) {
	data, file := hooksRead(t)

	t.Run("valid JSON with only description and hooks", func(t *testing.T) {
		// R-JSON: valid JSON whose only top-level keys are description and hooks.
		var top map[string]any
		if err := json.Unmarshal(data, &top); err != nil {
			t.Fatalf("hooks.json is not valid JSON: %v", err)
		}
		keys := make([]string, 0, len(top))
		for key := range top {
			keys = append(keys, key)
		}
		slices.Sort(keys)
		if !slices.Equal(keys, []string{"description", "hooks"}) {
			t.Fatalf("top-level keys %v, want [description hooks]", keys)
		}
		if strings.TrimSpace(file.Description) == "" {
			t.Fatal("description is empty")
		}
	})

	t.Run("entries are bounded inline commands", func(t *testing.T) {
		// R-JSON: every entry is type command with a non-empty inline command
		// and a timeout between 1 and 60.
		if len(file.Hooks) == 0 {
			t.Fatal("no hook events")
		}
		for event, groups := range file.Hooks {
			if len(groups) == 0 {
				t.Errorf("%s has no matcher groups", event)
			}
			for _, group := range groups {
				if len(group.Hooks) == 0 {
					t.Errorf("%s matcher %q has no hooks", event, group.Matcher)
				}
				for _, entry := range group.Hooks {
					if entry.Type != "command" {
						t.Errorf("%s entry type %q, want command", event, entry.Type)
					}
					if strings.TrimSpace(entry.Command) == "" {
						t.Errorf("%s entry has an empty command", event)
					}
					if entry.Timeout < 1 || entry.Timeout > 60 {
						t.Errorf("%s entry timeout %d outside 1..60", event, entry.Timeout)
					}
					for _, forbidden := range []string{".sh", ".py", ".bash", ".zsh", "/scripts/", "/hooks/"} {
						if strings.Contains(entry.Command, forbidden) {
							t.Errorf("%s command %q references %q; commands must be inline", event, entry.Command, forbidden)
						}
					}
				}
			}
		}
	})

	t.Run("commands do not exec cw and pre-publish ends with exit 0", func(t *testing.T) {
		// R-JSON: no command string contains the word exec, and every
		// PreToolUse command ends with exit 0.
		execWord := regexp.MustCompile(`\bexec\b`)
		for event, groups := range file.Hooks {
			for _, group := range groups {
				for _, entry := range group.Hooks {
					if execWord.MatchString(entry.Command) {
						t.Errorf("%s command %q contains the word exec", event, entry.Command)
					}
					if event == "PreToolUse" && !strings.HasSuffix(strings.TrimSpace(entry.Command), "exit 0") {
						t.Errorf("PreToolUse command %q does not end with exit 0", entry.Command)
					}
				}
			}
		}
	})

	t.Run("events matchers and conditions", func(t *testing.T) {
		// R-JSON, E1: SessionStart matcher startup|resume|clear|compact;
		// PreToolUse has one Bash group with exactly two handlers, whose ifs
		// are Bash(*git*) and Bash(*abandon*) and whose commands are both
		// the agreed inline command, so both reach the same decision where
		// both run; SessionStart calls cw hook and references the plugin
		// root and data directory.
		events := make([]string, 0, len(file.Hooks))
		for event := range file.Hooks {
			events = append(events, event)
		}
		slices.Sort(events)
		if !slices.Equal(events, []string{"PreToolUse", "SessionStart"}) {
			t.Fatalf("hook events %v, want [PreToolUse SessionStart]", events)
		}
		session := file.Hooks["SessionStart"]
		if len(session) != 1 || session[0].Matcher != "startup|resume|clear|compact" || len(session[0].Hooks) != 1 {
			t.Fatalf("SessionStart groups %+v", session)
		}
		command := session[0].Hooks[0].Command
		for _, want := range []string{"${CLAUDE_PLUGIN_ROOT}/global/AGENTS.md", "${CLAUDE_PLUGIN_DATA}", "cw hook session-start"} {
			if !strings.Contains(command, want) {
				t.Errorf("SessionStart command %q does not contain %q", command, want)
			}
		}
		pre := file.Hooks["PreToolUse"]
		if len(pre) != 1 || pre[0].Matcher != "Bash" {
			t.Fatalf("PreToolUse groups %+v", pre)
		}
		ifs := make([]string, 0, len(pre[0].Hooks))
		for _, entry := range pre[0].Hooks {
			ifs = append(ifs, entry.If)
			if entry.Command != preToolUseCommand {
				t.Fatalf("PreToolUse command %q, want %q", entry.Command, preToolUseCommand)
			}
		}
		if !slices.Equal(ifs, []string{preToolUseIf, preToolUseAbandonIf}) {
			t.Fatalf("PreToolUse handler conditions %q, want %q", ifs, []string{preToolUseIf, preToolUseAbandonIf})
		}
	})
}

// The agreed PreToolUse handlers (E1): one runs the gate for git commands, the
// other for cw task abandon, with the same command.
const (
	preToolUseIf        = "Bash(*git*)"
	preToolUseAbandonIf = "Bash(*abandon*)"
	preToolUseCommand   = "command -v cw >/dev/null 2>&1 || exit 0; cw hook pre-publish; exit 0"
)

// hooksPreToolUse returns the command of the PreToolUse handler whose if is
// condition.
func hooksPreToolUse(t *testing.T, file hooksFile, condition string) string {
	t.Helper()
	for _, group := range file.Hooks["PreToolUse"] {
		for _, entry := range group.Hooks {
			if entry.If == condition {
				return entry.Command
			}
		}
	}
	t.Fatalf("no PreToolUse handler with if %q in %+v", condition, file.Hooks["PreToolUse"])
	return ""
}

// gateInactiveLine returns the line SessionStart appends when cw is not on
// PATH (E4): the gate is inactive, and how to install cw at the plugin's
// release tag ("v" + the .claude-plugin/plugin.json version) or from the
// release archive.
func gateInactiveLine(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", ".claude-plugin", "plugin.json"))
	if err != nil {
		t.Fatal(err)
	}
	var plugin struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(data, &plugin); err != nil || plugin.Version == "" {
		t.Fatalf(".claude-plugin/plugin.json has no version (%v)", err)
	}
	return "agent-workflows: the commit and push verify gate is inactive because cw was not found on PATH." +
		" Install cw with `go install github.com/belevtsev/agent-workflows/v3/cmd/cw@v" + plugin.Version + "` (Go 1.27.1 or newer)" +
		" or from the release archive: https://github.com/belevtsev/agent-workflows/blob/main/docs/operations.md#get-cw"
}

// hooksCanonicalTempDir returns a fresh temporary directory in canonical form.
func hooksCanonicalTempDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

// hooksSh runs command with sh -c, stdin and exactly env, and returns the
// exit code and output.
func hooksSh(t *testing.T, command string, stdin []byte, env []string) (int, string, string) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "sh", "-c", command)
	cmd.Env = env
	cmd.Dir = t.TempDir()
	cmd.Stdin = bytes.NewReader(stdin)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	code := 0
	if err != nil {
		var exit *exec.ExitError
		if !errors.As(err, &exit) {
			t.Fatalf("sh -c %q did not run: %v", command, err)
		}
		code = exit.ExitCode()
	}
	return code, stdout.String(), stderr.String()
}

// hooksStubCW writes an executable cw stub with script as its body into a
// fresh directory and returns a PATH that puts it before /usr/bin:/bin.
func hooksStubCW(t *testing.T, script string) string {
	t.Helper()
	dir := hooksCanonicalTempDir(t)
	if err := os.WriteFile(filepath.Join(dir, "cw"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	return dir + ":/usr/bin:/bin"
}

// hooksOldCW stands in for a cw older than the hook subcommand.
const hooksOldCW = "#!/bin/sh\necho \"agent-workflows: unknown command: hook\" >&2\nexit 2\n"

// hooksSilentCW prints nothing and succeeds.
const hooksSilentCW = "#!/bin/sh\nexit 0\n"

// hooksGit runs git in dir with HOME set to home and no global or system
// configuration.
func hooksGit(t *testing.T, home, dir string, args ...string) {
	t.Helper()
	command := exec.CommandContext(t.Context(), "git", append([]string{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@example.com", "-c", "init.defaultBranch=main"}, args...)...)
	command.Env = []string{"HOME=" + home, "PATH=/usr/bin:/bin", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1"}
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, output)
	}
}

// hooksLines returns n distinct newline-terminated lines.
func hooksLines(n int) string {
	var b strings.Builder
	for i := range n {
		b.WriteString("line ")
		b.WriteString(strconv.Itoa(i))
		b.WriteString("\n")
	}
	return b.String()
}

// hooksWriteFile writes content to dir/rel, creating parent directories.
func hooksWriteFile(t *testing.T, dir, rel, content string) {
	t.Helper()
	path := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}

// hooksDecision is the PreToolUse decision a hook prints.
type hooksDecision struct {
	HookSpecificOutput struct {
		HookEventName            string `json:"hookEventName"`
		PermissionDecision       string `json:"permissionDecision"`
		PermissionDecisionReason string `json:"permissionDecisionReason"`
	} `json:"hookSpecificOutput"`
}

// hooksExpectDecision requires one newline-terminated PreToolUse JSON line
// with the wanted permissionDecision and returns its reason.
func hooksExpectDecision(t *testing.T, stdout, want string) string {
	t.Helper()
	if !strings.HasSuffix(stdout, "\n") || strings.Count(stdout, "\n") != 1 {
		t.Fatalf("stdout %q is not one JSON line", stdout)
	}
	var decision hooksDecision
	if err := json.Unmarshal([]byte(stdout), &decision); err != nil {
		t.Fatalf("stdout %q is not the decision JSON: %v", stdout, err)
	}
	output := decision.HookSpecificOutput
	if output.HookEventName != "PreToolUse" || output.PermissionDecision != want {
		t.Fatalf("decision %+v, want a PreToolUse %s", output, want)
	}
	return output.PermissionDecisionReason
}

func TestHooksEndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("builds cmd/cw")
	}
	if runtime.GOOS == "windows" {
		t.Skip("hook commands run under sh")
	}
	_, file := hooksRead(t)
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	binary := buildPlainCW(t)
	withCW := filepath.Dir(binary) + ":/usr/bin:/bin"
	const withoutCW = "/usr/bin:/bin"
	for _, dir := range []string{"/usr/bin", "/bin"} {
		if _, err := os.Stat(filepath.Join(dir, "cw")); err == nil {
			t.Skipf("%s/cw exists; cannot run the hooks without cw on PATH", dir)
		}
	}

	home := hooksCanonicalTempDir(t)
	data := hooksCanonicalTempDir(t)
	store := filepath.Join(hooksCanonicalTempDir(t), "store")
	repo := hooksCanonicalTempDir(t)
	hooksGit(t, home, repo, "init", "-q")
	if err := os.WriteFile(filepath.Join(repo, "source.txt"), []byte("source\n"), 0600); err != nil {
		t.Fatal(err)
	}
	// A directory outside any repository, used as the hook's cwd for E2.
	outside := hooksCanonicalTempDir(t)

	env := func(path string, extra ...string) []string {
		return append([]string{
			"PATH=" + path,
			"HOME=" + home,
			"CW_TASK_STORE=" + store,
			"CLAUDE_PLUGIN_ROOT=" + root,
			"CLAUDE_PLUGIN_DATA=" + data,
		}, extra...)
	}

	preCommand := hooksPreToolUse(t, file, preToolUseIf)
	abandonCommand := hooksPreToolUse(t, file, preToolUseAbandonIf)
	// preStdin is a PreToolUse payload for a Bash command run in cwd.
	preStdin := func(t *testing.T, cwd, bash string) []byte {
		t.Helper()
		stdin, err := json.Marshal(map[string]any{
			"session_id":      "s",
			"hook_event_name": "PreToolUse",
			"tool_name":       "Bash",
			"cwd":             cwd,
			"tool_input":      map[string]any{"command": bash},
		})
		if err != nil {
			t.Fatal(err)
		}
		return stdin
	}
	// The publish commands every pre-publish case runs from the repository.
	publish := []string{"git commit -m x", "git push origin main"}

	expectAllow := func(t *testing.T, stdin []byte, environment []string) {
		t.Helper()
		code, stdout, stderr := hooksSh(t, preCommand, stdin, environment)
		if code != 0 || stdout != "" {
			t.Fatalf("exit %d stdout %q stderr %q; want exit 0 and no output", code, stdout, stderr)
		}
	}
	expectDecision := func(t *testing.T, stdin []byte, environment []string, want string) string {
		t.Helper()
		code, stdout, stderr := hooksSh(t, preCommand, stdin, environment)
		if code != 0 {
			t.Fatalf("exit %d stderr %q", code, stderr)
		}
		return hooksExpectDecision(t, stdout, want)
	}

	t.Run("pre-publish allows without a record", func(t *testing.T) {
		// R-E2E: no record and nothing staged, exit 0 and empty stdout.
		for _, bash := range publish {
			expectAllow(t, preStdin(t, repo, bash), env(withCW))
		}
	})

	plan := filepath.Join(hooksCanonicalTempDir(t), "plan.json")
	if err := os.WriteFile(plan, []byte(`{"title":"T","kind":"substantial","checkpoints":{"required":false,"reason":"test"},"requirements":[{"id":"source","description":"Inspect actual source","check_ids":["unit"],"allow_non_execution":true}],"checks":[{"id":"unit","argv":["go","test","./..."]}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	start := exec.CommandContext(t.Context(), binary, "task", "start", "--full", "--repo", repo, "--plan", plan,
		"--policy", filepath.Join(root, "skills", "task-orchestration", "model-policy.yaml"), "--summary", "--store", store)
	start.Env = []string{"HOME=" + home, "PATH=" + withoutCW}
	start.Dir = t.TempDir()
	if output, err := start.CombinedOutput(); err != nil {
		t.Fatalf("cw task start: %v\n%s", err, output)
	}
	entries, err := os.ReadDir(store)
	if err != nil || len(entries) != 1 || !entries[0].IsDir() {
		t.Fatalf("store %s holds %v (%v), want one task directory", store, entries, err)
	}
	record, err := taskflow.Load(filepath.Join(store, entries[0].Name()))
	if err != nil {
		t.Fatalf("load started record: %v", err)
	}

	t.Run("pre-publish denies with an open record", func(t *testing.T) {
		// R-E2E: an open record yields exit 0 and the deny JSON naming the task.
		for _, bash := range publish {
			reason := expectDecision(t, preStdin(t, repo, bash), env(withCW), "deny")
			if !strings.Contains(reason, record.ID) {
				t.Fatalf("%s: deny reason %q does not name task %s", bash, reason, record.ID)
			}
		}
	})

	t.Run("pre-publish asks before cw task abandon", func(t *testing.T) {
		// Acceptance (G5, D2): the handler for cw task abandon answers the
		// abandon of the open record with an ask naming its ID and plan
		// title, and lets cw task show run without output.
		stdin := preStdin(t, outside, "cw task abandon --task "+record.Directory+" --reason x")
		code, stdout, stderr := hooksSh(t, abandonCommand, stdin, env(withCW))
		if code != 0 {
			t.Fatalf("exit %d stderr %q", code, stderr)
		}
		reason := hooksExpectDecision(t, stdout, "ask")
		if want := "task " + record.ID + " (" + record.Plan.Title + ") in " + record.Directory; !strings.Contains(reason, want) {
			t.Fatalf("ask reason %q does not contain %q", reason, want)
		}
		code, stdout, stderr = hooksSh(t, abandonCommand, preStdin(t, outside, "cw task show --task "+record.Directory), env(withCW))
		if code != 0 || stdout != "" {
			t.Fatalf("cw task show: exit %d stdout %q stderr %q; want exit 0 and no output", code, stdout, stderr)
		}
	})

	t.Run("pre-publish denies git -C from outside any repository", func(t *testing.T) {
		// E2: `git -C <repo> commit -m x` with a non-repository cwd is denied.
		reason := expectDecision(t, preStdin(t, outside, "git -C "+repo+" commit -m x"), env(withCW), "deny")
		if !strings.Contains(reason, record.ID) {
			t.Fatalf("deny reason %q does not name task %s", reason, record.ID)
		}
		// Control: the same cwd without -C is allowed.
		expectAllow(t, preStdin(t, outside, "git commit -m x"), env(withCW))
	})

	t.Run("pre-publish honours enforce_verify=false", func(t *testing.T) {
		// R-E2E: CLAUDE_PLUGIN_OPTION_ENFORCE_VERIFY=false allows.
		for _, bash := range publish {
			expectAllow(t, preStdin(t, repo, bash), env(withCW, "CLAUDE_PLUGIN_OPTION_ENFORCE_VERIFY=false"))
		}
	})

	t.Run("pre-publish allows without cw on PATH", func(t *testing.T) {
		// R-E2E: without cw the command exits 0 with no output.
		for _, bash := range publish {
			expectAllow(t, preStdin(t, repo, bash), env(withoutCW))
		}
	})

	withOldCW := hooksStubCW(t, hooksOldCW)
	withSilentCW := hooksStubCW(t, hooksSilentCW)

	t.Run("pre-publish allows with a cw that has no hook command", func(t *testing.T) {
		// R-E2E: a cw that exits 2 (older than the hook subcommand) gives
		// exit 0 and no output.
		for _, bash := range publish {
			expectAllow(t, preStdin(t, repo, bash), env(withOldCW))
		}
	})

	t.Run("pre-publish asks for a large unrecorded staged diff", func(t *testing.T) {
		// E3: a repository with no record whose staged diff meets the
		// thresholds (more than 300 lines of code in two top-level
		// directories, no tests) gets one ask line.
		other := hooksCanonicalTempDir(t)
		hooksGit(t, home, other, "init", "-q")
		hooksGit(t, home, other, "commit", "-q", "--allow-empty", "-m", "initial")
		hooksWriteFile(t, other, "a/x.go", hooksLines(200))
		hooksWriteFile(t, other, "b/y.go", hooksLines(200))
		hooksGit(t, home, other, "add", "-A")
		expectDecision(t, preStdin(t, other, "git commit -m x"), env(withCW), "ask")
	})

	session := file.Hooks["SessionStart"]
	if len(session) != 1 || len(session[0].Hooks) != 1 {
		t.Fatalf("SessionStart groups %+v", session)
	}
	sessionCommand := session[0].Hooks[0].Command
	agents, err := os.ReadFile(filepath.Join(root, "global", "AGENTS.md"))
	if err != nil {
		t.Fatal(err)
	}
	sessionStdin, err := json.Marshal(map[string]any{"session_id": "s", "hook_event_name": "SessionStart", "source": "startup", "cwd": repo})
	if err != nil {
		t.Fatal(err)
	}

	t.Run("session-start with cw emits the instructions and the cw gate install line", func(t *testing.T) {
		// R-E2E: with cw on PATH, one SessionStart JSON line carrying
		// global/AGENTS.md without trailing newlines, a blank line and the
		// line that suggests cw gate install, since the session's repository
		// has the open substantial record and no git hooks.
		code, stdout, stderr := hooksSh(t, sessionCommand, sessionStdin, env(withCW))
		if code != 0 {
			t.Fatalf("exit %d stderr %q", code, stderr)
		}
		if !strings.HasSuffix(stdout, "\n") || strings.Count(stdout, "\n") != 1 {
			t.Fatalf("stdout %q is not one JSON line", stdout)
		}
		var output struct {
			HookSpecificOutput struct {
				HookEventName     string `json:"hookEventName"`
				AdditionalContext string `json:"additionalContext"`
			} `json:"hookSpecificOutput"`
		}
		if err := json.Unmarshal([]byte(stdout), &output); err != nil {
			t.Fatalf("stdout is not JSON: %v", err)
		}
		if output.HookSpecificOutput.HookEventName != "SessionStart" {
			t.Fatalf("hookEventName %q", output.HookSpecificOutput.HookEventName)
		}
		const gateLine = "git hooks absent, so a commit or push from a script, an alias or a terminal skips the verify gate: suggest cw gate install to the user"
		got := output.HookSpecificOutput.AdditionalContext
		if want := strings.TrimRight(string(agents), "\n") + "\n\n" + gateLine; got != want {
			t.Fatalf("additionalContext differs from global/AGENTS.md without trailing newlines, a blank line and %q (got %d bytes ending %q, want %d)",
				gateLine, len(got), got[max(0, len(got)-200):], len(want))
		}
	})

	t.Run("session-start adds nothing when Codex resumes", func(t *testing.T) {
		// Oracle: under Codex, which sets PLUGIN_ROOT, a resume prints one
		// SessionStart JSON line without additionalContext, exactly as cw
		// wrote it: not the file, which the command prints when cw prints
		// nothing, and not the instructions, which the resumed thread holds.
		resume, err := json.Marshal(map[string]any{"session_id": "s", "hook_event_name": "SessionStart", "source": "resume", "cwd": repo, "model": "m"})
		if err != nil {
			t.Fatal(err)
		}
		const want = `{"hookSpecificOutput":{"hookEventName":"SessionStart"}}` + "\n"
		code, stdout, stderr := hooksSh(t, sessionCommand, resume, env(withCW, "PLUGIN_ROOT="+root))
		if code != 0 || stdout != want {
			t.Fatalf("exit %d stderr %q stdout %q; want exit 0 and %q", code, stderr, stdout, want)
		}
	})

	t.Run("session-start without cw adds the inactive-gate line", func(t *testing.T) {
		// E4: without cw on PATH, the raw file verbatim followed by exactly
		// one line saying the gate is inactive, newline-terminated.
		// oracle: stdout equals global/AGENTS.md followed by gateInactiveLine,
		// whose install command names the plugin's release tag.
		if !strings.HasSuffix(string(agents), "\n") {
			t.Fatal("global/AGENTS.md does not end with a newline; the expected output below assumes it does")
		}
		line := gateInactiveLine(t)
		want := string(agents) + line + "\n"
		code, stdout, stderr := hooksSh(t, sessionCommand, sessionStdin, env(withoutCW))
		if code != 0 || stdout != want {
			t.Fatalf("exit %d stderr %q stdout %q; want exit 0 and the %d-byte file followed by %q", code, stderr, stdout, len(agents), line+"\n")
		}
	})

	t.Run("session-start falls back to the raw file", func(t *testing.T) {
		// E4, R-E2E: with a cw that exits 2 and with a cw that prints
		// nothing, the command exits 0 and prints the raw file verbatim, with
		// no extra line.
		tests := []struct {
			name string
			path string
		}{
			{"cw exits 2", withOldCW},
			{"cw prints nothing", withSilentCW},
		}
		for _, test := range tests {
			t.Run(test.name, func(t *testing.T) {
				code, stdout, stderr := hooksSh(t, sessionCommand, sessionStdin, env(test.path))
				if code != 0 || stdout != string(agents) {
					t.Fatalf("exit %d stderr %q stdout %d bytes; want exit 0 and the %d-byte file verbatim (equal: %v)", code, stderr, len(stdout), len(agents), stdout == string(agents))
				}
			})
		}
	})
}
