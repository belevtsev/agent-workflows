package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// carveoutAllProviders is every provider, in record order.
var carveoutAllProviders = []string{"jev", "subq_snap", "openai_decisions"}

// carveoutEmbeddedProviders is the embedded default policy's enabled providers.
var carveoutEmbeddedProviders = []string{"subq_snap", "openai_decisions"}

// carveoutPrivateDir returns a fresh, empty 0700 directory without symlink
// components, as configuration and task storage require.
func carveoutPrivateDir(t *testing.T) string {
	t.Helper()
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(base, "private")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	return dir
}

// carveoutOptedInDir is a private configuration directory whose preferences
// opt in to every adviser, so the selected policy alone decides who takes part.
func carveoutOptedInDir(t *testing.T) string {
	t.Helper()
	dir := carveoutPrivateDir(t)
	if err := os.WriteFile(filepath.Join(dir, "preferences.json"), []byte(optInEveryAdviser), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

type carveoutConsultResult struct {
	err    error
	stderr string
	output string
}

// carveoutRunConsult runs `[rootFlags] consult [flags] --request R --output O
// --dry-run` with a fresh configuration directory.
func carveoutRunConsult(t *testing.T, config Config, rootFlags []string, flags ...string) carveoutConsultResult {
	t.Helper()
	request, output := requestFile(t)
	var stderr bytes.Buffer
	config.Err = &stderr
	config.Out = io.Discard
	args := append([]string{"--config-dir", carveoutOptedInDir(t)}, rootFlags...)
	args = append(args, "consult")
	args = append(args, flags...)
	args = append(args, "--request", request, "--output", output, "--dry-run")
	err := Execute(context.Background(), args, config)
	return carveoutConsultResult{err: err, stderr: stderr.String(), output: output}
}

// carveoutRecord reads the dry-run preview at path, a JSON object, and returns
// its top-level status and each provider's status (PR-1). Both must be
// strings, and providers must be an object of objects.
func carveoutRecord(t *testing.T, path string) (string, map[string]string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read record: %v", err)
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(data, &top); err != nil {
		t.Fatalf("record is not a JSON object: %v", err)
	}
	var status string
	if err := json.Unmarshal(top["status"], &status); err != nil || top["status"] == nil {
		t.Fatalf("record status = %s; want a string", top["status"])
	}
	var providers map[string]map[string]json.RawMessage
	if err := json.Unmarshal(top["providers"], &providers); err != nil || providers == nil {
		t.Fatalf("record providers = %s; want an object of objects", top["providers"])
	}
	statuses := map[string]string{}
	for provider, outcome := range providers {
		var providerStatus string
		if err := json.Unmarshal(outcome["status"], &providerStatus); err != nil || outcome["status"] == nil {
			t.Fatalf("provider %s status = %s; want a string", provider, outcome["status"])
		}
		statuses[provider] = providerStatus
	}
	return status, statuses
}

// carveoutWithCredentials merges every provider's fixture credential into env.
func carveoutWithCredentials(env map[string]string) map[string]string {
	maps.Copy(env, fixtureCredentials(carveoutAllProviders...))
	return env
}

// carveoutPersonalSymlink creates the personal policy path under home, with
// its parent directories, as a symlink to target.
func carveoutPersonalSymlink(t *testing.T, home, target string) {
	t.Helper()
	dir := filepath.Join(home, ".agents", "skills", "task-orchestration")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(dir, "model-policy.yaml")); err != nil {
		t.Fatal(err)
	}
}

// PR-1: policy selection order for `cw consult`.
func TestCarveoutConsultPolicySelection(t *testing.T) {
	for _, tc := range []struct {
		name string
		// setup prepares policies and returns root flags and consult flags;
		// it may also replace the configuration's working directory.
		setup func(t *testing.T, home string, env map[string]string, config *Config) (root, flags []string)
		want  []string
	}{
		{
			name: "a flag beats environment and personal file",
			setup: func(t *testing.T, home string, env map[string]string, _ *Config) ([]string, []string) {
				flag := writePolicyFixture(t, t.TempDir(), 3, []fixturePin{pinJev})
				env["CW_MODEL_POLICY"] = writePolicyFixture(t, t.TempDir(), 3, []fixturePin{pinSnap})
				personalPolicyFixture(t, home, 3, []fixturePin{pinOpenAI})
				return nil, []string{"--policy", flag}
			},
			want: []string{"jev"},
		},
		{
			name: "b environment beats personal file",
			setup: func(t *testing.T, home string, env map[string]string, _ *Config) ([]string, []string) {
				env["CW_MODEL_POLICY"] = writePolicyFixture(t, t.TempDir(), 3, []fixturePin{pinSnap})
				personalPolicyFixture(t, home, 3, []fixturePin{pinOpenAI})
				return nil, nil
			},
			want: []string{"subq_snap"},
		},
		{
			name: "c personal file",
			setup: func(t *testing.T, home string, env map[string]string, _ *Config) ([]string, []string) {
				personalPolicyFixture(t, home, 3, []fixturePin{pinOpenAI})
				return nil, nil
			},
			want: []string{"openai_decisions"},
		},
		{
			name: "d embedded default",
			setup: func(t *testing.T, home string, env map[string]string, _ *Config) ([]string, []string) {
				return nil, nil
			},
			want: carveoutEmbeddedProviders,
		},
		{
			name: "e home flag selects its personal file",
			setup: func(t *testing.T, home string, env map[string]string, _ *Config) ([]string, []string) {
				personalPolicyFixture(t, home, 3, []fixturePin{pinSnap})
				other := t.TempDir()
				personalPolicyFixture(t, other, 3, []fixturePin{pinJev})
				return []string{"--home", other}, nil
			},
			want: []string{"jev"},
		},
		{
			name: "g personal path is a symlink to a policy file",
			setup: func(t *testing.T, home string, env map[string]string, _ *Config) ([]string, []string) {
				target := writePolicyFixture(t, t.TempDir(), 3, []fixturePin{pinOpenAI})
				carveoutPersonalSymlink(t, home, target)
				return nil, nil
			},
			want: []string{"openai_decisions"},
		},
		{
			name: "i home flag with trailing slash",
			setup: func(t *testing.T, home string, env map[string]string, _ *Config) ([]string, []string) {
				personalPolicyFixture(t, home, 3, []fixturePin{pinSnap})
				other := t.TempDir()
				personalPolicyFixture(t, other, 3, []fixturePin{pinJev})
				return []string{"--home", other + "/"}, nil
			},
			want: []string{"jev"},
		},
		{
			name: "i relative home flag resolves against the working directory",
			setup: func(t *testing.T, home string, env map[string]string, config *Config) ([]string, []string) {
				personalPolicyFixture(t, home, 3, []fixturePin{pinSnap})
				// Oracle: cw joins a relative --home to its working directory
				// (os.Getwd in production, config.WorkingDir here), so a path
				// relative to that directory selects other's policy. Both
				// directories are temporary, so the path climbs a fixed
				// three levels wherever the checkout sits.
				working := filepath.Join(t.TempDir(), "work", "dir")
				if err := os.MkdirAll(working, 0o700); err != nil {
					t.Fatal(err)
				}
				config.WorkingDir = func() (string, error) { return working, nil }
				other := t.TempDir()
				personalPolicyFixture(t, other, 3, []fixturePin{pinJev})
				relative, err := filepath.Rel(working, other)
				if err != nil || !strings.HasPrefix(relative, "..") {
					t.Fatalf("relative path from %q to %q = %q, %v; want one that climbs", working, other, relative, err)
				}
				return []string{"--home", relative}, nil
			},
			want: []string{"jev"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			config, _ := testConfig(t)
			env := carveoutWithCredentials(map[string]string{})
			config.Getenv = fixtureEnvironment(env)
			root, flags := tc.setup(t, homeOf(t, config), env, &config)
			result := carveoutRunConsult(t, config, root, flags...)
			if result.err != nil {
				t.Fatalf("consult error = %v; stderr: %s", result.err, result.stderr)
			}
			status, providers := carveoutRecord(t, result.output)
			if status != "dry_run" {
				t.Errorf("record status = %q; want dry_run", status)
			}
			got := slices.Sorted(maps.Keys(providers))
			want := slices.Sorted(slices.Values(tc.want))
			if !slices.Equal(got, want) {
				t.Fatalf("record providers = %q; want %q", got, want)
			}
			for provider, providerStatus := range providers {
				if providerStatus != "dry_run" {
					t.Errorf("provider %s status = %q with its credential set; want dry_run", provider, providerStatus)
				}
			}
		})
	}

	// PR-1 (f, h): a personal path that exists but is not a readable policy
	// file fails the command; only a missing path falls back.
	for _, tc := range []struct {
		name  string
		setup func(t *testing.T, home string)
	}{
		{"f personal path is a directory", func(t *testing.T, home string) {
			path := filepath.Join(home, ".agents", "skills", "task-orchestration", "model-policy.yaml")
			if err := os.MkdirAll(path, 0o700); err != nil {
				t.Fatal(err)
			}
		}},
		{"h personal path is a dangling symlink", func(t *testing.T, home string) {
			carveoutPersonalSymlink(t, home, filepath.Join(t.TempDir(), "missing.yaml"))
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			config, _ := testConfig(t)
			config.Getenv = fixtureEnvironment(carveoutWithCredentials(map[string]string{}))
			tc.setup(t, homeOf(t, config))
			result := carveoutRunConsult(t, config, nil)
			if result.err == nil {
				t.Fatal("consult error = nil; want an error")
			}
			if code := ExitCode(result.err); code != 1 {
				t.Errorf("ExitCode = %d; want 1 (error %v)", code, result.err)
			}
			if _, err := os.Lstat(result.output); !errors.Is(err, os.ErrNotExist) {
				t.Errorf("output stat error = %v; want the output not created", err)
			}
		})
	}

	t.Run("d embedded default without credentials", func(t *testing.T) {
		config, _ := testConfig(t)
		result := carveoutRunConsult(t, config, nil)
		if result.err != nil {
			t.Fatalf("consult error = %v; stderr: %s", result.err, result.stderr)
		}
		status, providers := carveoutRecord(t, result.output)
		if status != "dry_run" {
			t.Errorf("record status = %q; want dry_run", status)
		}
		if got := slices.Sorted(maps.Keys(providers)); !slices.Equal(got, slices.Sorted(slices.Values(carveoutEmbeddedProviders))) {
			t.Fatalf("record providers = %q; want %q", got, carveoutEmbeddedProviders)
		}
		for provider, providerStatus := range providers {
			if providerStatus != "not_configured" {
				t.Errorf("provider %s status = %q without a credential; want not_configured", provider, providerStatus)
			}
		}
	})
}

// PR-2: an unreadable selected policy fails the command.
func TestCarveoutConsultUnreadablePolicy(t *testing.T) {
	t.Run("environment names a missing file", func(t *testing.T) {
		config, _ := testConfig(t)
		env := carveoutWithCredentials(map[string]string{
			"CW_MODEL_POLICY": filepath.Join(t.TempDir(), "missing.yaml"),
		})
		config.Getenv = fixtureEnvironment(env)
		result := carveoutRunConsult(t, config, nil)
		if result.err == nil {
			t.Fatal("consult error = nil; want an error for a missing CW_MODEL_POLICY file")
		}
		if code := ExitCode(result.err); code != 1 {
			t.Errorf("ExitCode = %d; want 1 (error %v)", code, result.err)
		}
		if _, err := os.Stat(result.output); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("output stat error = %v; want the output not created", err)
		}
	})
	t.Run("personal file with version 2", func(t *testing.T) {
		config, _ := testConfig(t)
		config.Getenv = fixtureEnvironment(carveoutWithCredentials(map[string]string{}))
		personalPolicyFixture(t, homeOf(t, config), 2, nil)
		result := carveoutRunConsult(t, config, nil)
		if result.err == nil {
			t.Fatal("consult error = nil; want an error for a version-2 personal policy")
		}
		text := result.err.Error() + "\n" + result.stderr
		const want = "model policy version 2 is not supported; this cw requires version 3"
		if !strings.Contains(text, want) {
			t.Errorf("error and stderr = %q; want text containing %q", text, want)
		}
	})
}

const carveoutPlan = `{"title":"t","kind":"small","requirements":[{"id":"r","description":"d","allow_non_execution":true}],"checks":[]}`

// carveoutTaskStartAdvisers runs `task start --plan - --store S` and returns
// the advisers member of its JSON output.
func carveoutTaskStartAdvisers(t *testing.T, config Config) map[string]string {
	t.Helper()
	work := t.TempDir()
	if err := os.WriteFile(filepath.Join(work, "README.txt"), []byte("fixture\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	config.WorkingDir = func() (string, error) { return work, nil }
	config.In = strings.NewReader(carveoutPlan)
	var out, stderr bytes.Buffer
	config.Out = &out
	config.Err = &stderr
	args := []string{"--config-dir", carveoutOptedInDir(t), "task", "start", "--plan", "-", "--store", carveoutPrivateDir(t)}
	if err := Execute(context.Background(), args, config); err != nil {
		t.Fatalf("task start error = %v; stderr: %s", err, stderr.String())
	}
	decoder := json.NewDecoder(bytes.NewReader(out.Bytes()))
	for {
		var document map[string]json.RawMessage
		if err := decoder.Decode(&document); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			t.Fatalf("task start output is not JSON objects: %v\n%s", err, out.String())
		}
		raw, ok := document["advisers"]
		if !ok {
			continue
		}
		advisers := map[string]string{}
		if err := json.Unmarshal(raw, &advisers); err != nil {
			t.Fatalf("advisers member %s: %v", raw, err)
		}
		return advisers
	}
	t.Fatalf("task start output has no advisers member:\n%s", out.String())
	return nil
}

// PR-3: `cw task start` binds advisers from the selected policy.
func TestCarveoutTaskStartPolicySelection(t *testing.T) {
	t.Run("embedded default", func(t *testing.T) {
		config, _ := testConfig(t)
		config.Getenv = fixtureEnvironment(carveoutWithCredentials(map[string]string{}))
		got := carveoutTaskStartAdvisers(t, config)
		want := map[string]string{"subq_snap": "subq-snap-preview", "openai_decisions": "gpt-6-luna"}
		if !maps.Equal(got, want) {
			t.Errorf("advisers = %v; want %v", got, want)
		}
	})
	t.Run("environment policy", func(t *testing.T) {
		config, _ := testConfig(t)
		env := carveoutWithCredentials(map[string]string{})
		env["CW_MODEL_POLICY"] = writePolicyFixture(t, t.TempDir(), 3, []fixturePin{pinJev})
		config.Getenv = fixtureEnvironment(env)
		got := carveoutTaskStartAdvisers(t, config)
		want := map[string]string{"jev": "jev-1.14.0"}
		if !maps.Equal(got, want) {
			t.Errorf("advisers = %v; want %v", got, want)
		}
	})
	t.Run("personal policy", func(t *testing.T) {
		config, _ := testConfig(t)
		config.Getenv = fixtureEnvironment(carveoutWithCredentials(map[string]string{}))
		personalPolicyFixture(t, homeOf(t, config), 3, []fixturePin{pinSnap})
		got := carveoutTaskStartAdvisers(t, config)
		want := map[string]string{"subq_snap": "subq-snap-preview"}
		if !maps.Equal(got, want) {
			t.Errorf("advisers = %v; want %v", got, want)
		}
	})
}
