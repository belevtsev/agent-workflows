package skills

import (
	"cmp"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
)

const maxDirectories = 4096

type source struct {
	path, base, scope, namespace, reason string
}

type scanner struct {
	catalog Catalog
	seen    map[string]bool
	dirs    int
	home    string
	hostDir string
}

// Discover reads repository, personal and installed plugin skills for one host.
// root is the repository root; an empty root requests personal inventory only.
// When the current directory is inside root, its ancestor skill roots are read.
func Discover(root, host string) (Catalog, error) {
	return DiscoverWithRoots(root, host, nil)
}

// DiscoverWithRoots also reads explicit skill collections or individual skill
// directories. These sources do not imply host installation or enabled state.
func DiscoverWithRoots(root, host string, extraRoots []string) (Catalog, error) {
	if host != "codex" && host != "claude" {
		return Catalog{}, fmt.Errorf("unknown skill host %q; use codex or claude", host)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return Catalog{}, fmt.Errorf("locate personal skills: %w", err)
	}
	if root != "" {
		root, err = filepath.Abs(root)
		if err != nil {
			return Catalog{}, fmt.Errorf("locate repository skills: %w", err)
		}
	}
	s := scanner{
		catalog: Catalog{Skills: []Entry{}, Warnings: []string{}, host: host, overrides: map[string]string{}},
		seen:    map[string]bool{}, home: home,
	}
	sources := s.hostSources(root, host)
	for _, path := range extraRoots {
		absolute, err := filepath.Abs(path)
		if err != nil {
			return Catalog{}, fmt.Errorf("locate explicit skill root: %w", err)
		}
		sources = append(sources, source{path: absolute, base: absolute, scope: "explicit"})
	}
	for _, src := range sources {
		s.scan(src, src.path, 0, map[string]bool{})
	}
	slices.SortFunc(s.catalog.Skills, func(a, b Entry) int { return cmp.Compare(a.ID, b.ID) })
	slices.Sort(s.catalog.Warnings)
	s.catalog.Warnings = slices.Compact(s.catalog.Warnings)
	return s.catalog, nil
}

func (s *scanner) scan(src source, dir string, depth int, visited map[string]bool) {
	if s.dirs >= maxDirectories || depth > 12 {
		s.warn(dir, "skill scan limit reached")
		return
	}
	key := realPath(dir)
	if visited[key] {
		return
	}
	visited[key] = true
	s.dirs++
	path := filepath.Join(dir, "SKILL.md")
	if _, err := os.Stat(path); err == nil {
		s.add(src, path)
		return
	} else if !missing(err) {
		s.warn(path, "cannot inspect skill file")
		return
	}
	entries, err := readDirs(dir)
	if err != nil {
		if !missing(err) || src.scope == "explicit" {
			s.warn(dir, "cannot read skill directory")
		}
		return
	}
	for _, entry := range entries {
		if s.dirs >= maxDirectories {
			s.warn(dir, "skill scan limit reached")
			return
		}
		if entry.Name() == ".git" || entry.Name() == "node_modules" {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		info, err := os.Stat(path)
		if err == nil && info.IsDir() {
			s.scan(src, path, depth+1, visited)
		} else if err != nil && entry.Type()&os.ModeSymlink != 0 {
			s.warn(path, "cannot inspect symlinked skill directory")
		}
	}
}

func (s *scanner) add(src source, path string) {
	key := realPath(path)
	if s.seen[key] {
		return
	}
	s.seen[key] = true
	entry, err := readSkill(path, s.catalog.host)
	rel, relErr := filepath.Rel(src.base, filepath.Dir(path))
	if relErr != nil {
		s.warn(path, "cannot identify skill directory")
		return
	}
	entry.Scope = src.scope
	identity := filepath.ToSlash(rel)
	if src.namespace != "" {
		identity = src.namespace + ":" + identity
	}
	entry.ID = entry.Host + ":" + entry.Scope + ":" + identity
	if src.scope == "explicit" {
		entry.ID = explicitID(entry)
	}
	if err != nil {
		entry.Disabled, entry.Reason = true, err.Error()
		s.warn(path, err.Error())
	}
	if src.reason != "" {
		entry.Disabled, entry.Reason = true, src.reason
	}
	s.catalog.Skills = append(s.catalog.Skills, s.catalog.applyPolicy(entry))
}

func readDirs(path string) ([]os.DirEntry, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	entries, err := file.ReadDir(maxDirectories + 1)
	if len(entries) > maxDirectories {
		return nil, fmt.Errorf("directory exceeds %d entries", maxDirectories)
	}
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	slices.SortFunc(entries, func(a, b os.DirEntry) int { return cmp.Compare(a.Name(), b.Name()) })
	return entries, nil
}

func (s *scanner) warn(path, reason string) {
	s.catalog.Warnings = append(s.catalog.Warnings, path+": "+reason)
}
