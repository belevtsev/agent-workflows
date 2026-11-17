package plugin_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/belevtsev/kitsu/v3/internal/kitsutest"
)

// Requirement "Instruction text comes from kitsu", scenario "kitsu is upgraded".
func TestSessionContextComesFromTheInstalledKitsu(t *testing.T) {
	t.Parallel()
	needShell(t)
	repo := gatedDemo(t)
	const newer = "Newer kitsu text 7f3a: run the checks with kitsu verify."
	fake := t.TempDir()
	script := "#!/bin/sh\nif [ \"$1\" = context ]; then echo '" + newer + "'; exit 0; fi\nexit 2\n"
	writeProgram(t, filepath.Join(fake, "kitsu"), script)
	env := withPath(repo.Env(), fake+string(os.PathListSeparator)+pathOf(repo.Env()))
	for _, h := range hostFiles {
		t.Run(h.host, func(t *testing.T) {
			result := sessionStart(t, repo, env, h.file)
			if !strings.Contains(result.Stdout, newer) {
				t.Errorf("the context (exit %d) does not hold the text of the newer kitsu:\n%s", result.Code, result.Output())
			}
		})
	}
}

// Requirement "Instruction text comes from kitsu", scenario "kitsu is not
// installed".
func TestSessionContextWhenKitsuIsNotInstalled(t *testing.T) {
	t.Parallel()
	needShell(t)
	repo := gatedDemo(t)
	plain := kitsutest.NewRepo(t)
	plain.Write("nested/README.md", "# no gate\n")
	bin := toolFolder(t, "sh") // Installation guidance must not depend on Git being available.
	env := withPath(repo.Env(), bin)
	goCommand := regexp.MustCompile(`\bgo (install|build|run|get)\b`)
	for _, h := range hostFiles {
		for name, dir := range map[string]*kitsutest.Repo{"gated": repo, "plain": plain, "subfolder": plain.In("nested"), "outside Git": plain.In(t.TempDir())} {
			t.Run(h.host+"/"+name, func(t *testing.T) {
				result := sessionStart(t, dir, env, h.file)
				text := strings.TrimRight(result.Stdout, "\r\n")
				lower := strings.ToLower(text)
				if text == "" || strings.ContainsAny(text, "\r\n") {
					t.Fatalf("the context (exit %d) is not one line:\n%s", result.Code, result.Output())
				}
				if !strings.Contains(lower, "kitsu is not installed") {
					t.Errorf("the line does not say that kitsu is not installed:\n%s", text)
				}
				if strings.Contains(lower, "commit") || strings.Contains(lower, "denied") {
					t.Errorf("installation guidance must not guess whether Git hooks enforce a gate:\n%s", text)
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
}

// Requirement "Instruction text comes from kitsu": the plugin holds neither a
// copy of the session text nor a kitsu version number, and (scenario "kitsu is not
// installed") names no Go command.
func TestHookFilesHoldNoCopyVersionOrGoCommand(t *testing.T) {
	t.Parallel()
	repo := gatedDemo(t)
	context := repo.Kitsu("context")
	if context.Code != 0 || context.Stdout == "" {
		t.Fatalf("kitsu context exited %d and printed:\n%s", context.Code, context.Output())
	}
	version := regexp.MustCompile(`\bv?\d+\.\d+\.\d+|kitsu@`)
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
