// Package gate holds the git hooks of cw: the files it installs and the
// decisions they make.
package gate

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/belevtsev/agent-workflows/v3/internal/gitx"
)

// Hooks are the three hooks cw installs.
var Hooks = []string{"pre-commit", "pre-merge-commit", "pre-push"}

// InstallURL is where the install instruction is. A message that tells a
// user to install cw names this place and no command of any toolchain.
const InstallURL = "https://github.com/belevtsev/agent-workflows#install"

// marker is the line that shows cw wrote a hook file.
const marker = "# cw-hook: written by `cw init`. `cw uninstall` removes this file."

// The states of the hooks in one clone.
const (
	Installed = "installed"
	Missing   = "missing"
)

// IsHook says whether name is one of the hooks cw knows.
func IsHook(name string) bool {
	for _, h := range Hooks {
		if h == name {
			return true
		}
	}
	return false
}

// Line is the line that calls cw from a hook another tool owns. It must be
// the last command of that hook.
func Line(name string) string {
	return fmt.Sprintf(`cw hook %s "$@"`, name)
}

// Script is the hook file cw writes. It is a POSIX shell script, which is
// also what Git for Windows runs. Without cw on PATH it denies.
func Script(name string) string {
	return strings.Join([]string{
		"#!/bin/sh",
		marker,
		"if ! command -v cw >/dev/null 2>&1; then",
		"  echo \"cw: the cw program was not found on PATH, so this " + name + " cannot be judged and is denied.\" >&2",
		"  echo \"cw: the install instruction is at " + InstallURL + "\" >&2",
		"  exit 3",
		"fi",
		"exec " + Line(name),
		"",
	}, "\n")
}

// Foreign is a hook file cw did not write.
type Foreign struct {
	Name string
	Path string
}

// Install writes the three hooks into the hooks directory of the common git
// directory. It never changes a file it did not write, and writes nothing
// when core.hooksPath is set. A hook it left alone is returned for the user
// to edit, unless that hook already calls cw.
func Install(ctx context.Context, repo *gitx.Repo) (foreign []Foreign, err error) {
	dir, configured, err := repo.HooksDir(ctx)
	if err != nil {
		return nil, err
	}
	if configured {
		for _, name := range Hooks {
			if path := filepath.Join(dir, name); !calls(path, name) {
				foreign = append(foreign, Foreign{Name: name, Path: path})
			}
		}
		return foreign, nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	for _, name := range Hooks {
		path := filepath.Join(dir, name)
		script := Script(name)
		old, readErr := os.ReadFile(path)
		switch {
		case errors.Is(readErr, fs.ErrNotExist):
		case readErr != nil:
			return nil, readErr
		case !strings.Contains(string(old), marker):
			if !calls(path, name) {
				foreign = append(foreign, Foreign{Name: name, Path: path})
			}
			continue
		case string(old) == script && executable(path):
			continue
		}
		if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
			return nil, err
		}
		if err := os.Chmod(path, 0o755); err != nil {
			return nil, err
		}
	}
	return foreign, nil
}

// executable says whether git would run the file as a hook.
func executable(path string) bool {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return false
	}
	return runtime.GOOS == "windows" || info.Mode()&0o111 != 0
}

// calls says whether the file is a hook git would run and that calls cw.
func calls(path, name string) bool {
	data, err := os.ReadFile(path)
	return err == nil && executable(path) && strings.Contains(string(data), "cw hook "+name)
}

// State says whether git runs the hooks of cw in this clone: each of the
// three files git would run exists, can be executed and calls `cw hook`.
func State(ctx context.Context, repo *gitx.Repo) string {
	dir, _, err := repo.HooksDir(ctx)
	if err != nil {
		return Missing
	}
	for _, name := range Hooks {
		if !calls(filepath.Join(dir, name), name) {
			return Missing
		}
	}
	return Installed
}

// Uninstall removes the hook files cw wrote. A file of another tool that
// still calls `cw hook` is left as it is and returned.
func Uninstall(ctx context.Context, repo *gitx.Repo) (foreign []Foreign, err error) {
	dir, _, err := repo.HooksDir(ctx)
	if err != nil {
		return nil, err
	}
	dirs := []string{dir}
	if own := filepath.Join(repo.CommonDir, "hooks"); own != dir {
		dirs = append(dirs, own)
	}
	for _, dir := range dirs {
		for _, name := range Hooks {
			path := filepath.Join(dir, name)
			data, readErr := os.ReadFile(path)
			switch {
			case readErr != nil:
			case strings.Contains(string(data), marker):
				if err := os.Remove(path); err != nil {
					return nil, err
				}
			case strings.Contains(string(data), "cw hook "+name):
				foreign = append(foreign, Foreign{Name: name, Path: path})
			}
		}
	}
	return foreign, nil
}
