package workflow

import (
	"bytes"
	"encoding/json/v2"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func aliasTestPaths(t *testing.T) Paths {
	t.Helper()
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	paths := Paths{Source: filepath.Join(base, "editable source ' $dollar"), Home: filepath.Join(base, "home ' with spaces"), Codex: filepath.Join(base, "codex ' home"), State: filepath.Join(base, "state ' dir")}
	for _, path := range []string{paths.Source, paths.Home} {
		if err := os.Mkdir(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(paths.Source, "install.sh"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return paths
}

func aliasTestShellEnvironment(home string) []string {
	// Real-shell fixtures must neither discover the user's cw nor inherit secrets.
	return []string{"HOME=" + home, "ZDOTDIR=" + home, "PATH=/usr/bin:/bin", "LC_ALL=C"}
}

func TestAliasFixtureResolvesSymlinkedTemporaryDirectory(t *testing.T) {
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	real := filepath.Join(base, "real temporary directory")
	alias := filepath.Join(base, "temporary directory alias")
	if err := os.Mkdir(real, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, alias); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TMPDIR", alias)
	t.Run("canonical fixture and strict production paths", func(t *testing.T) {
		paths := aliasTestPaths(t)
		if !strings.HasPrefix(paths.Source, real+string(filepath.Separator)) {
			t.Fatalf("fixture source %q did not resolve temporary alias %q", paths.Source, alias)
		}
		if _, _, _, err := AliasPrepare(paths, "bash", nil); err != nil {
			t.Fatalf("canonical fixture refused: %v", err)
		}
		paths.Source = alias + strings.TrimPrefix(paths.Source, real)
		if _, _, _, err := AliasPrepare(paths, "bash", nil); err == nil {
			t.Fatal("production accepted a source path through a symlinked directory")
		}
	})
}

func aliasTestWrite(t *testing.T, path string, data []byte, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, data, mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}

func aliasTestRead(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func aliasTestApply(t *testing.T, operation Object, paths Paths) {
	t.Helper()
	if operation == nil {
		return
	}
	data, mode, remove, err := AliasRender(operation, paths)
	if err != nil {
		t.Fatal(err)
	}
	path := operation["path"].(string)
	if remove {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
		return
	}
	aliasTestWrite(t, path, data, mode)
}

func TestAliasSelectedShell(t *testing.T) {
	paths := Paths{Home: "/home/fixture"}
	cases := []struct {
		choice        string
		environment   map[string]string
		shell, status string
	}{
		{"auto", nil, "bash", "enrolled"},
		{"auto", map[string]string{"SHELL": "/bin/zsh"}, "zsh", "enrolled"},
		{"auto", map[string]string{"SHELL": "/bin/fish"}, "", "unsupported shell fish; add cw manually"},
		{"none", nil, "", "disabled"},
		{"zsh", map[string]string{"ZDOTDIR": paths.Home}, "zsh", "enrolled"},
		{"zsh", map[string]string{"ZDOTDIR": paths.Home + "/nested"}, "", "ZDOTDIR is set; add cw manually or unset ZDOTDIR for home .zshrc enrollment"},
	}
	for _, test := range cases {
		if shell, status := aliasSelectedShell(test.choice, paths, test.environment); shell != test.shell || status != test.status {
			t.Fatalf("%s %v: got %q, %q", test.choice, test.environment, shell, status)
		}
	}
}

func TestAliasSegmentExactDeployedBytes(t *testing.T) {
	paths := Paths{Source: "/source", Home: "/home", Codex: "/codex", State: "/state"}
	want := "# >>> agent-workflows cw >>>\n" +
		"if command -v cw >/dev/null 2>&1; then\n" +
		"  if [ \"${_AGENT_WORKFLOWS_CW_OWNER-}\" != '/source/install.sh --home /home --codex-home /codex --state-dir /state \"$@\"' ]; then\n" +
		"    printf '%s\\n' 'agent-workflows: cw already exists; keeping current command' >&2\n" +
		"  fi\nelse\n  function cw {\n    /source/install.sh --home /home --codex-home /codex --state-dir /state \"$@\"\n  }\n" +
		"  typeset +x _AGENT_WORKFLOWS_CW_OWNER='/source/install.sh --home /home --codex-home /codex --state-dir /state \"$@\"'\nfi\n" +
		"# <<< agent-workflows cw <<<\n"
	if got := aliasSegment(paths, ""); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	if got := aliasSegment(paths, "\n"); got != "\n"+want {
		t.Fatalf("leading newline mismatch: %q", got)
	}
}

func TestAliasPreservesUnownedBytesAndLaterMode(t *testing.T) {
	paths := aliasTestPaths(t)
	rc := filepath.Join(paths.Home, ".bashrc")
	initial := []byte("# personal credentials\nexport PRIVATE_TOKEN='preserve'\xff")
	aliasTestWrite(t, rc, initial, 0o640)
	metadata, operation, _, err := AliasPrepare(paths, "bash", nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []Object{metadata, operation} {
		data, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(data, []byte("PRIVATE_TOKEN")) {
			t.Fatal("unrelated bytes serialized into ownership")
		}
	}
	aliasTestApply(t, operation, paths)
	want := append(bytes.Clone(initial), []byte(metadata["segment"].(string))...)
	if got := aliasTestRead(t, rc); !bytes.Equal(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
	if err := os.Chmod(rc, 0o604); err != nil {
		t.Fatal(err)
	}
	_, repeat, report, err := AliasPrepare(paths, "zsh", metadata)
	if err != nil || repeat != nil || report["status"] != "preserved" {
		t.Fatalf("repeat: %v, %v, %v", repeat, report, err)
	}
	aliasTestWrite(t, rc, append(aliasTestRead(t, rc), []byte("# independent later\n")...), 0o604)
	removal, err := AliasRemoval(metadata, paths)
	if err != nil {
		t.Fatal(err)
	}
	aliasTestApply(t, removal, paths)
	if got := aliasTestRead(t, rc); !bytes.Equal(got, append(bytes.Clone(initial), []byte("# independent later\n")...)) {
		t.Fatalf("got %q", got)
	}
	info, err := os.Stat(rc)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o604 {
		t.Fatalf("later mode lost: %o", info.Mode().Perm())
	}
}

func TestAliasAbsentAndEmptyOriginsRemainDistinct(t *testing.T) {
	for _, exists := range []bool{false, true} {
		t.Run(map[bool]string{true: "empty", false: "absent"}[exists], func(t *testing.T) {
			paths := aliasTestPaths(t)
			rc := filepath.Join(paths.Home, ".bashrc")
			if exists {
				aliasTestWrite(t, rc, nil, 0o640)
			}
			metadata, operation, _, err := AliasPrepare(paths, "bash", nil)
			if err != nil {
				t.Fatal(err)
			}
			aliasTestApply(t, operation, paths)
			removal, err := AliasRemoval(metadata, paths)
			if err != nil {
				t.Fatal(err)
			}
			aliasTestApply(t, removal, paths)
			info, err := os.Stat(rc)
			if exists {
				if err != nil {
					t.Fatal(err)
				}
				if info.Size() != 0 || info.Mode().Perm() != 0o640 {
					t.Fatalf("empty origin changed: %v", info)
				}
			} else if !os.IsNotExist(err) {
				t.Fatalf("absent origin not removed: %v", err)
			}
		})
	}
}

func TestAliasConflictingDefinitionsAndMarkersRefuse(t *testing.T) {
	definitions := []string{"cw() { :; }\n", "function cw { :; }\n", "alias cw='prior'\n", "alias other='one' cw='prior'\n", "if true; then cw() { :; }; fi\n", "if true; then alias cw='prior'; fi\n", "command alias cw='prior'\n", "alias 'cw'='prior'\n", "alias \"cw\"=\"prior\"\n", "alias \\\ncw='prior'\n", "functions[cw]='prior'\n", aliasStart + "\n", "# agent-workflows-cw-start\n"}
	for _, data := range definitions {
		t.Run(data, func(t *testing.T) {
			paths := aliasTestPaths(t)
			rc := filepath.Join(paths.Home, ".bashrc")
			aliasTestWrite(t, rc, []byte(data), 0o600)
			if _, _, _, err := AliasPrepare(paths, "bash", nil); err == nil {
				t.Fatal("existing definition accepted")
			}
			if got := string(aliasTestRead(t, rc)); got != data {
				t.Fatal("input mutated")
			}
		})
	}
	paths := aliasTestPaths(t)
	aliasTestWrite(t, filepath.Join(paths.Home, ".bashrc"), []byte("# cw() { example only; }\n# alias cw=example\n"), 0o600)
	if _, _, _, err := AliasPrepare(paths, "bash", nil); err != nil {
		t.Fatal(err)
	}
}

func TestAliasSymlinksAndNoncanonicalMetadataRefuse(t *testing.T) {
	paths := aliasTestPaths(t)
	rc := filepath.Join(paths.Home, ".bashrc")
	target := filepath.Join(filepath.Dir(paths.Home), "foreign")
	aliasTestWrite(t, target, []byte("foreign"), 0o600)
	if err := os.Symlink(target, rc); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := AliasPrepare(paths, "bash", nil); err == nil {
		t.Fatal("symlinked startup accepted")
	}
	if err := os.Remove(rc); err != nil {
		t.Fatal(err)
	}
	metadata, _, _, err := AliasPrepare(paths, "bash", nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{target, filepath.Join(paths.Home, "nested", ".bashrc"), paths.Home + "/../foreign"} {
		bad := maps.Clone(metadata)
		bad["path"] = path
		if err := AliasValidateMetadata(bad, paths); err == nil {
			t.Fatalf("unsafe metadata path accepted: %s", path)
		}
	}
	if err := os.Remove(paths.Home); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(paths.Source, paths.Home); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := AliasPrepare(paths, "bash", nil); err == nil {
		t.Fatal("symlinked home accepted")
	}
}

func TestAliasChangedBlockAndForeignDefinitionRefuseAllPhases(t *testing.T) {
	for _, mutation := range []func([]byte) []byte{
		func(data []byte) []byte { return bytes.ReplaceAll(data, []byte(`"$@"`), []byte(`"$1"`)) },
		func(data []byte) []byte { return append(data, []byte("alias cw='foreign'\n")...) },
	} {
		paths := aliasTestPaths(t)
		metadata, operation, _, err := AliasPrepare(paths, "bash", nil)
		if err != nil {
			t.Fatal(err)
		}
		aliasTestApply(t, operation, paths)
		rc := metadata["path"].(string)
		data := mutation(aliasTestRead(t, rc))
		aliasTestWrite(t, rc, data, 0o600)
		if err := AliasVerify(metadata, paths); err == nil {
			t.Fatal("changed block verified")
		}
		if _, err := AliasReversal(operation, paths); err == nil {
			t.Fatal("changed block reversed")
		}
		if _, err := AliasRemoval(metadata, paths); err == nil {
			t.Fatal("changed block removed")
		}
		if !bytes.Equal(aliasTestRead(t, rc), data) {
			t.Fatal("changed block mutated")
		}
	}
}

func TestAliasRecoveryAndResumptionPreserveConcurrentBytes(t *testing.T) {
	paths := aliasTestPaths(t)
	rc := filepath.Join(paths.Home, ".bashrc")
	original := []byte("# original\n")
	aliasTestWrite(t, rc, original, 0o600)
	_, operation, _, err := AliasPrepare(paths, "bash", nil)
	if err != nil {
		t.Fatal(err)
	}
	if reverse, err := AliasReversal(operation, paths); err != nil || reverse != nil {
		t.Fatalf("before apply: %v, %v", reverse, err)
	}
	aliasTestApply(t, operation, paths)
	aliasTestWrite(t, rc, append(aliasTestRead(t, rc), []byte("# later\n")...), 0o600)
	reverse, err := AliasReversal(operation, paths)
	if err != nil {
		t.Fatal(err)
	}
	aliasTestWrite(t, rc, append(aliasTestRead(t, rc), []byte("# before resumed recovery\n")...), 0o600)
	pending, err := AliasPending(reverse, paths)
	if err != nil {
		t.Fatal(err)
	}
	aliasTestApply(t, pending, paths)
	if pending, err := AliasPending(reverse, paths); err != nil || pending != nil {
		t.Fatalf("retry: %v, %v", pending, err)
	}
	if got := string(aliasTestRead(t, rc)); got != "# original\n# later\n# before resumed recovery\n" {
		t.Fatalf("got %q", got)
	}
}

func TestAliasRemovalRecoveryPreservesUnterminatedConcurrentBytes(t *testing.T) {
	paths := aliasTestPaths(t)
	rc := filepath.Join(paths.Home, ".bashrc")
	aliasTestWrite(t, rc, []byte("# original\n"), 0o600)
	metadata, operation, _, err := AliasPrepare(paths, "bash", nil)
	if err != nil {
		t.Fatal(err)
	}
	aliasTestApply(t, operation, paths)
	removal, err := AliasRemoval(metadata, paths)
	if err != nil {
		t.Fatal(err)
	}
	aliasTestApply(t, removal, paths)
	concurrent := []byte("# changed unrelated bytes without newline")
	aliasTestWrite(t, rc, concurrent, 0o600)
	reverse, err := AliasReversal(removal, paths)
	if err != nil {
		t.Fatal(err)
	}
	aliasTestApply(t, reverse, paths)
	if err := AliasVerify(metadata, paths); err != nil {
		t.Fatal(err)
	}
	if pending, err := AliasPending(reverse, paths); err != nil || pending != nil {
		t.Fatalf("retry: %v, %v", pending, err)
	}
	removal, err = AliasRemoval(metadata, paths)
	if err != nil {
		t.Fatal(err)
	}
	aliasTestApply(t, removal, paths)
	if got := aliasTestRead(t, rc); !bytes.Equal(got, concurrent) {
		t.Fatalf("got %q", got)
	}
}

func TestAliasEnrollmentExistenceAndRemovalModeConflicts(t *testing.T) {
	t.Run("created concurrently", func(t *testing.T) {
		paths := aliasTestPaths(t)
		_, operation, _, err := AliasPrepare(paths, "bash", nil)
		if err != nil {
			t.Fatal(err)
		}
		aliasTestWrite(t, operation["path"].(string), nil, 0o600)
		if _, _, _, err := AliasRender(operation, paths); err == nil {
			t.Fatal("concurrent empty file adopted")
		}
	})
	t.Run("mode changed before deletion", func(t *testing.T) {
		paths := aliasTestPaths(t)
		metadata, operation, _, err := AliasPrepare(paths, "bash", nil)
		if err != nil {
			t.Fatal(err)
		}
		aliasTestApply(t, operation, paths)
		removal, err := AliasRemoval(metadata, paths)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(metadata["path"].(string), 0o640); err != nil {
			t.Fatal(err)
		}
		if _, _, _, err := AliasRender(removal, paths); err == nil {
			t.Fatal("mode conflict accepted")
		}
	})
}

func TestAliasMetadataOperationJSONCompatibility(t *testing.T) {
	paths := aliasTestPaths(t)
	metadata, operation, _, err := AliasPrepare(paths, "bash", nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []Object{metadata, operation} {
		data, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		var decoded Object
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatal(err)
		}
		if decoded["kind"] == "command_alias" {
			if err := AliasValidateOperation(decoded, paths); err != nil {
				t.Fatal(err)
			}
		} else if err := AliasValidateMetadata(decoded, paths); err != nil {
			t.Fatal(err)
		}
	}
	for _, field := range []string{"shell", "source", "path", "segment", "original_mode", "original_exists"} {
		bad := maps.Clone(metadata)
		bad[field] = nil
		if err := AliasValidateMetadata(bad, paths); err == nil {
			t.Fatalf("invalid %s accepted", field)
		}
	}
	aliasTestApply(t, operation, paths)
	other := filepath.Join(filepath.Dir(paths.Source), "another source")
	if err := os.Mkdir(other, 0o700); err != nil {
		t.Fatal(err)
	}
	aliasTestWrite(t, filepath.Join(other, "install.sh"), []byte("#!/bin/sh\nexit 0\n"), 0o755)
	paths.Source = other
	if _, _, _, err := AliasPrepare(paths, "auto", metadata); err == nil || !strings.Contains(err.Error(), "enrolled checkout or uninstall") {
		t.Fatalf("got %v", err)
	}
}

func TestAliasRealShellPreservesQuotedPathsArgvStatusAndCWD(t *testing.T) {
	paths := aliasTestPaths(t)
	aliasTestWrite(t, filepath.Join(paths.Source, "install.sh"), []byte("#!/bin/sh\nprintf '%s\\0' \"$PWD\" \"$@\"\nexit 7\n"), 0o755)
	metadata, operation, _, err := AliasPrepare(paths, "bash", nil)
	if err != nil {
		t.Fatal(err)
	}
	aliasTestApply(t, operation, paths)
	arguments := []string{"status", "space argument", "apostrophe ' $() ;", "--help"}
	for _, shell := range []string{"bash", "zsh"} {
		t.Run(shell, func(t *testing.T) {
			binary, err := exec.LookPath(shell)
			if err != nil {
				t.Skipf("%s unavailable", shell)
			}
			command := []string{"-c", `source "$1"; shift; cw "$@"`, "fixture", metadata["path"].(string)}
			command = append(command, arguments...)
			process := exec.CommandContext(t.Context(), binary, command...)
			process.Env = aliasTestShellEnvironment(paths.Home)
			process.Dir = filepath.Dir(paths.Source)
			var stdout, stderr bytes.Buffer
			process.Stdout, process.Stderr = &stdout, &stderr
			err = process.Run()
			exit, ok := err.(*exec.ExitError)
			if !ok || exit.ExitCode() != 7 {
				t.Fatalf("exit = %v, stderr = %s", err, stderr.Bytes())
			}
			want := []string{process.Dir, "--home", paths.Home, "--codex-home", paths.Codex, "--state-dir", paths.State}
			want = append(want, arguments...)
			got := strings.Split(strings.TrimSuffix(stdout.String(), "\x00"), "\x00")
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("got %#v, want %#v", got, want)
			}
		})
	}
}

func TestAliasRuntimeGuardAndDoubleSource(t *testing.T) {
	for _, name := range []string{"TYPESAFE_API_KEY", "SUBQ_API_KEY", "NOTION_TOKEN"} {
		t.Setenv(name, "fixture-secret-must-not-leak")
	}
	paths := aliasTestPaths(t)
	metadata, operation, _, err := AliasPrepare(paths, "bash", nil)
	if err != nil {
		t.Fatal(err)
	}
	aliasTestApply(t, operation, paths)
	for _, shell := range []string{"bash", "zsh"} {
		t.Run(shell, func(t *testing.T) {
			binary, err := exec.LookPath(shell)
			if err != nil {
				t.Skipf("%s unavailable", shell)
			}
			for _, definition := range []string{"alias cw='printf prior'", "function cw { printf prior; }"} {
				definitionPath := filepath.Join(filepath.Dir(paths.Home), "indirect definition.rc")
				aliasTestWrite(t, definitionPath, []byte(definition+"\n"), 0o600)
				command := `source "$1"; source "$2"; eval cw`
				if shell == "bash" {
					command = "shopt -s expand_aliases; " + command
				}
				process := exec.CommandContext(t.Context(), binary, "-c", command, "fixture", definitionPath, metadata["path"].(string))
				process.Env = aliasTestShellEnvironment(paths.Home)
				var stdout, stderr bytes.Buffer
				process.Stdout, process.Stderr = &stdout, &stderr
				if err := process.Run(); err != nil {
					t.Fatalf("%v: %s", err, stderr.String())
				}
				if stdout.String() != "prior" || !strings.Contains(stderr.String(), "cw already exists; keeping current command") {
					t.Fatalf("stdout = %q, stderr = %q", stdout.String(), stderr.String())
				}
			}
			process := exec.CommandContext(t.Context(), binary, "-c", `source "$1"; source "$1"; env`, "fixture", metadata["path"].(string))
			process.Env = aliasTestShellEnvironment(paths.Home)
			var stdout, stderr bytes.Buffer
			process.Stdout, process.Stderr = &stdout, &stderr
			if err := process.Run(); err != nil {
				t.Fatalf("%v: %s", err, stderr.String())
			}
			if bytes.Contains(stdout.Bytes(), []byte("fixture-secret-must-not-leak")) {
				t.Fatal("real-shell fixture inherited a credential")
			}
			if stderr.Len() != 0 || bytes.Contains(stdout.Bytes(), []byte("_AGENT_WORKFLOWS_CW_OWNER=")) {
				t.Fatalf("stdout = %q, stderr = %q", stdout.String(), stderr.String())
			}
		})
	}
}
