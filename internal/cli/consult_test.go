package cli

import (
	"bytes"
	"context"
	json "encoding/json/v2"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/belevtsev/agent-workflows/internal/manageruntime"
)

const sharedRequest = `{"state":"evidence","questions":{"claim":{"type":"choice","instructions":"Is it supported?","criteria":{"supported":"Yes.","contradicted":"No."}}}}`

// installedRelease places the policy where an active installation holds it:
// behind the state directory's current symlink.
func installedRelease(t *testing.T, state string) {
	t.Helper()
	release := filepath.Join(state, "releases", "fixture")
	skill := filepath.Join(release, "skills", "task-orchestration")
	source := filepath.Join("..", "..", "skills", "task-orchestration")
	if err := os.MkdirAll(filepath.Join(skill, "references"), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"model-policy.yaml", filepath.Join("references", "jev-questions.json")} {
		data, err := os.ReadFile(filepath.Join(source, name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(skill, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(release, filepath.Join(state, "current")); err != nil {
		t.Fatal(err)
	}
}

func requestFile(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	path := filepath.Join(root, "request.json")
	if err := os.WriteFile(path, []byte(sharedRequest), 0o600); err != nil {
		t.Fatal(err)
	}
	return path, filepath.Join(root, "record.json")
}

func TestConsultDefaultPolicyFollowsStateResolutionOrder(t *testing.T) {
	for name, setup := range map[string]func(*Config, *[]string, string){
		"state flag": func(_ *Config, args *[]string, state string) { *args = append(*args, "--state-dir", state) },
		"locator": func(config *Config, _ *[]string, state string) {
			config.Resolve = func(string) (manageruntime.Locator, error) { return manageruntime.Locator{State: state}, nil }
		},
		"XDG": func(config *Config, _ *[]string, state string) {
			parent := filepath.Dir(state)
			config.Getenv = func(key string) string {
				if key == "XDG_STATE_HOME" {
					return parent
				}
				return ""
			}
		},
		"home": func(config *Config, _ *[]string, state string) {
			home := filepath.Dir(filepath.Dir(filepath.Dir(state)))
			config.HomeDir = func() (string, error) { return home, nil }
		},
	} {
		t.Run(name, func(t *testing.T) {
			state := filepath.Join(t.TempDir(), ".local", "state", "agent-workflows")
			if err := os.MkdirAll(state, 0o700); err != nil {
				t.Fatal(err)
			}
			installedRelease(t, state)
			request, output := requestFile(t)
			config, out, _, _ := testConfig(t)
			args := []string{"consult", "--request", request, "--output", output, "--dry-run"}
			setup(&config, &args, state)
			if err := Execute(t.Context(), args, config); err != nil {
				t.Fatal(err)
			}
			if out.String() != "Decision consultation dry_run; Coordinator decision pending\n" {
				t.Fatalf("stdout = %q", out.String())
			}
			data, err := os.ReadFile(output)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(data), `"requested_model": "gpt-6-luna"`) {
				t.Fatalf("record = %s", data)
			}
		})
	}
}

func TestConsultExplicitPolicyAndExitStatuses(t *testing.T) {
	policy := filepath.Join("..", "..", "skills", "task-orchestration", "model-policy.yaml")
	request, output := requestFile(t)
	config, out, _, _ := testConfig(t)
	stderr := new(bytes.Buffer)
	config.Err = stderr
	config.Getenv = func(string) string { return "" }
	// Missing credentials make no requests and report incomplete coverage.
	err := Execute(t.Context(), []string{"consult", "--request", request, "--output", output, "--policy", policy}, config)
	if ExitCode(err) != 2 || err.Error() != "" {
		t.Fatalf("error = %v, exit %d", err, ExitCode(err))
	}
	if out.String() != "Decision consultation unavailable; Coordinator decision pending\n" || stderr.Len() != 0 {
		t.Fatalf("stdout %q stderr %q", out, stderr)
	}
	out.Reset()
	err = Execute(t.Context(), []string{"consult", "--request", request, "--output", output, "--policy", policy}, config)
	if ExitCode(err) != 1 || err.Error() != "" || stderr.String() != "Decision consultation validation failed: cannot create a new output file\n" {
		t.Fatalf("error = %v, exit %d, stderr %q", err, ExitCode(err), stderr)
	}
}

func TestConsultAndGraphifyUsageErrorsExitTwo(t *testing.T) {
	for _, args := range [][]string{
		{"consult"}, {"consult", "--request", "r"}, {"consult", "--request", "r", "--adjudicate", "a", "--decision", "d", "--output", "o"},
		{"consult", "--request", "r", "--decision", "d", "--output", "o"}, {"consult", "--adjudicate", "a", "--output", "o"},
		{"consult", "--adjudicate", "a", "--decision", "d", "--output", "o", "--dry-run"},
		{"consult", "--adjudicate", "a", "--decision", "d", "--output", "o", "--with-openai"},
		{"consult", "--request", "r", "--output", "o", "--with-openai", "--without-openai"},
		{"consult", "--unknown"}, {"consult", "unexpected"},
		{"graphify", "unexpected"}, {"graphify", "build"}, {"graphify", "query", "--repo", "r"},
		{"graphify", "build", "--repo", "r", "--timeout", "nan"}, {"graphify", "build", "--repo", "r", "--timeout", "inf"},
		{"graphify", "build", "--repo", "r", "--timeout", "-1"}, {"graphify", "build", "--repo", "r", "--timeout", "601"},
		{"graphify", "query", "--repo", "r", "helper", "--budget", "0"}, {"graphify", "query", "--repo", "r", "helper", "--budget", "16001"},
		{"graphify", "query", "--repo", "r", "helper", "--timeout", "nan"}, {"graphify", "query", "--repo", "r", " "},
		{"graphify", "query", "--repo", "r", strings.Repeat("x", 8001)},
	} {
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			config, _, _, _ := testConfig(t)
			config.Getenv = func(string) string { t.Fatal("rejected arguments read the environment"); return "" }
			if err := Execute(t.Context(), args, config); ExitCode(err) != 2 {
				t.Fatalf("error = %v, exit %d", err, ExitCode(err))
			}
		})
	}
}

func TestGraphifyBuildRejectsInheritedDryRunBeforeWrites(t *testing.T) {
	for _, args := range [][]string{{"--dry-run", "graphify", "build"}, {"graphify", "build", "--dry-run"}} {
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			config, _, _, _ := testConfig(t)
			config.HomeDir = func() (string, error) { t.Fatal("dry-run accessed storage"); return "", nil }
			directory := t.TempDir()
			marker, cache := filepath.Join(directory, "ran"), filepath.Join(directory, "cache")
			runtime := filepath.Join(directory, "graphify")
			if err := os.WriteFile(runtime, []byte("#!/bin/sh\ntouch '"+marker+"'\n"), 0o700); err != nil {
				t.Fatal(err)
			}
			err := Execute(t.Context(), append(args, "--repo", directory, "--runtime", runtime, "--cache-root", cache), config)
			if err == nil || ExitCode(err) != 2 || !strings.Contains(err.Error(), "rejects --dry-run") {
				t.Fatalf("dry-run was not rejected explicitly: %v", err)
			}
			for _, path := range []string{marker, cache} {
				if _, err := os.Lstat(path); !os.IsNotExist(err) {
					t.Fatalf("dry-run wrote %s: %v", path, err)
				}
			}
		})
	}
}

func TestGraphifyReportsUnavailableAsStructuredOutput(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is required")
	}
	config, out, _, _ := testConfig(t)
	missing := filepath.Join(t.TempDir(), "missing")
	err := Execute(t.Context(), []string{"graphify", "build", "--repo", missing}, config)
	if ExitCode(err) != 2 || err.Error() != "" {
		t.Fatalf("error = %v, exit %d", err, ExitCode(err))
	}
	var report graphifyFailure
	if decodeErr := json.Unmarshal(out.Bytes(), &report); decodeErr != nil {
		t.Fatalf("%v: %s", decodeErr, out)
	}
	if report.Status != "unavailable" || report.Code != "repository" || report.Fallback == "" || report.Detail == "" {
		t.Fatalf("report = %+v", report)
	}
}

func TestReportedExitKeepsCancellationPrecedence(t *testing.T) {
	if ExitCode(reportedExit(2)) != 2 || ExitCode(context.Canceled) != 130 || ExitCode(errors.New("failure")) != 1 || ExitCode(finish(0, nil)) != 0 {
		t.Fatal("wrong exit mapping")
	}
	if err := finish(1, context.Canceled); ExitCode(err) != 130 {
		t.Fatalf("cancellation lost: %v", err)
	}
}
