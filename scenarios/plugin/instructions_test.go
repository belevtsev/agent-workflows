package plugin_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Requirement "Instruction text comes from cw", scenario "cw is upgraded".
func TestSessionContextComesFromTheInstalledCW(t *testing.T) {
	t.Parallel()
	needShell(t)
	repo := gatedDemo(t)
	const newer = "Newer cw text 7f3a: run the checks with cw verify."
	fake := t.TempDir()
	script := "#!/bin/sh\nif [ \"$1\" = context ]; then echo '" + newer + "'; exit 0; fi\nexit 2\n"
	if err := os.WriteFile(filepath.Join(fake, "cw"), []byte(script), 0o755); err != nil { //nolint:gosec // the fake cw must be executable
		t.Fatal(err)
	}
	env := withPath(repo.Env(), fake+string(os.PathListSeparator)+pathOf(repo.Env()))
	for _, h := range hostFiles {
		t.Run(h.host, func(t *testing.T) {
			result := sessionStart(t, repo, env, h.file)
			if !strings.Contains(result.Stdout, newer) {
				t.Errorf("the context (exit %d) does not hold the text of the newer cw:\n%s", result.Code, result.Output())
			}
		})
	}
}

// Requirement "Instruction text comes from cw", scenario "cw is not
// installed".
func TestSessionContextWhenCWIsNotInstalled(t *testing.T) {
	t.Parallel()
	needShell(t)
	repo := gatedDemo(t)
	bin := t.TempDir()
	for _, program := range []string{"git", "sh"} {
		path, err := exec.LookPath(program)
		if err != nil {
			t.Skipf("%s is not on PATH: %v", program, err)
		}
		if err := os.Symlink(path, filepath.Join(bin, program)); err != nil {
			t.Fatal(err)
		}
	}
	env := withPath(repo.Env(), bin)
	goCommand := regexp.MustCompile(`\bgo (install|build|run|get)\b`)
	for _, h := range hostFiles {
		t.Run(h.host, func(t *testing.T) {
			result := sessionStart(t, repo, env, h.file)
			text := strings.TrimRight(result.Stdout, "\r\n")
			lower := strings.ToLower(text)
			if text == "" || strings.ContainsAny(text, "\r\n") {
				t.Fatalf("the context (exit %d) is not one line:\n%s", result.Code, result.Output())
			}
			if !strings.Contains(lower, "cw is not installed") {
				t.Errorf("the line does not say that cw is not installed:\n%s", text)
			}
			if !strings.Contains(lower, "commit") || !strings.Contains(lower, "denied") {
				t.Errorf("the line does not say that commits will be denied:\n%s", text)
			}
			if !strings.Contains(lower, "install") || !regexp.MustCompile(`https?://\S+|\S+\.md\b`).MatchString(text) {
				t.Errorf("the line does not say where the install instruction is:\n%s", text)
			}
			if goCommand.MatchString(text) {
				t.Errorf("the line names a Go command:\n%s", text)
			}
		})
	}
}

// Requirement "Instruction text comes from cw": the plugin holds neither a
// copy of the session text nor a cw version number, and (scenario "cw is not
// installed") names no Go command.
func TestHookFilesHoldNoCopyVersionOrGoCommand(t *testing.T) {
	t.Parallel()
	repo := gatedDemo(t)
	context := repo.CW("context")
	if context.Code != 0 || context.Stdout == "" {
		t.Fatalf("cw context exited %d and printed:\n%s", context.Code, context.Output())
	}
	version := regexp.MustCompile(`\bv?\d+\.\d+\.\d+|cw@`)
	goCommand := regexp.MustCompile(`\bgo (install|build|run|get)\b`)
	for _, h := range hostFiles {
		t.Run(h.host, func(t *testing.T) {
			content := readHookFile(t, h.file)
			for _, line := range strings.Split(context.Stdout, "\n") {
				line = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "- "))
				if len(line) >= 20 && strings.Contains(content, line) {
					t.Errorf("%s holds a copy of the session text: %q", h.file, line)
				}
			}
			if found := version.FindString(content); found != "" {
				t.Errorf("%s holds a version number: %q", h.file, found)
			}
			if found := goCommand.FindString(content); found != "" {
				t.Errorf("%s names a Go command: %q", h.file, found)
			}
		})
	}
}
