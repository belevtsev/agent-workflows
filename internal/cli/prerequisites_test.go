package cli

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/belevtsev/agent-workflows/internal/workflow"
)

func TestOptionalPrerequisitesReportMissingToolsWithoutExecutingThem(t *testing.T) {
	missing := func(string) (string, error) { return "", os.ErrNotExist }
	noFile := func(string) (os.FileInfo, error) { return nil, os.ErrNotExist }
	report := skillPrerequisites(workflow.Object{"registrations": []string{"archify", "docker-agent-run"}}, "/fixture/home", noFile, missing)
	for _, name := range []string{"node", "browser", "docker"} {
		if report[name].Status != "missing" {
			t.Fatalf("missing %s not reported: %+v", name, report[name])
		}
	}
	if report["docker_agent"].Status != "not_checked" || report["docker_sandboxes"].Status != "not_checked" {
		t.Fatal("invented subcommand availability")
	}
	detected := func(string) (string, error) { return "/fixture/tool", nil }
	report = skillPrerequisites(workflow.Object{"registrations": []string{"archify"}}, "/fixture/home", noFile, detected)
	if report["node"].Status != "detected" || report["browser"].Status != "detected" {
		t.Fatal("available executable not detected")
	}
	if len(skillPrerequisites(workflow.Object{"registrations": []string{"typesafe-ai"}}, "/fixture/home", noFile, detected)) != 0 {
		t.Fatal("reported prerequisites for uninstalled skills")
	}
}

func TestGraphifyPrerequisiteRequiresRegistration(t *testing.T) {
	stat := func(path string) (os.FileInfo, error) {
		t.Fatalf("checked unregistered Graphify executable %q", path)
		return nil, os.ErrNotExist
	}
	lookup := func(name string) (string, error) {
		t.Fatalf("looked up unregistered Graphify executable %q", name)
		return "", os.ErrNotExist
	}
	if report := skillPrerequisites(workflow.Object{"registrations": []string{"typesafe-ai"}}, "/fixture/home", stat, lookup); len(report) != 0 {
		t.Fatalf("reported prerequisites for unregistered skills: %+v", report)
	}
}

func TestGraphifyPrerequisiteExecutableDetection(t *testing.T) {
	home := filepath.Join(t.TempDir(), "custom-installation-home")
	defaultPath := filepath.Join(home, ".venvs", "agent-graphify", "bin", "graphify")
	if runtime.GOOS == "windows" {
		defaultPath = filepath.Join(home, ".venvs", "agent-graphify", "Scripts", "graphify.exe")
	}
	pathCandidate := filepath.Join(t.TempDir(), "portable", "graphify")
	if runtime.GOOS == "windows" {
		pathCandidate += ".exe"
	}
	executable := prerequisiteFileInfo(t, 0755)
	nonExecutable := prerequisiteFileInfo(t, 0644)
	directory, err := os.Stat(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS == "windows" {
		// Windows executable detection uses file type and the executable name,
		// since permission bits do not identify runnable files there.
		nonExecutable = directory
	}
	for _, test := range []struct {
		name       string
		files      map[string]os.FileInfo
		lookupPath string
		wantStatus string
		wantPath   string
		wantLookup bool
	}{
		{name: "missing", wantStatus: "missing", wantPath: defaultPath, wantLookup: true},
		{name: "custom home executable", files: map[string]os.FileInfo{defaultPath: executable}, wantStatus: "detected", wantPath: defaultPath},
		{name: "PATH fallback", files: map[string]os.FileInfo{pathCandidate: executable}, lookupPath: pathCandidate, wantStatus: "detected", wantPath: pathCandidate, wantLookup: true},
		{name: "non-executable default falls back", files: map[string]os.FileInfo{defaultPath: nonExecutable, pathCandidate: executable}, lookupPath: pathCandidate, wantStatus: "detected", wantPath: pathCandidate, wantLookup: true},
		{name: "non-executable paths", files: map[string]os.FileInfo{defaultPath: nonExecutable, pathCandidate: nonExecutable}, lookupPath: pathCandidate, wantStatus: "missing", wantPath: defaultPath, wantLookup: true},
		{name: "directories", files: map[string]os.FileInfo{defaultPath: directory, pathCandidate: directory}, lookupPath: pathCandidate, wantStatus: "missing", wantPath: defaultPath, wantLookup: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			var checked []string
			stat := func(path string) (os.FileInfo, error) {
				checked = append(checked, path)
				if info, ok := test.files[path]; ok {
					return info, nil
				}
				return nil, os.ErrNotExist
			}
			lookedUp := false
			lookup := func(name string) (string, error) {
				lookedUp = true
				if name != "graphify" {
					t.Fatalf("looked up unrelated executable %q", name)
				}
				if test.lookupPath == "" {
					return "", os.ErrNotExist
				}
				return test.lookupPath, nil
			}
			report := skillPrerequisites(workflow.Object{"registrations": []string{"graphify"}}, home, stat, lookup)
			got := report["graphify"]
			if len(report) != 1 || got.Status != test.wantStatus {
				t.Fatalf("unexpected Graphify prerequisite: %+v", report)
			}
			if !strings.Contains(got.Detail, test.wantPath) || !strings.Contains(got.Detail, "graphifyy==0.9.91, checked only when the skill is used") {
				t.Fatalf("missing path or version-check limitation: %q", got.Detail)
			}
			if len(checked) == 0 || checked[0] != defaultPath {
				t.Fatalf("did not check resolved installation home first: %v", checked)
			}
			if lookedUp != test.wantLookup {
				t.Fatalf("PATH lookup = %v, want %v", lookedUp, test.wantLookup)
			}
		})
	}
}

func prerequisiteFileInfo(t *testing.T, mode os.FileMode) os.FileInfo {
	t.Helper()
	path := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(path, nil, mode); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info
}
