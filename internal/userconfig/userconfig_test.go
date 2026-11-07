package userconfig_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/belevtsev/agent-workflows/internal/userconfig"
)

func noEnv(string) string { return "" }

func envWith(name, value string) func(string) string {
	return func(key string) string {
		if key == name {
			return value
		}
		return ""
	}
}

func requireErrorContains(t *testing.T, err error, want string) {
	t.Helper()
	if err == nil {
		t.Fatalf("error = nil; want text containing %q", want)
	}
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("error = %q; want text containing %q", err.Error(), want)
	}
}

// UC-1: precedence of explicit, XDG_CONFIG_HOME and home.
func TestDirPrecedence(t *testing.T) {
	xdg := envWith("XDG_CONFIG_HOME", "/xdg")
	for _, tc := range []struct {
		name, home, explicit string
		getenv               func(string) string
		want                 string
	}{
		{"explicit wins", "/home/u", "/explicit", xdg, "/explicit"},
		{"xdg", "/home/u", "", xdg, "/xdg/agent-workflows"},
		{"home", "/home/u", "", noEnv, "/home/u/.config/agent-workflows"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := userconfig.Dir(tc.home, tc.explicit, tc.getenv)
			if err != nil {
				t.Fatalf("Dir error = %v", err)
			}
			if got != tc.want {
				t.Errorf("Dir(%q, %q) = %q; want %q", tc.home, tc.explicit, got, tc.want)
			}
		})
	}
	t.Run("nil getenv reads the process environment", func(t *testing.T) {
		t.Setenv("XDG_CONFIG_HOME", "/xdg-process")
		got, err := userconfig.Dir("/home/u", "", nil)
		if err != nil {
			t.Fatalf("Dir error = %v", err)
		}
		if got != "/xdg-process/agent-workflows" {
			t.Errorf("Dir with nil getenv = %q; want %q", got, "/xdg-process/agent-workflows")
		}
	})
}

// UC-2: path validation.
func TestDirValidation(t *testing.T) {
	const explicitErr = "explicit configuration directory must be an absolute clean path"
	for _, explicit := range []string{"relative/dir", "/a/../b", "/a/", "/a\nb"} {
		t.Run("explicit "+strings.ReplaceAll(explicit, "\n", `\n`), func(t *testing.T) {
			_, err := userconfig.Dir("/home/u", explicit, noEnv)
			requireErrorContains(t, err, explicitErr)
		})
	}
	t.Run("relative XDG_CONFIG_HOME", func(t *testing.T) {
		_, err := userconfig.Dir("/home/u", "", envWith("XDG_CONFIG_HOME", "relative"))
		requireErrorContains(t, err, "XDG_CONFIG_HOME must be an absolute clean path")
	})
	for _, home := range []string{"relative", ""} {
		t.Run("home "+home, func(t *testing.T) {
			_, err := userconfig.Dir(home, "", noEnv)
			requireErrorContains(t, err, "home directory must be an absolute clean path")
		})
	}
}

// UC-3: the macOS /tmp and /var aliases resolve to /private.
func TestDirMacOSAlias(t *testing.T) {
	if target, err := os.Readlink("/tmp"); err != nil || target != "private/tmp" {
		t.Skip("/tmp is not a symlink to private/tmp on this system")
	}
	t.Run("home under /tmp", func(t *testing.T) {
		got, err := userconfig.Dir("/tmp/h", "", noEnv)
		if err != nil {
			t.Fatal(err)
		}
		if want := "/private/tmp/h/.config/agent-workflows"; got != want {
			t.Errorf("Dir = %q; want %q", got, want)
		}
	})
	t.Run("explicit under /tmp", func(t *testing.T) {
		got, err := userconfig.Dir("", "/tmp/x", noEnv)
		if err != nil {
			t.Fatal(err)
		}
		if want := "/private/tmp/x"; got != want {
			t.Errorf("Dir = %q; want %q", got, want)
		}
	})
	t.Run("XDG under /var", func(t *testing.T) {
		if target, err := os.Readlink("/var"); err != nil || target != "private/var" {
			t.Skip("/var is not a symlink to private/var on this system")
		}
		got, err := userconfig.Dir("/home/u", "", envWith("XDG_CONFIG_HOME", "/var/y"))
		if err != nil {
			t.Fatal(err)
		}
		if want := "/private/var/y/agent-workflows"; got != want {
			t.Errorf("Dir = %q; want %q", got, want)
		}
	})
	t.Run("/tmpfoo is not rewritten", func(t *testing.T) {
		got, err := userconfig.Dir("", "/tmpfoo/x", noEnv)
		if err != nil {
			t.Fatal(err)
		}
		if want := "/tmpfoo/x"; got != want {
			t.Errorf("Dir = %q; want %q", got, want)
		}
	})
}

// resolvedTemp returns t.TempDir() with every symlink component resolved.
func resolvedTemp(t *testing.T) string {
	t.Helper()
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return base
}

// mkdirMode creates dir with exactly mode.
func mkdirMode(t *testing.T, dir string, mode os.FileMode) {
	t.Helper()
	if err := os.Mkdir(dir, mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, mode); err != nil {
		t.Fatal(err)
	}
}

// writeMode writes path with exactly mode.
func writeMode(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}

// privateDir returns a fresh 0700 directory without symlink components.
func privateDir(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(resolvedTemp(t), "config")
	mkdirMode(t, dir, 0o700)
	return dir
}

// privateDirWith returns a private directory holding a 0600 preferences.json.
func privateDirWith(t *testing.T, content string) string {
	t.Helper()
	dir := privateDir(t)
	writeMode(t, filepath.Join(dir, "preferences.json"), content, 0o600)
	return dir
}

const validPreferences = `{"version":1,"advisers":{"disabled":["jev"]}}`

// UC-4: no preferences file.
func TestDisabledAdvisersNoFile(t *testing.T) {
	requireNone := func(t *testing.T, dir string) {
		t.Helper()
		got, err := userconfig.DisabledAdvisers(dir)
		if err != nil || got != nil {
			t.Fatalf("DisabledAdvisers(%q) = (%q, %v); want (nil, nil)", dir, got, err)
		}
	}
	t.Run("missing directory", func(t *testing.T) {
		requireNone(t, filepath.Join(resolvedTemp(t), "missing"))
	})
	t.Run("empty private directory", func(t *testing.T) {
		requireNone(t, privateDir(t))
	})
	t.Run("directory mode 0755", func(t *testing.T) {
		dir := filepath.Join(resolvedTemp(t), "open")
		mkdirMode(t, dir, 0o755)
		requireNone(t, dir)
	})
	t.Run("symlink to directory", func(t *testing.T) {
		base := resolvedTemp(t)
		real := filepath.Join(base, "real")
		mkdirMode(t, real, 0o700)
		link := filepath.Join(base, "link")
		if err := os.Symlink(real, link); err != nil {
			t.Fatal(err)
		}
		requireNone(t, link)
	})
	for _, dir := range []string{"relative/dir", "/a/../b"} {
		t.Run("invalid path "+dir, func(t *testing.T) {
			_, err := userconfig.DisabledAdvisers(dir)
			requireErrorContains(t, err, "private configuration requires an absolute clean path")
		})
	}
}

// UC-5: valid documents.
func TestDisabledAdvisersValid(t *testing.T) {
	for _, tc := range []struct {
		name, document string
		want           []string
	}{
		{"version only", `{"version":1}`, nil},
		{"empty advisers", `{"version":1,"advisers":{}}`, nil},
		{"empty disabled", `{"version":1,"advisers":{"disabled":[]}}`, nil},
		{"null advisers", `{"version":1,"advisers":null}`, nil},
		{"one", `{"version":1,"advisers":{"disabled":["openai_decisions"]}}`, []string{"openai_decisions"}},
		{"two in provider order", `{"version":1,"advisers":{"disabled":["openai_decisions","subq_snap"]}}`,
			[]string{"subq_snap", "openai_decisions"}},
		{"three in provider order", `{"version":1,"advisers":{"disabled":["openai_decisions","jev","subq_snap"]}}`,
			[]string{"jev", "subq_snap", "openai_decisions"}},
		{"other members ignored", `{"version":1,"hosts":{"codex":{}},"shared":{"x":1},"advisers":{"disabled":["jev"]}}`,
			[]string{"jev"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := userconfig.DisabledAdvisers(privateDirWith(t, tc.document))
			if err != nil {
				t.Fatalf("DisabledAdvisers error = %v", err)
			}
			if tc.want == nil {
				if got != nil {
					t.Fatalf("DisabledAdvisers = %#v; want nil", got)
				}
				return
			}
			if !slices.Equal(got, tc.want) {
				t.Errorf("DisabledAdvisers = %q; want %q", got, tc.want)
			}
		})
	}
}

// UC-6: invalid documents.
func TestDisabledAdvisersInvalid(t *testing.T) {
	for _, tc := range []struct {
		name, document, want string
		prefix               bool
	}{
		{"unknown adviser", `{"version":1,"advisers":{"disabled":["gemini_review"]}}`, `unknown adviser "gemini_review" in advisers.disabled`, false},
		{"duplicate", `{"version":1,"advisers":{"disabled":["subq_snap","jev","subq_snap"]}}`, `adviser "subq_snap" is listed twice in advisers.disabled`, false},
		{"wrong case", `{"version":1,"advisers":{"disabled":["JEV"]}}`, "unknown adviser", false},
		{"empty name", `{"version":1,"advisers":{"disabled":[""]}}`, "unknown adviser", false},
		{"unknown advisers member", `{"version":1,"advisers":{"enabled":["jev"]}}`, "invalid preferences", true},
		{"disabled not a list", `{"version":1,"advisers":{"disabled":"jev"}}`, "invalid preferences", true},
		{"truncated", `{"version":1,"advisers":`, "invalid preferences", true},
		{"misspelled top-level member", `{"version":1,"adviser":{"disabled":["subq_snap"]}}`, "invalid preferences", true},
		{"version 2", `{"version":2}`, "preferences require version 1", false},
		{"no version", `{}`, "preferences require version 1", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := userconfig.DisabledAdvisers(privateDirWith(t, tc.document))
			if err == nil {
				t.Fatalf("DisabledAdvisers = %q, nil; want an error", got)
			}
			if tc.prefix {
				if !strings.HasPrefix(err.Error(), tc.want) {
					t.Fatalf("error = %q; want text starting with %q", err.Error(), tc.want)
				}
				return
			}
			requireErrorContains(t, err, tc.want)
		})
	}
}

// UC-7: file and directory rules.
func TestDisabledAdvisersFileRules(t *testing.T) {
	const dirErr = "private configuration directory must be owned by the current user with mode 0700"
	const fileErr = "preferences must be a current-user regular file with mode 0600"

	t.Run("directory mode 0755", func(t *testing.T) {
		dir := filepath.Join(resolvedTemp(t), "open")
		mkdirMode(t, dir, 0o755)
		writeMode(t, filepath.Join(dir, "preferences.json"), validPreferences, 0o600)
		_, err := userconfig.DisabledAdvisers(dir)
		requireErrorContains(t, err, dirErr)
	})
	t.Run("directory through symlink", func(t *testing.T) {
		base := resolvedTemp(t)
		real := filepath.Join(base, "realdir")
		mkdirMode(t, real, 0o700)
		writeMode(t, filepath.Join(real, "preferences.json"), validPreferences, 0o600)
		link := filepath.Join(base, "link")
		if err := os.Symlink(real, link); err != nil {
			t.Fatal(err)
		}
		_, err := userconfig.DisabledAdvisers(link)
		if err == nil || !strings.HasPrefix(err.Error(), "unsafe directory: ") {
			t.Fatalf("error = %v; want text starting with %q", err, "unsafe directory: ")
		}
	})
	t.Run("symlinked ancestor", func(t *testing.T) {
		base := resolvedTemp(t)
		real := filepath.Join(base, "real")
		mkdirMode(t, real, 0o700)
		config := filepath.Join(real, "config")
		mkdirMode(t, config, 0o700)
		writeMode(t, filepath.Join(config, "preferences.json"), validPreferences, 0o600)
		link := filepath.Join(base, "link")
		if err := os.Symlink(real, link); err != nil {
			t.Fatal(err)
		}
		_, err := userconfig.DisabledAdvisers(filepath.Join(link, "config"))
		if err == nil || !strings.HasPrefix(err.Error(), "unsafe directory: ") {
			t.Fatalf("error = %v; want text starting with %q", err, "unsafe directory: ")
		}
	})
	t.Run("file mode 0644", func(t *testing.T) {
		dir := privateDir(t)
		writeMode(t, filepath.Join(dir, "preferences.json"), validPreferences, 0o644)
		_, err := userconfig.DisabledAdvisers(dir)
		requireErrorContains(t, err, fileErr)
	})
	t.Run("file is a directory", func(t *testing.T) {
		dir := privateDir(t)
		mkdirMode(t, filepath.Join(dir, "preferences.json"), 0o700)
		_, err := userconfig.DisabledAdvisers(dir)
		requireErrorContains(t, err, fileErr)
	})
	t.Run("file is a symlink", func(t *testing.T) {
		dir := privateDir(t)
		other := privateDir(t)
		target := filepath.Join(other, "target.json")
		writeMode(t, target, validPreferences, 0o600)
		if err := os.Symlink(target, filepath.Join(dir, "preferences.json")); err != nil {
			t.Fatal(err)
		}
		_, err := userconfig.DisabledAdvisers(dir)
		requireErrorContains(t, err, fileErr)
	})
	t.Run("file larger than 1 MiB", func(t *testing.T) {
		content := validPreferences + strings.Repeat(" ", (1<<20)+1-len(validPreferences))
		if len(content) != (1<<20)+1 {
			t.Fatalf("fixture length = %d", len(content))
		}
		dir := privateDirWith(t, content)
		_, err := userconfig.DisabledAdvisers(dir)
		requireErrorContains(t, err, fileErr)
	})
}
