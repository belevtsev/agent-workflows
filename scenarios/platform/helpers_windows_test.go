//go:build windows

package platform_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/belevtsev/kitsu/v3/internal/kitsutest"
)

// extensions are the extensions that Windows tries for a name without one.
func extensions() []string {
	pathext := os.Getenv("PATHEXT")
	if pathext == "" {
		pathext = ".COM;.EXE;.BAT;.CMD"
	}
	var exts []string
	for _, ext := range strings.Split(pathext, ";") {
		if ext != "" {
			exts = append(exts, strings.ToLower(ext))
		}
	}
	return exts
}

// holds says whether dir holds a program that Windows finds as name.
func holds(dir, name string) bool {
	for _, ext := range extensions() {
		if info, err := os.Stat(filepath.Join(dir, name+ext)); err == nil && !info.IsDir() {
			return true
		}
	}
	return false
}

// pathWithoutSh is the repository's PATH without every directory that holds
// `sh`, and with a directory that holds git: Git for Windows keeps git.exe
// alone in ...\Git\cmd, while ...\Git\bin and ...\Git\usr\bin also hold
// sh.exe.
func pathWithoutSh(t *testing.T, repo *kitsutest.Repo) string {
	t.Helper()
	var kept, dropped []string
	for _, dir := range filepath.SplitList(pathOf(repo)) {
		if dir == "" {
			continue
		}
		if holds(dir, "sh") {
			dropped = append(dropped, dir)
			continue
		}
		kept = append(kept, dir)
	}
	hasGit := false
	for _, dir := range kept {
		if holds(dir, "git") {
			hasGit = true
			break
		}
	}
	if !hasGit {
		for _, dir := range dropped {
			for _, root := range []string{filepath.Dir(dir), filepath.Dir(filepath.Dir(dir))} {
				candidate := filepath.Join(root, "cmd")
				if !hasGit && holds(candidate, "git") && !holds(candidate, "sh") {
					kept = append(kept, candidate)
					hasGit = true
				}
			}
		}
	}
	if !hasGit {
		t.Fatalf("no directory holds git without sh; PATH without sh: %v, left out: %v", kept, dropped)
	}
	return strings.Join(kept, string(os.PathListSeparator))
}

// buildProgram compiles a main package of one file into dir\name.exe.
func buildProgram(t *testing.T, dir, name, source string) string {
	t.Helper()
	src := filepath.Join(t.TempDir(), name+".go")
	if err := os.WriteFile(src, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, name+".exe")
	build := exec.Command("go", "build", "-o", out, src)
	build.Dir = filepath.Dir(src)
	build.Env = append(os.Environ(), "GOWORK=off", "GOFLAGS=")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("building %s: %v\n%s", name, err, output)
	}
	return out
}

// exists says whether a file exists.
func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// reasonLines are the lines of the reason a hook printed, without what the
// shell that started git put around them.
func reasonLines(output string) []string {
	var lines []string
	for _, line := range strings.Split(strings.ReplaceAll(output, "\r\n", "\n"), "\n") {
		if i := strings.Index(line, "kitsu: "); i >= 0 {
			line = line[i:]
		}
		if line = strings.TrimSpace(line); line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

// pathOf is the PATH that the repository's commands run with.
func pathOf(repo *kitsutest.Repo) string {
	path := ""
	for _, kv := range repo.Env() {
		name, value, _ := strings.Cut(kv, "=")
		if strings.EqualFold(name, "PATH") {
			path = value
		}
	}
	return path
}

// prependPath puts dir first on the PATH of the repository's commands.
func prependPath(repo *kitsutest.Repo, dir string) {
	repo.Setenv("PATH", dir+string(os.PathListSeparator)+pathOf(repo))
}
