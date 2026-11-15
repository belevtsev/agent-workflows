// Package gate holds the git hooks of kitsu: the files it installs and the
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
	"slices"
	"strings"

	"github.com/belevtsev/kitsu/v3/internal/gitx"
)

// Hooks are the three hooks kitsu installs.
var Hooks = []string{"pre-commit", "pre-merge-commit", "pre-push"}

// InstallURL is where the install instruction is. A message that tells a
// user to install kitsu names this place and no command of any toolchain.
const InstallURL = "https://github.com/belevtsev/kitsu#install"

// NoDeclarationURL is where the documents say what to do about a tree that
// holds no kitsu.toml on purpose. A denial names this place and no command.
const NoDeclarationURL = "https://github.com/belevtsev/kitsu/blob/main/docs/operations.md#a-tree-without-kitsutoml"

// marker is the line that shows kitsu wrote a hook file.
const marker = "# kitsu-hook: written by `kitsu init`. `kitsu uninstall` removes this file."

// The states of the hooks in one clone.
const (
	Installed = "installed"
	Missing   = "missing"
)

// IsHook says whether name is one of the hooks kitsu knows.
func IsHook(name string) bool { return slices.Contains(Hooks, name) }

// Line is the line that calls kitsu from a hook another tool owns. It must be
// the last command of that hook.
func Line(name string) string {
	return fmt.Sprintf(`kitsu hook %s "$@"`, name)
}

// Script is the hook file kitsu writes. It is a POSIX shell script, which is
// also what Git for Windows runs. It looks for kitsu only in the entries of PATH
// that are absolute paths, so an empty or relative entry cannot make it run a
// file of the repository, whatever form the shell gives to what it finds;
// without such a kitsu it denies.
func Script(name string) string {
	return strings.Join([]string{
		"#!/bin/sh",
		marker,
		"dirs=; IFS=:; set -f",
		`for dir in $PATH; do case "$dir" in /*) dirs="${dirs:+$dirs:}$dir" ;; esac; done; unset IFS; set +f`,
		`kitsu=$([ -n "$dirs" ] && PATH=$dirs && command -v kitsu 2>/dev/null)`,
		`case "$kitsu" in /*) exec ` + ownLine + name + ` "$@" ;; esac`,
		`echo "kitsu: the kitsu program was not found in an entry of PATH that is an absolute path, so this ` + name + ` cannot be judged and is denied. An empty or relative entry is not searched." >&2`,
		`echo "kitsu: the install instruction is at ` + InstallURL + `" >&2`,
		"exit 3",
		"",
	}, "\n")
}

// ownLine is how the file kitsu writes calls kitsu, up to the name of the hook.
const ownLine = `"$kitsu" hook `

// Foreign is a hook file kitsu did not write, or one it wrote that was changed
// since. Line is its line that calls kitsu, when Uninstall left the file.
type Foreign struct{ Name, Path, Line string }

// Install writes the three hooks into the hooks directory of the common git
// directory. It never changes a file it did not write, and writes nothing
// when core.hooksPath is set. A hook it left alone is returned for the user
// to edit, unless that hook already calls kitsu.
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

// Own lists the hooks git runs in this clone whose file kitsu wrote, in the
// order of Hooks. A hook of another tool is not among them, whether or not
// it calls kitsu.
func Own(ctx context.Context, repo *gitx.Repo) []string {
	dir, _, err := repo.HooksDir(ctx)
	if err != nil {
		return nil
	}
	var names []string
	for _, name := range Hooks {
		if data, err := os.ReadFile(filepath.Join(dir, name)); err == nil && strings.Contains(string(data), marker) {
			names = append(names, name)
		}
	}
	return names
}

// executable says whether git would run the file as a hook.
func executable(path string) bool {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return false
	}
	return runtime.GOOS == "windows" || info.Mode()&0o111 != 0
}

// calls says whether the file is a hook git would run and that calls kitsu.
func calls(path, name string) bool {
	data, err := os.ReadFile(path)
	return err == nil && executable(path) && callLine(string(data), name) != ""
}

// callLine is the first line of a hook file that is no comment and calls kitsu,
// or nothing.
func callLine(script, name string) string {
	for _, line := range strings.Split(script, "\n") {
		if line = strings.TrimSpace(line); !strings.HasPrefix(line, "#") && (strings.Contains(line, "kitsu hook "+name) || strings.Contains(line, ownLine+name)) {
			return line
		}
	}
	return ""
}

// State says whether git runs the hooks of kitsu in this clone: each of the
// three files git would run exists, can be executed and calls `kitsu hook`.
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

// Uninstall removes the hook files kitsu wrote and says how many there were: in
// the hooks directory of the common git directory, where Install writes, a
// file with the marker; in a directory that core.hooksPath names, which
// belongs to its owner, only a file that is exactly the script of kitsu. A file
// it leaves that still calls `kitsu hook` is returned.
func Uninstall(ctx context.Context, repo *gitx.Repo) (removed int, foreign []Foreign, err error) {
	dir, _, err := repo.HooksDir(ctx)
	if err != nil {
		return 0, nil, err
	}
	own := filepath.Join(repo.CommonDir, "hooks")
	dirs := []string{own}
	if dir != own {
		dirs = append(dirs, dir)
	}
	for _, dir := range dirs {
		for _, name := range Hooks {
			path := filepath.Join(dir, name)
			data, readErr := os.ReadFile(path)
			switch {
			case readErr != nil:
			case string(data) == Script(name) || dir == own && strings.Contains(string(data), marker):
				if err := os.Remove(path); err != nil {
					return removed, nil, err
				}
				removed++
			case callLine(string(data), name) != "":
				foreign = append(foreign, Foreign{Name: name, Path: path, Line: callLine(string(data), name)})
			}
		}
	}
	return removed, foreign, nil
}
