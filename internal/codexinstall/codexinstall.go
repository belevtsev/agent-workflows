// Package codexinstall links the agent-workflows skills from a source
// checkout into Codex's personal skill directory and keeps the managed block
// in Codex's AGENTS.md. It keeps no state: it owns exactly the symlinks in
// ~/.agents/skills that point into the checkout and the text between the
// managed markers, and it refuses to replace anything else.
package codexinstall

import (
	"encoding/json/v2"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// ErrNotOwned reports an entry the installer would have to replace but does
// not own.
var ErrNotOwned = errors.New("entry is not owned by agent-workflows")

// ErrInvalidSource reports a checkout whose manifest is missing or unusable.
var ErrInvalidSource = errors.New("invalid agent-workflows checkout")

// Options selects the checkout and the directories the installer writes.
type Options struct {
	Source    string // checkout root holding skills-manifest.json
	Home      string // skills are linked under Home/.agents/skills
	CodexHome string // the managed block lives in CodexHome/AGENTS.md
}

// Change is one skill-link outcome: "link", "relink", "unlink", "unchanged"
// or "skip" (a registered name held by an entry the installer does not own).
type Change struct {
	Action string
	Path   string
	Target string
}

// Report describes what Install or Uninstall did.
type Report struct {
	Changes      []Change
	Instructions string // path of AGENTS.md
	// Block is "inserted", "replaced", "unchanged", "removed" or "absent".
	Block string
	// RecommendedConfig holds global/codex-defaults.toml from the checkout.
	// Install prints it for the user and never edits config.toml itself.
	RecommendedConfig string
}

const (
	manifestName   = "skills-manifest.json"
	defaultsConfig = "global/codex-defaults.toml"
)

var skillName = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

type manifest struct {
	Registrations      map[string]string `json:"registrations"`
	GlobalInstructions string            `json:"global_instructions"`
}

// layout is the resolved set of paths one run works on.
type layout struct {
	source, skills, instructions string
}

func resolve(o Options) (layout, error) {
	source, err := filepath.Abs(o.Source)
	if err != nil {
		return layout{}, fmt.Errorf("%w: %v", ErrInvalidSource, err)
	}
	return layout{
		source:       source,
		skills:       filepath.Join(o.Home, ".agents", "skills"),
		instructions: filepath.Join(o.CodexHome, "AGENTS.md"),
	}, nil
}

// readManifest loads and checks the checkout's manifest: every registration
// has a plain lower-case name and names an existing directory inside it.
func readManifest(source string) (manifest, error) {
	var m manifest
	data, err := os.ReadFile(filepath.Join(source, manifestName))
	if err != nil {
		return m, fmt.Errorf("%w: %v", ErrInvalidSource, err)
	}
	if err := json.Unmarshal(data, &m); err != nil {
		return m, fmt.Errorf("%w: %s: %v", ErrInvalidSource, manifestName, err)
	}
	if len(m.Registrations) == 0 {
		return m, fmt.Errorf("%w: %s has no registrations", ErrInvalidSource, manifestName)
	}
	for name, dir := range m.Registrations {
		if !skillName.MatchString(name) {
			return m, fmt.Errorf("%w: invalid registration name %q", ErrInvalidSource, name)
		}
		if !local(dir) {
			return m, fmt.Errorf("%w: registration %s path %q must be relative and inside the checkout", ErrInvalidSource, name, dir)
		}
		if info, err := os.Stat(filepath.Join(source, filepath.FromSlash(dir))); err != nil || !info.IsDir() {
			return m, fmt.Errorf("%w: registration %s is not a directory: %s", ErrInvalidSource, name, dir)
		}
	}
	if !local(m.GlobalInstructions) {
		return m, fmt.Errorf("%w: global_instructions %q must be relative and inside the checkout", ErrInvalidSource, m.GlobalInstructions)
	}
	return m, nil
}

// local reports a nonempty slash path that stays inside its root.
func local(path string) bool {
	return path != "" && !slices.Contains(strings.Split(path, "/"), "..") && filepath.IsLocal(filepath.FromSlash(path))
}

// within reports whether path is root or lies beneath it.
func within(root, path string) bool {
	return path == root || strings.HasPrefix(path, root+string(filepath.Separator))
}

// linkTarget returns the cleaned absolute target of the symlink at path, or
// ok=false when path is not a symlink.
func linkTarget(path string) (target string, ok bool, err error) {
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&fs.ModeSymlink == 0 {
		return "", false, err
	}
	target, err = os.Readlink(path)
	if err != nil {
		return "", false, err
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(filepath.Dir(path), target)
	}
	return filepath.Clean(target), true, nil
}

// ownedLinks returns the symlinks directly in dir that point into source,
// keyed by name, with their cleaned targets.
func ownedLinks(dir, source string) (map[string]string, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return map[string]string{}, nil
	}
	if err != nil {
		return nil, err
	}
	owned := map[string]string{}
	for _, entry := range entries {
		target, ok, err := linkTarget(filepath.Join(dir, entry.Name()))
		if err != nil {
			return nil, err
		}
		if ok && within(source, target) {
			owned[entry.Name()] = target
		}
	}
	return owned, nil
}

// Install links every registration into Home/.agents/skills, removes owned
// links the manifest no longer registers, and writes the managed block into
// AGENTS.md. Every check runs before the first write, so a refusal changes
// nothing. A second run with the same checkout changes nothing.
func Install(o Options) (Report, error) {
	paths, err := resolve(o)
	if err != nil {
		return Report{}, err
	}
	report := Report{Instructions: paths.instructions}
	m, err := readManifest(paths.source)
	if err != nil {
		return report, err
	}
	body, err := os.ReadFile(filepath.Join(paths.source, filepath.FromSlash(m.GlobalInstructions)))
	if err != nil {
		return report, fmt.Errorf("%w: %v", ErrInvalidSource, err)
	}
	if defaults, err := os.ReadFile(filepath.Join(paths.source, filepath.FromSlash(defaultsConfig))); err == nil {
		report.RecommendedConfig = string(defaults)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return report, err
	}
	owned, err := ownedLinks(paths.skills, paths.source)
	if err != nil {
		return report, err
	}
	for _, name := range sortedKeys(m.Registrations) {
		path := filepath.Join(paths.skills, name)
		want := filepath.Join(paths.source, filepath.FromSlash(m.Registrations[name]))
		target, isOwned := owned[name]
		switch {
		case isOwned && target == want:
			report.Changes = append(report.Changes, Change{Action: "unchanged", Path: path, Target: want})
		case isOwned:
			report.Changes = append(report.Changes, Change{Action: "relink", Path: path, Target: want})
		default:
			if _, err := os.Lstat(path); err == nil {
				return Report{Instructions: paths.instructions}, fmt.Errorf("%w: %s (remove it; if a v2 installation left it, see \"Cleaning up a v2 installation\" in docs/operations.md)", ErrNotOwned, path)
			} else if !errors.Is(err, fs.ErrNotExist) {
				return report, err
			}
			report.Changes = append(report.Changes, Change{Action: "link", Path: path, Target: want})
		}
	}
	for _, name := range sortedKeys(owned) {
		if _, registered := m.Registrations[name]; !registered {
			report.Changes = append(report.Changes, Change{Action: "unlink", Path: filepath.Join(paths.skills, name)})
		}
	}
	instructions, err := readInstructions(paths.instructions)
	if err != nil {
		return report, err
	}
	updated, status, err := UpsertBlock(instructions.data, body)
	if err != nil {
		return report, fmt.Errorf("%s: %w", instructions.path, err)
	}
	report.Block = status

	if err := os.MkdirAll(paths.skills, 0o755); err != nil { //nolint:gosec // G301: Codex's skills directory keeps the usual 0755 of a home config directory.
		return report, err
	}
	for _, change := range report.Changes {
		if err := applyLink(change); err != nil {
			return report, err
		}
	}
	if status != "unchanged" {
		if err := instructions.save(updated); err != nil {
			return report, err
		}
	}
	return report, nil
}

// Uninstall removes the owned links and the managed block. It needs only the
// checkout path, not its contents, so it still works after the checkout is
// deleted. Entries it does not own stay; registered names held by them are
// reported as "skip".
func Uninstall(o Options) (Report, error) {
	paths, err := resolve(o)
	if err != nil {
		return Report{}, err
	}
	report := Report{Instructions: paths.instructions}
	instructions, err := readInstructions(paths.instructions)
	if err != nil {
		return report, err
	}
	updated, removed, err := RemoveBlock(instructions.data)
	if err != nil {
		return report, fmt.Errorf("%s: %w", instructions.path, err)
	}
	owned, err := ownedLinks(paths.skills, paths.source)
	if err != nil {
		return report, err
	}
	for _, name := range sortedKeys(owned) {
		report.Changes = append(report.Changes, Change{Action: "unlink", Path: filepath.Join(paths.skills, name)})
	}
	if m, err := readManifest(paths.source); err == nil {
		for _, name := range sortedKeys(m.Registrations) {
			path := filepath.Join(paths.skills, name)
			if _, isOwned := owned[name]; !isOwned {
				if _, err := os.Lstat(path); err == nil {
					report.Changes = append(report.Changes, Change{Action: "skip", Path: path})
				}
			}
		}
	}
	for _, change := range report.Changes {
		if err := applyLink(change); err != nil {
			return report, err
		}
	}
	report.Block = "absent"
	if removed {
		report.Block = "removed"
		return report, instructions.save(updated)
	}
	return report, nil
}

func applyLink(change Change) error {
	switch change.Action {
	case "link":
		return os.Symlink(change.Target, change.Path)
	case "relink":
		if err := os.Remove(change.Path); err != nil {
			return err
		}
		return os.Symlink(change.Target, change.Path)
	case "unlink":
		return os.Remove(change.Path)
	}
	return nil
}

// instructionFile is AGENTS.md as read before any change. path is the file
// actually written: the symlink's target when AGENTS.md is a symlink, so the
// link itself survives.
type instructionFile struct {
	path   string
	data   []byte
	mode   fs.FileMode
	exists bool
}

func readInstructions(path string) (instructionFile, error) {
	file := instructionFile{path: path, mode: 0o644}
	// Follow the link chain by hand: unlike EvalSymlinks this also resolves a
	// dangling link, so writing its missing target keeps the link in place.
	for range 40 {
		info, err := os.Lstat(file.path)
		if err != nil || info.Mode()&fs.ModeSymlink == 0 {
			break
		}
		target, err := os.Readlink(file.path)
		if err != nil {
			return file, err
		}
		if !filepath.IsAbs(target) {
			target = filepath.Join(filepath.Dir(file.path), target)
		}
		file.path = target
	}
	info, err := os.Stat(file.path)
	if errors.Is(err, fs.ErrNotExist) {
		return file, nil
	}
	if err != nil {
		return file, err
	}
	if !info.Mode().IsRegular() {
		return file, fmt.Errorf("%w: %s is not a regular file", ErrNotOwned, path)
	}
	file.exists, file.mode = true, info.Mode().Perm()
	file.data, err = os.ReadFile(file.path)
	return file, err
}

// save replaces the file atomically, keeping its mode, or deletes it when data
// is empty. A new file is created with mode 0644 regardless of the umask.
func (f instructionFile) save(data []byte) error {
	if len(data) == 0 {
		return os.Remove(f.path)
	}
	if err := os.MkdirAll(filepath.Dir(f.path), 0o755); err != nil { //nolint:gosec // G301: the instruction file's directory keeps the usual 0755 of a home config directory.
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(f.path), ".AGENTS.md.*")
	if err != nil {
		return err
	}
	defer os.Remove(temp.Name())
	_, err = temp.Write(data)
	if err == nil {
		err = temp.Chmod(f.mode)
	}
	if err == nil {
		err = temp.Sync()
	}
	if closeErr := temp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(temp.Name(), f.path)
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}
