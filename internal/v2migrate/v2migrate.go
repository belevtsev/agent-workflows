// Package v2migrate removes a v2 agent-workflows installation: the host links
// that point into its state directory, the managed instruction blocks and the
// release snapshots. It keeps the task store and everything it does not own.
// It exists only to move v2 users to the plugins and can be deleted with v2.
package v2migrate

import (
	"encoding/json/v2"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/belevtsev/agent-workflows/v3/internal/codexinstall"
)

// Options names the v2 installation's directories.
type Options struct {
	Home, ClaudeHome, CodexHome, StateDir string
	DryRun                                bool
}

// Action is one removal: "unlink", "strip-block" or "remove".
type Action struct {
	Kind string
	Path string
}

// ErrNotStateDir reports a state directory that does not look like v2's.
var ErrNotStateDir = errors.New("not a v2 state directory")

// stateEntries are the v2 engine's own files in the state directory. The
// task store (workflow-tasks) and anything else there stays.
var stateEntries = []string{"current", "releases", "bases", "runtime", "state.json", "journal.json"}

// Run plans every action, then performs them unless DryRun is set. Planning
// reads everything first, so malformed markers stop the run before any change.
// The managed blocks are stripped only while the state directory still holds
// v2's state.json or journal.json, so a v3 block written by cw install --host
// codex survives a later run. On failure Run returns the actions it completed.
// A completed run leaves nothing to do, so running again is a no-op.
//
// Relative directories are resolved against the working directory. A state
// directory holding none of v2's entries has nothing to migrate, so a
// finished migration, or a mistyped --state-dir such as ~/.local/state or
// ~/.local/share/agent-workflows, changes nothing. One that holds release
// entries but neither state.json nor journal.json must be named
// agent-workflows; otherwise Run refuses it.
func Run(o Options) ([]Action, error) {
	for _, path := range []*string{&o.Home, &o.ClaudeHome, &o.CodexHome, &o.StateDir} {
		var err error
		if *path, err = filepath.Abs(*path); err != nil {
			return nil, err
		}
	}
	recorded, installed, err := recordedHomes(o.StateDir)
	if err != nil {
		return nil, err
	}
	if !installed {
		engine := false
		for _, name := range stateEntries {
			if _, err := os.Lstat(filepath.Join(o.StateDir, name)); err == nil {
				engine = true
			} else if !errors.Is(err, fs.ErrNotExist) {
				return nil, err
			}
		}
		if !engine {
			return nil, nil
		}
		if filepath.Base(o.StateDir) != "agent-workflows" {
			return nil, fmt.Errorf("%w: %s holds no state.json or journal.json and is not named agent-workflows", ErrNotStateDir, o.StateDir)
		}
	}
	roots := []string{o.StateDir}
	if resolved, err := filepath.EvalSymlinks(o.StateDir); err == nil && resolved != roots[0] {
		roots = append(roots, resolved)
	}
	claudeHomes := homes(o.ClaudeHome, recorded.claude)
	codexHomes := homes(o.CodexHome, recorded.codex)
	var actions []Action
	type place struct{ dir, pattern string }
	links := []place{
		{filepath.Join(o.Home, ".agents", "skills"), "*"},
		{filepath.Join(o.Home, ".local", "bin"), "cw"},
	}
	for _, home := range claudeHomes {
		links = append(links, place{filepath.Join(home, "skills"), "*"}, place{filepath.Join(home, "agents"), "cw-*.md"})
	}
	for _, place := range links {
		matches, err := filepath.Glob(filepath.Join(place.dir, place.pattern))
		if err != nil {
			return nil, err
		}
		for _, path := range matches {
			if intoState(path, roots) {
				actions = append(actions, Action{"unlink", path})
			}
		}
	}
	if installed {
		var instructions []string
		for _, home := range claudeHomes {
			instructions = append(instructions, filepath.Join(home, "CLAUDE.md"))
		}
		for _, home := range codexHomes {
			instructions = append(instructions, filepath.Join(home, "AGENTS.md"))
		}
		for _, path := range instructions {
			found, err := codexinstall.RemoveBlockFromFile(path, true)
			if err != nil {
				return nil, err
			}
			if found {
				actions = append(actions, Action{"strip-block", path})
			}
		}
	}
	for _, name := range stateEntries {
		path := filepath.Join(o.StateDir, name)
		if _, err := os.Lstat(path); err == nil {
			actions = append(actions, Action{"remove", path})
		} else if !errors.Is(err, fs.ErrNotExist) {
			return nil, err
		}
	}
	if o.DryRun {
		return actions, nil
	}
	for done, action := range actions {
		var err error
		switch action.Kind {
		case "unlink":
			err = os.Remove(action.Path)
		case "strip-block":
			_, err = codexinstall.RemoveBlockFromFile(action.Path, false)
		case "remove":
			err = removeTree(action.Path)
		}
		if err != nil {
			return actions[:done], err
		}
	}
	return actions, nil
}

type hostHomes struct{ codex, claude string }

// recordedHomes reads the host roots v2 recorded, journal.json first because
// an interrupted uninstall can leave no state.json, as v2 itself did.
// installed reports whether either file exists. A record that is not the
// expected JSON names no roots; the selected roots still apply.
func recordedHomes(stateDir string) (homes hostHomes, installed bool, err error) {
	var journal struct {
		CodexHome  string `json:"codex_home"`
		ClaudeHome string `json:"claude_home"`
	}
	found, err := readRecord(filepath.Join(stateDir, "journal.json"), &journal)
	if err != nil || found {
		return hostHomes{journal.CodexHome, journal.ClaudeHome}, found, err
	}
	var state struct {
		Hosts map[string]struct {
			Home string `json:"home"`
		} `json:"hosts"`
	}
	found, err = readRecord(filepath.Join(stateDir, "state.json"), &state)
	return hostHomes{state.Hosts["codex"].Home, state.Hosts["claude"].Home}, found, err
}

// readRecord decodes path into value when it holds a JSON object of that
// shape. found reports whether the file exists; only a read failure is an
// error.
func readRecord(path string, value any) (found bool, err error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return true, err
	}
	_ = json.Unmarshal(data, value)
	return true, nil
}

// homes is the selected root plus a different absolute recorded one.
func homes(selected, recorded string) []string {
	if recorded == "" || !filepath.IsAbs(recorded) || filepath.Clean(recorded) == filepath.Clean(selected) {
		return []string{selected}
	}
	return []string{selected, filepath.Clean(recorded)}
}

// intoState reports whether path is a symlink whose target, dangling or not,
// lies inside one of the state directory's spellings.
func intoState(path string, roots []string) bool {
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&fs.ModeSymlink == 0 {
		return false
	}
	target, err := os.Readlink(path)
	if err != nil {
		return false
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(filepath.Dir(path), target)
	}
	target = filepath.Clean(target)
	for _, spelling := range []string{target, resolveExisting(target)} {
		for _, root := range roots {
			if spelling == root || strings.HasPrefix(spelling, root+string(filepath.Separator)) {
				return true
			}
		}
	}
	return false
}

// resolveExisting resolves symlinks in the longest existing prefix of an
// absolute path and appends the rest unchanged, so a dangling target spelled
// through a symlinked parent (such as /var on macOS) compares with the
// resolved state directory.
func resolveExisting(path string) string {
	rest := ""
	for dir := path; ; dir = filepath.Dir(dir) {
		if resolved, err := filepath.EvalSymlinks(dir); err == nil {
			return filepath.Join(resolved, rest)
		}
		if parent := filepath.Dir(dir); parent == dir {
			return path
		}
		rest = filepath.Join(filepath.Base(dir), rest)
	}
}

// removeTree deletes path recursively. v2 release snapshots are read-only, so
// directories are made writable first; symlinks are removed, never followed.
func removeTree(path string) error {
	err := filepath.WalkDir(path, func(entry string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return os.Chmod(entry, 0o700)
		}
		return nil
	})
	if err != nil {
		return err
	}
	return os.RemoveAll(path)
}
