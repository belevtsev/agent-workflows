package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/belevtsev/agent-workflows/internal/credentials"
)

type carveoutKeysResult struct {
	err         error
	out, stderr string
}

// text is everything the command reported: its error, standard output and
// standard error.
func (r carveoutKeysResult) text() string {
	text := r.out + "\n" + r.stderr
	if r.err != nil {
		text = r.err.Error() + "\n" + text
	}
	return text
}

// carveoutKeysDir returns a configuration directory path, not yet created,
// whose parent has no symlink component.
func carveoutKeysDir(t *testing.T) string {
	t.Helper()
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Join(base, "config")
}

// carveoutKeysConfig returns a test Config whose environment serves env.
func carveoutKeysConfig(t *testing.T, env map[string]string) Config {
	t.Helper()
	config, _, _, _ := testConfig(t)
	if env == nil {
		env = map[string]string{}
	}
	config.Getenv = fixtureEnvironment(env)
	return config
}

// carveoutKeys runs args with stdin and requires that none of secrets appears
// in standard output, standard error or the returned error (K-1).
func carveoutKeys(t *testing.T, config Config, stdin string, secrets []string, args ...string) carveoutKeysResult {
	t.Helper()
	var out, stderr bytes.Buffer
	config.In = strings.NewReader(stdin)
	config.Out = &out
	config.Err = &stderr
	err := Execute(context.Background(), args, config)
	result := carveoutKeysResult{err: err, out: out.String(), stderr: stderr.String()}
	for _, secret := range secrets {
		if secret == "" {
			continue
		}
		if strings.Contains(result.out, secret) {
			t.Errorf("standard output of %q contains the key value", args)
		}
		if strings.Contains(result.stderr, secret) {
			t.Errorf("standard error of %q contains the key value", args)
		}
		if err != nil && strings.Contains(err.Error(), secret) {
			t.Errorf("error of %q contains the key value", args)
		}
	}
	return result
}

// carveoutJSONLine parses out as a single JSON object on one line.
func carveoutJSONLine(t *testing.T, out string) map[string]any {
	t.Helper()
	trimmed := strings.TrimRight(out, "\n")
	if strings.Contains(trimmed, "\n") {
		t.Fatalf("output is not a single line:\n%s", out)
	}
	var document map[string]any
	if err := json.Unmarshal([]byte(trimmed), &document); err != nil {
		t.Fatalf("output is not a JSON object: %v\n%s", err, out)
	}
	return document
}

func carveoutRequireOK(t *testing.T, result carveoutKeysResult) {
	t.Helper()
	if result.err != nil {
		t.Fatalf("error = %v; stderr: %s", result.err, result.stderr)
	}
	if code := ExitCode(result.err); code != 0 {
		t.Fatalf("ExitCode = %d; want 0", code)
	}
}

func carveoutRequireCode(t *testing.T, result carveoutKeysResult, code int) {
	t.Helper()
	if result.err == nil {
		t.Fatalf("error = nil (output %q); want ExitCode %d", result.out, code)
	}
	if got := ExitCode(result.err); got != code {
		t.Fatalf("ExitCode = %d (error %v); want %d", got, result.err, code)
	}
}

// carveoutStored returns the saved keys of the store in dir.
func carveoutStored(t *testing.T, dir string) map[string]string {
	t.Helper()
	snapshot, err := credentials.Store{Directory: dir}.Read()
	if err != nil {
		t.Fatalf("credentials.Store{%q}.Read() error = %v", dir, err)
	}
	return snapshot.Keys
}

// carveoutRequireNothingSaved requires that dir holds no saved keys and no
// credentials.json.
func carveoutRequireNothingSaved(t *testing.T, dir string) {
	t.Helper()
	if _, err := os.Lstat(filepath.Join(dir, "credentials.json")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("credentials.json stat error = %v; want it absent", err)
	}
	if keys := carveoutStored(t, dir); len(keys) != 0 {
		t.Errorf("saved key names = %v; want none", carveoutKeyNames(keys))
	}
}

func carveoutKeyNames(keys map[string]string) []string {
	names := make([]string, 0, len(keys))
	for name := range keys {
		names = append(names, name)
	}
	return names
}

func carveoutRequireMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Errorf("mode of %s = %#o; want %#o", path, got, want)
	}
}

func carveoutWant(action, name, status string, committed bool) map[string]any {
	return map[string]any{
		"action": action, "committed": committed, "durability_verified": true, "name": name, "status": status,
	}
}

// K-1: set from standard input.
func TestCarveoutKeysSetFromStdin(t *testing.T) {
	for _, tc := range []struct{ name, stdin string }{
		{"lf", "sk-test-123\n"},
		{"crlf", "sk-test-123\r\n"},
		{"no newline", "sk-test-123"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := carveoutKeysDir(t)
			config := carveoutKeysConfig(t, nil)
			result := carveoutKeys(t, config, tc.stdin, []string{"sk-test-123"}, "keys", "set", "OPENAI_API_KEY", "--config-dir", dir)
			carveoutRequireOK(t, result)
			got := carveoutJSONLine(t, result.out)
			if want := carveoutWant("set", "OPENAI_API_KEY", "saved", true); !reflect.DeepEqual(got, want) {
				t.Errorf("output = %v; want %v", got, want)
			}
			if value := carveoutStored(t, dir)["OPENAI_API_KEY"]; value != "sk-test-123" {
				t.Errorf("saved OPENAI_API_KEY has %d bytes; want the value sk-test-123", len(value))
			}
			carveoutRequireMode(t, dir, 0o700)
			carveoutRequireMode(t, filepath.Join(dir, "credentials.json"), 0o600)
		})
	}
}

// K-2: set from a named environment variable.
func TestCarveoutKeysSetFromEnv(t *testing.T) {
	t.Run("saved and stdin ignored", func(t *testing.T) {
		dir := carveoutKeysDir(t)
		config := carveoutKeysConfig(t, map[string]string{"MY_SUBQ": "sq-456"})
		result := carveoutKeys(t, config, "stdin-unused-9\n", []string{"sq-456", "stdin-unused-9"},
			"keys", "set", "SUBQ_API_KEY", "--from-env", "MY_SUBQ", "--config-dir", dir)
		carveoutRequireOK(t, result)
		got := carveoutJSONLine(t, result.out)
		if want := carveoutWant("set", "SUBQ_API_KEY", "saved", true); !reflect.DeepEqual(got, want) {
			t.Errorf("output = %v; want %v", got, want)
		}
		if value := carveoutStored(t, dir)["SUBQ_API_KEY"]; value != "sq-456" {
			t.Errorf("saved SUBQ_API_KEY is not the MY_SUBQ value (%d bytes)", len(value))
		}
	})
	t.Run("empty variable", func(t *testing.T) {
		dir := carveoutKeysDir(t)
		config := carveoutKeysConfig(t, nil)
		result := carveoutKeys(t, config, "stdin-unused-9\n", []string{"stdin-unused-9"},
			"keys", "set", "SUBQ_API_KEY", "--from-env", "MY_SUBQ", "--config-dir", dir)
		carveoutRequireCode(t, result, 1)
		if !strings.Contains(result.err.Error(), "MY_SUBQ") {
			t.Errorf("error = %q; want text naming MY_SUBQ", result.err.Error())
		}
		carveoutRequireNothingSaved(t, dir)
	})
}

// K-3: key values never come from argv; usage errors.
func TestCarveoutKeysUsage(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{"value in argv", []string{"keys", "set", "OPENAI_API_KEY", "sk-123"}, ""},
		{"set without name", []string{"keys", "set"}, ""},
		{"unknown name", []string{"keys", "set", "GITHUB_TOKEN"}, "TYPESAFE_API_KEY, SUBQ_API_KEY, OPENAI_API_KEY"},
		{"unset without name", []string{"keys", "unset"}, ""},
		{"unset unknown name", []string{"keys", "unset", "FOO"}, ""},
		{"status extra argument", []string{"keys", "status", "extra"}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := carveoutKeysDir(t)
			config := carveoutKeysConfig(t, nil)
			args := append(append([]string{}, tc.args...), "--config-dir", dir)
			result := carveoutKeys(t, config, "stdin-value-1\n", []string{"sk-123", "stdin-value-1"}, args...)
			carveoutRequireCode(t, result, 2)
			if tc.want != "" && !strings.Contains(result.err.Error()+"\n"+result.stderr, tc.want) {
				t.Errorf("error and stderr = %q; want text containing %q", result.err.Error()+"\n"+result.stderr, tc.want)
			}
			carveoutRequireNothingSaved(t, dir)
		})
	}
	t.Run("keys alone shows help", func(t *testing.T) {
		config := carveoutKeysConfig(t, nil)
		result := carveoutKeys(t, config, "", nil, "keys")
		if result.err != nil {
			t.Fatalf("keys error = %v; want nil", result.err)
		}
	})
}

// K-4: invalid key values.
func TestCarveoutKeysInvalidInput(t *testing.T) {
	long := strings.Repeat("a", 64*1024+1)
	for _, tc := range []struct {
		name, stdin, want string
	}{
		{"empty", "", "no key was read from standard input"},
		{"newline only", "\n", "no key was read from standard input"},
		{"inner space", "ab cd\n", ""},
		{"tab", "ab\tcd\n", ""},
		{"control character", "ab\x01cd\n", ""},
		{"escape character", "ab\x1bcd\n", ""},
		{"longer than 64 KiB", long + "\n", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := carveoutKeysDir(t)
			config := carveoutKeysConfig(t, nil)
			secret := strings.TrimSuffix(tc.stdin, "\n")
			result := carveoutKeys(t, config, tc.stdin, []string{secret}, "keys", "set", "OPENAI_API_KEY", "--config-dir", dir)
			carveoutRequireCode(t, result, 1)
			if tc.want != "" && !strings.Contains(result.err.Error(), tc.want) {
				t.Errorf("error = %q; want text containing %q", result.err.Error(), tc.want)
			}
			carveoutRequireNothingSaved(t, dir)
		})
	}
	for _, tc := range []struct{ name, value string }{
		{"from-env inner space", "ab cd"},
		{"from-env tab", "ab\tcd"},
		{"from-env longer than 64 KiB", long},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := carveoutKeysDir(t)
			config := carveoutKeysConfig(t, map[string]string{"MY_KEY": tc.value})
			result := carveoutKeys(t, config, "", []string{tc.value},
				"keys", "set", "OPENAI_API_KEY", "--from-env", "MY_KEY", "--config-dir", dir)
			carveoutRequireCode(t, result, 1)
			carveoutRequireNothingSaved(t, dir)
		})
	}
	t.Run("64 KiB then a second line", func(t *testing.T) {
		dir := carveoutKeysDir(t)
		config := carveoutKeysConfig(t, nil)
		exact := strings.Repeat("a", 64*1024)
		result := carveoutKeys(t, config, exact+"\r\nSECOND\n", []string{exact, "SECOND"},
			"keys", "set", "OPENAI_API_KEY", "--config-dir", dir)
		carveoutRequireCode(t, result, 1)
		carveoutRequireNothingSaved(t, dir)
	})
	t.Run("exactly 64 KiB is saved", func(t *testing.T) {
		dir := carveoutKeysDir(t)
		config := carveoutKeysConfig(t, nil)
		exact := strings.Repeat("a", 64*1024)
		result := carveoutKeys(t, config, exact+"\n", []string{exact},
			"keys", "set", "OPENAI_API_KEY", "--config-dir", dir)
		carveoutRequireOK(t, result)
		if value := carveoutStored(t, dir)["OPENAI_API_KEY"]; value != exact {
			t.Errorf("saved OPENAI_API_KEY has %d bytes; want the 65536 input bytes", len(value))
		}
	})
}

// K-5: setting the same value is idempotent; a new value replaces it.
func TestCarveoutKeysSetIdempotent(t *testing.T) {
	dir := carveoutKeysDir(t)
	config := carveoutKeysConfig(t, nil)
	secrets := []string{"value-one-1", "value-two-2"}
	carveoutRequireOK(t, carveoutKeys(t, config, "value-one-1\n", secrets, "keys", "set", "TYPESAFE_API_KEY", "--config-dir", dir))

	again := carveoutKeys(t, config, "value-one-1\n", secrets, "keys", "set", "TYPESAFE_API_KEY", "--config-dir", dir)
	carveoutRequireOK(t, again)
	if got, want := carveoutJSONLine(t, again.out), carveoutWant("set", "TYPESAFE_API_KEY", "unchanged", false); !reflect.DeepEqual(got, want) {
		t.Errorf("second set output = %v; want %v", got, want)
	}

	replaced := carveoutKeys(t, config, "value-two-2\n", secrets, "keys", "set", "TYPESAFE_API_KEY", "--config-dir", dir)
	carveoutRequireOK(t, replaced)
	if got, want := carveoutJSONLine(t, replaced.out), carveoutWant("set", "TYPESAFE_API_KEY", "saved", true); !reflect.DeepEqual(got, want) {
		t.Errorf("replacing set output = %v; want %v", got, want)
	}
	if value := carveoutStored(t, dir)["TYPESAFE_API_KEY"]; value != "value-two-2" {
		t.Errorf("saved TYPESAFE_API_KEY is not the replacement value (%d bytes)", len(value))
	}
}

// K-6: unset.
func TestCarveoutKeysUnset(t *testing.T) {
	dir := carveoutKeysDir(t)
	config := carveoutKeysConfig(t, nil)
	secrets := []string{"sk-test-123", "sq-keep-77"}
	carveoutRequireOK(t, carveoutKeys(t, config, "sk-test-123\n", secrets, "keys", "set", "OPENAI_API_KEY", "--config-dir", dir))
	carveoutRequireOK(t, carveoutKeys(t, config, "sq-keep-77\n", secrets, "keys", "set", "SUBQ_API_KEY", "--config-dir", dir))

	first := carveoutKeys(t, config, "", secrets, "keys", "unset", "OPENAI_API_KEY", "--config-dir", dir)
	carveoutRequireOK(t, first)
	if got, want := carveoutJSONLine(t, first.out), carveoutWant("unset", "OPENAI_API_KEY", "removed", true); !reflect.DeepEqual(got, want) {
		t.Errorf("unset output = %v; want %v", got, want)
	}
	stored := carveoutStored(t, dir)
	if _, ok := stored["OPENAI_API_KEY"]; ok {
		t.Error("OPENAI_API_KEY is still saved after unset")
	}
	if stored["SUBQ_API_KEY"] != "sq-keep-77" {
		t.Error("unsetting OPENAI_API_KEY did not keep the saved SUBQ_API_KEY")
	}

	second := carveoutKeys(t, config, "", secrets, "keys", "unset", "OPENAI_API_KEY", "--config-dir", dir)
	carveoutRequireOK(t, second)
	got := carveoutJSONLine(t, second.out)
	if got["status"] != "unchanged" || got["committed"] != false {
		t.Errorf("second unset output = %v; want status unchanged, committed false", got)
	}
	if got["action"] != "unset" || got["name"] != "OPENAI_API_KEY" {
		t.Errorf("second unset output = %v; want action unset, name OPENAI_API_KEY", got)
	}
}

type carveoutStatus struct {
	Directory string                          `json:"directory"`
	Keys      map[string]credentials.Presence `json:"keys"`
}

func carveoutParseStatus(t *testing.T, out string) carveoutStatus {
	t.Helper()
	carveoutJSONLine(t, out)
	var status carveoutStatus
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &status); err != nil {
		t.Fatalf("status output: %v\n%s", err, out)
	}
	if len(status.Keys) != len(credentials.Names) {
		t.Errorf("status keys = %v; want exactly %v", status.Keys, credentials.Names)
	}
	for _, name := range credentials.Names {
		if _, ok := status.Keys[name]; !ok {
			t.Errorf("status keys lack %s", name)
		}
	}
	return status
}

var carveoutNone = credentials.Presence{Source: "none"}

// K-7: status.
func TestCarveoutKeysStatus(t *testing.T) {
	for _, dryRun := range []bool{false, true} {
		name := "missing directory"
		if dryRun {
			name += " with dry run"
		}
		t.Run(name, func(t *testing.T) {
			dir := carveoutKeysDir(t)
			config := carveoutKeysConfig(t, nil)
			args := []string{"keys", "status", "--config-dir", dir}
			if dryRun {
				args = append([]string{"--dry-run"}, args...)
			}
			result := carveoutKeys(t, config, "", nil, args...)
			carveoutRequireOK(t, result)
			status := carveoutParseStatus(t, result.out)
			if status.Directory != dir {
				t.Errorf("directory = %q; want %q", status.Directory, dir)
			}
			for _, key := range credentials.Names {
				if got := status.Keys[key]; got != carveoutNone {
					t.Errorf("%s = %+v; want %+v", key, got, carveoutNone)
				}
			}
		})
	}
	t.Run("after set", func(t *testing.T) {
		dir := carveoutKeysDir(t)
		config := carveoutKeysConfig(t, nil)
		secrets := []string{"sk-status-1"}
		carveoutRequireOK(t, carveoutKeys(t, config, "sk-status-1\n", secrets, "keys", "set", "OPENAI_API_KEY", "--config-dir", dir))
		result := carveoutKeys(t, config, "", secrets, "keys", "status", "--config-dir", dir)
		carveoutRequireOK(t, result)
		status := carveoutParseStatus(t, result.out)
		want := credentials.Presence{Stored: true, Effective: true, Source: "stored"}
		if got := status.Keys["OPENAI_API_KEY"]; got != want {
			t.Errorf("OPENAI_API_KEY = %+v; want %+v", got, want)
		}
		for _, key := range []string{"TYPESAFE_API_KEY", "SUBQ_API_KEY"} {
			if got := status.Keys[key]; got != carveoutNone {
				t.Errorf("%s = %+v; want %+v", key, got, carveoutNone)
			}
		}
	})
	t.Run("environment and stored", func(t *testing.T) {
		dir := carveoutKeysDir(t)
		env := map[string]string{}
		config := carveoutKeysConfig(t, env)
		secrets := []string{"sk-stored-2", "sk-env-3"}
		carveoutRequireOK(t, carveoutKeys(t, config, "sk-stored-2\n", secrets, "keys", "set", "OPENAI_API_KEY", "--config-dir", dir))
		env["OPENAI_API_KEY"] = "sk-env-3"
		result := carveoutKeys(t, config, "", secrets, "keys", "status", "--config-dir", dir)
		carveoutRequireOK(t, result)
		status := carveoutParseStatus(t, result.out)
		want := credentials.Presence{Environment: true, Stored: true, Effective: true, Source: "environment"}
		if got := status.Keys["OPENAI_API_KEY"]; got != want {
			t.Errorf("OPENAI_API_KEY = %+v; want %+v", got, want)
		}
	})
	t.Run("temp directory alias reported resolved", func(t *testing.T) {
		parent := t.TempDir()
		resolvedParent, err := filepath.EvalSymlinks(parent)
		if err != nil {
			t.Fatal(err)
		}
		dir := filepath.Join(parent, "config")
		config := carveoutKeysConfig(t, nil)
		result := carveoutKeys(t, config, "", nil, "keys", "status", "--config-dir", dir)
		carveoutRequireOK(t, result)
		status := carveoutParseStatus(t, result.out)
		if want := filepath.Join(resolvedParent, "config"); status.Directory != want {
			t.Errorf("directory = %q; want %q", status.Directory, want)
		}
	})
}

// K-8: dry run refuses changes.
func TestCarveoutKeysDryRun(t *testing.T) {
	t.Run("set", func(t *testing.T) {
		dir := carveoutKeysDir(t)
		config := carveoutKeysConfig(t, nil)
		result := carveoutKeys(t, config, "x-dry-value\n", []string{"x-dry-value"},
			"--dry-run", "keys", "set", "OPENAI_API_KEY", "--config-dir", dir)
		carveoutRequireCode(t, result, 2)
		carveoutRequireNothingSaved(t, dir)
		if _, err := os.Lstat(dir); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("configuration directory stat error = %v; want it not created", err)
		}
	})
	t.Run("unset", func(t *testing.T) {
		dir := carveoutKeysDir(t)
		config := carveoutKeysConfig(t, nil)
		secrets := []string{"sk-dry-keep"}
		carveoutRequireOK(t, carveoutKeys(t, config, "sk-dry-keep\n", secrets, "keys", "set", "OPENAI_API_KEY", "--config-dir", dir))
		result := carveoutKeys(t, config, "", secrets, "--dry-run", "keys", "unset", "OPENAI_API_KEY", "--config-dir", dir)
		carveoutRequireCode(t, result, 2)
		if carveoutStored(t, dir)["OPENAI_API_KEY"] != "sk-dry-keep" {
			t.Error("dry-run unset changed the saved OPENAI_API_KEY")
		}
	})
}

// K-9: configuration directory resolution without --config-dir.
func TestCarveoutKeysDirectoryResolution(t *testing.T) {
	t.Run("XDG_CONFIG_HOME", func(t *testing.T) {
		xdg := t.TempDir()
		config := carveoutKeysConfig(t, map[string]string{"XDG_CONFIG_HOME": xdg})
		carveoutRequireOK(t, carveoutKeys(t, config, "ts-xdg-1\n", []string{"ts-xdg-1"}, "keys", "set", "TYPESAFE_API_KEY"))
		dir := filepath.Join(xdg, "agent-workflows")
		if _, err := os.Stat(filepath.Join(dir, "credentials.json")); err != nil {
			t.Fatalf("credentials.json under XDG_CONFIG_HOME: %v", err)
		}
		carveoutRequireMode(t, dir, 0o700)
		resolved, err := filepath.EvalSymlinks(dir)
		if err != nil {
			t.Fatal(err)
		}
		if carveoutStored(t, resolved)["TYPESAFE_API_KEY"] != "ts-xdg-1" {
			t.Error("TYPESAFE_API_KEY was not saved under XDG_CONFIG_HOME/agent-workflows")
		}
	})
	t.Run("home", func(t *testing.T) {
		config := carveoutKeysConfig(t, nil)
		home := homeOf(t, config)
		carveoutRequireOK(t, carveoutKeys(t, config, "ts-home-1\n", []string{"ts-home-1"}, "keys", "set", "TYPESAFE_API_KEY"))
		dir := filepath.Join(home, ".config", "agent-workflows")
		if _, err := os.Stat(filepath.Join(dir, "credentials.json")); err != nil {
			t.Fatalf("credentials.json under the home configuration directory: %v", err)
		}
		carveoutRequireMode(t, filepath.Join(home, ".config"), 0o700)
		carveoutRequireMode(t, dir, 0o700)
		resolved, err := filepath.EvalSymlinks(dir)
		if err != nil {
			t.Fatal(err)
		}
		if carveoutStored(t, resolved)["TYPESAFE_API_KEY"] != "ts-home-1" {
			t.Error("TYPESAFE_API_KEY was not saved under <home>/.config/agent-workflows")
		}
	})
	for _, tc := range []struct{ name, suffix string }{
		{"home flag with trailing slash", "/"},
		{"home flag without trailing slash", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			config := carveoutKeysConfig(t, nil)
			home := homeOf(t, config)
			carveoutRequireOK(t, carveoutKeys(t, config, "ts-flag-1\n", []string{"ts-flag-1"},
				"--home", home+tc.suffix, "keys", "set", "TYPESAFE_API_KEY"))
			dir := filepath.Join(home, ".config", "agent-workflows")
			if _, err := os.Stat(filepath.Join(dir, "credentials.json")); err != nil {
				t.Fatalf("credentials.json under <home>/.config/agent-workflows: %v", err)
			}
			resolved, err := filepath.EvalSymlinks(dir)
			if err != nil {
				t.Fatal(err)
			}
			if carveoutStored(t, resolved)["TYPESAFE_API_KEY"] != "ts-flag-1" {
				t.Error("TYPESAFE_API_KEY was not saved under <home>/.config/agent-workflows")
			}
		})
	}
	t.Run("home flag naming another directory", func(t *testing.T) {
		config := carveoutKeysConfig(t, nil)
		defaultHome := homeOf(t, config)
		home, err := filepath.EvalSymlinks(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		if home == defaultHome {
			t.Fatalf("--home %q must differ from HomeDir()", home)
		}
		carveoutRequireOK(t, carveoutKeys(t, config, "ts-other-1\n", []string{"ts-other-1"},
			"--home", home, "keys", "set", "TYPESAFE_API_KEY"))
		dir := filepath.Join(home, ".config", "agent-workflows")
		if _, err := os.Stat(filepath.Join(dir, "credentials.json")); err != nil {
			t.Fatalf("credentials.json under <--home>/.config/agent-workflows: %v", err)
		}
		if carveoutStored(t, dir)["TYPESAFE_API_KEY"] != "ts-other-1" {
			t.Error("TYPESAFE_API_KEY was not saved under <--home>/.config/agent-workflows")
		}
		if _, err := os.Lstat(filepath.Join(defaultHome, ".config")); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("<HomeDir()>/.config stat error = %v; want it absent", err)
		}
	})
}

// K-10: an unsafe existing directory is refused.
func TestCarveoutKeysUnsafeStore(t *testing.T) {
	for _, tc := range []struct {
		name  string
		args  []string
		stdin string
	}{
		{"set", []string{"keys", "set", "OPENAI_API_KEY"}, "sk-unsafe-1\n"},
		{"status", []string{"keys", "status"}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := carveoutKeysDir(t)
			if err := os.Mkdir(dir, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(dir, 0o755); err != nil {
				t.Fatal(err)
			}
			config := carveoutKeysConfig(t, nil)
			args := append(append([]string{}, tc.args...), "--config-dir", dir)
			result := carveoutKeys(t, config, tc.stdin, []string{"sk-unsafe-1"}, args...)
			carveoutRequireCode(t, result, 1)
			if !strings.HasPrefix(result.err.Error(), "credential storage is unsafe") {
				t.Errorf("error = %q; want text starting with %q", result.err.Error(), "credential storage is unsafe")
			}
			if _, err := os.Lstat(filepath.Join(dir, "credentials.json")); !errors.Is(err, os.ErrNotExist) {
				t.Errorf("credentials.json stat error = %v; want it absent", err)
			}
		})
	}
}
