package devcheck

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	json "encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
)

// ExtractBinary obtains the already-built native cw from its verified release
// archive. Only the single regular executable is materialized.
func ExtractBinary(archive, destination string) (err error) {
	file, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer file.Close()
	zipped, err := gzip.NewReader(file)
	if err != nil {
		return err
	}
	defer zipped.Close()
	reader := tar.NewReader(zipped)
	var executable []byte
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
		if header.Name != "cw" {
			continue
		}
		if executable != nil || header.Typeflag != tar.TypeReg || header.Size <= 0 || header.Size > 128<<20 {
			return errors.New("release archive has an invalid cw executable")
		}
		executable, err = io.ReadAll(io.LimitReader(reader, header.Size+1))
		if err != nil || int64(len(executable)) != header.Size {
			return errors.Join(errors.New("release executable size differs"), err)
		}
	}
	if executable == nil {
		return errors.New("release archive is missing cw")
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return err
	}
	return os.WriteFile(destination, executable, 0o755)
}

// NativeSmoke validates an exact committed source and one prebuilt binary in
// isolated paths with spaces. Bash runs only to exercise supported startup files.
func NativeSmoke(ctx context.Context, source, binary string, output io.Writer) error {
	if output == nil {
		output = io.Discard
	}
	temporary, err := os.MkdirTemp("", "cw-native-smoke-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(temporary)
	checkout := filepath.Join(temporary, "source with spaces")
	home := filepath.Join(temporary, "home with spaces")
	codex := filepath.Join(temporary, "codex with spaces")
	state := filepath.Join(temporary, "state with spaces")
	command := exec.CommandContext(ctx, "git", "clone", "--quiet", "--no-hardlinks", source, checkout)
	if result, err := command.CombinedOutput(); err != nil {
		return fmt.Errorf("smoke source clone: %w\n%s", err, result)
	}
	data, err := os.ReadFile(binary)
	if err != nil {
		return err
	}
	native := filepath.Join(checkout, ".bin", "cw")
	if err := os.Mkdir(filepath.Dir(native), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(native, data, 0o755); err != nil {
		return err
	}
	env := make([]string, 0, len(os.Environ()))
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		if !slices.Contains([]string{"HOME", "SHELL", "PATH", "TYPESAFE_API_KEY", "SUBQ_API_KEY", "CODEX_HOME", "XDG_STATE_HOME", "AGENT_WORKFLOWS_FAULT", "ZDOTDIR", "GIT_CONFIG_GLOBAL", "GIT_CONFIG_NOSYSTEM"}, name) && !strings.HasPrefix(name, "BASH_FUNC_") {
			env = append(env, entry)
		}
	}
	var directories []string
	for _, tool := range []string{"git", "go"} {
		path, err := exec.LookPath(tool)
		if err != nil {
			return err
		}
		path, err = filepath.EvalSymlinks(path)
		if err != nil {
			return err
		}
		directory := filepath.Dir(path)
		if !slices.Contains(directories, directory) {
			directories = append(directories, directory)
		}
	}
	directories = append(directories, "/usr/bin", "/bin")
	env = append(env, "HOME="+home, "SHELL=/bin/bash", "PATH="+strings.Join(directories, string(os.PathListSeparator)), "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_NOSYSTEM=1")
	run := func(program string, arguments ...string) ([]byte, error) {
		command := exec.CommandContext(ctx, program, arguments...)
		command.Dir, command.Env = temporary, env
		result, err := command.CombinedOutput()
		if err != nil {
			return nil, fmt.Errorf("native smoke %s: %w\n%s", filepath.Base(program), err, result)
		}
		return result, nil
	}
	paths := []string{"--source", checkout, "--home", home, "--codex-home", codex, "--state-dir", state}
	launch := func(arguments ...string) ([]byte, error) {
		return run(native, append(arguments, paths...)...)
	}
	if _, err := run(filepath.Join(checkout, "install.sh"), append([]string{"--dry-run", "--shell", "bash"}, paths[2:]...)...); err != nil {
		return err
	}
	for _, path := range []string{home, codex, state} {
		if _, err := os.Lstat(path); !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("preview created installation path: %s", path)
		}
	}
	// Exercise the native environment-reuse boundary without package downloads.
	// Provisioning/repair commands have separate fake-process lifecycle tests.
	python := filepath.Join(home, ".venvs", "codex-decisions", "bin", "python")
	if err := os.MkdirAll(filepath.Dir(python), 0o700); err != nil {
		return err
	}
	probe := "#!/bin/sh\nprintf '%s\\n' '{\"status\":\"ready\",\"python_version\":\"3.9.6\",\"pyyaml_version\":\"6.0.3\",\"pip_available\":true}'\n"
	if err := os.WriteFile(python, []byte(probe), 0o755); err != nil {
		return err
	}
	setup, err := run(filepath.Join(checkout, "install.sh"), append([]string{"--shell", "bash"}, paths[2:]...)...)
	if err != nil {
		return err
	}
	if err := requireInstalled(setup, true); err != nil {
		return err
	}
	manifestData, err := os.ReadFile(filepath.Join(checkout, "skills-manifest.json"))
	if err != nil {
		return err
	}
	var manifest struct {
		Registrations map[string]string `json:"registrations"`
	}
	if err := json.Unmarshal(manifestData, &manifest); err != nil || len(manifest.Registrations) == 0 {
		return errors.Join(errors.New("smoke manifest has no valid registrations"), err)
	}
	for _, skill := range slices.Sorted(maps.Keys(manifest.Registrations)) {
		path := filepath.Join(home, ".agents", "skills", skill)
		info, err := os.Lstat(path)
		if err != nil || info.Mode()&os.ModeSymlink == 0 {
			return fmt.Errorf("smoke registration is missing: %s", skill)
		}
		if info, err := os.Stat(path); err != nil || !info.IsDir() {
			return fmt.Errorf("smoke registration does not resolve: %s", skill)
		}
	}
	installed := filepath.Join(home, ".local", "bin", "cw")
	cached, err := filepath.EvalSymlinks(installed)
	if err != nil {
		return err
	}
	files := []string{filepath.Join(state, "state.json"), filepath.Join(codex, "config.toml"), filepath.Join(home, ".bashrc")}
	before := make(map[string][]byte)
	for _, path := range files {
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		before[path] = data
	}
	if _, err := launch("setup", "--shell", "bash"); err != nil {
		return err
	}
	for path, expected := range before {
		data, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(data, expected) {
			return fmt.Errorf("repeat setup changed owned file: %s", path)
		}
	}
	loaded, err := run("bash", "--noprofile", "--norc", "-c", `. "$1"; type -t cw; cw help; cw status`, "cw-smoke", filepath.Join(home, ".bashrc"))
	if err != nil {
		return err
	}
	if !bytes.HasPrefix(loaded, []byte("file\n")) || !bytes.Contains(loaded, []byte(`"installed":true`)) {
		return errors.New("shell startup did not resolve cw as a direct native binary")
	}
	for _, action := range []string{"status", "recover"} {
		if _, err := launch(action); err != nil {
			return err
		}
	}
	// A fresh installed process must find all three custom roots from its
	// locator, without inherited flags or the original checkout launcher.
	located, err := run(installed, "status")
	if err != nil {
		return err
	}
	if err := requireInstalled(located, true); err != nil {
		return err
	}
	fault := exec.CommandContext(ctx, installed, "uninstall")
	fault.Dir = temporary
	fault.Env = append(append([]string{}, env...), "AGENT_WORKFLOWS_FAULT=after-manager-locator")
	failure, err := fault.CombinedOutput()
	if err == nil || !bytes.Contains(failure, []byte("after-manager-locator")) {
		return fmt.Errorf("native interrupted-uninstall fixture did not reach its locator fault: %v\n%s", err, failure)
	}
	for _, removed := range []string{installed, filepath.Join(state, "runtime", "current"), filepath.Join(state, "runtime", "locator.json")} {
		if _, err := os.Lstat(removed); !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("interrupted uninstall did not remove expected locator path: %s", removed)
		}
	}
	recovered, err := run(cached, "recover")
	if err != nil {
		return err
	}
	if err := requireInstalled(recovered, true); err != nil {
		return err
	}
	located, err = run(installed, "status")
	if err != nil {
		return err
	}
	if err := requireInstalled(located, true); err != nil {
		return err
	}
	uninstalled, err := run(installed, "uninstall")
	if err != nil {
		return err
	}
	if err := requireInstalled(uninstalled, false); err != nil {
		return err
	}
	for _, path := range []string{filepath.Join(home, ".bashrc"), filepath.Join(codex, "AGENTS.md"), installed} {
		if _, err := os.Lstat(path); !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("uninstall retained owned path: %s", path)
		}
	}
	_, err = fmt.Fprintln(output, "Native preview, setup, direct cw, repeat, custom-root discovery, interrupted locator recovery and uninstall smoke passed.")
	return err
}

func requireInstalled(data []byte, want bool) error {
	var report struct {
		Status struct {
			Installed *bool `json:"installed"`
		} `json:"status"`
	}
	if err := json.Unmarshal(data, &report); err != nil {
		return err
	}
	if report.Status.Installed == nil {
		return errors.New("native smoke returned no installed status")
	}
	if *report.Status.Installed != want {
		return fmt.Errorf("native smoke installed status = %v, want %v", *report.Status.Installed, want)
	}
	return nil
}
