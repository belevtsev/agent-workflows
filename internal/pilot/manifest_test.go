// Package pilot is an explicitly invoked experiment harness, not production
// Kitsu code. Test-only placement preserves the production execution boundary.
package pilot

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math/rand"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

type asset struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

type homeFile struct {
	Source      string `json:"source"`
	Destination string `json:"destination"`
	SHA256      string `json:"sha256"`
}

type command struct {
	Argv           []string `json:"argv"`
	TimeoutSeconds int      `json:"timeout_seconds"`
}

type pilotHost struct {
	Argv      []string          `json:"argv"`
	Env       map[string]string `json:"env"`
	PassEnv   []string          `json:"pass_env"`
	HomeFiles []homeFile        `json:"home_files"`
	Rollouts  string            `json:"rollouts"`
	Assets    []asset           `json:"assets"`
}

type pilotGrader struct {
	Argv    []string          `json:"argv"`
	Assets  []asset           `json:"assets"`
	Env     map[string]string `json:"env,omitempty"`
	PassEnv []string          `json:"pass_env,omitempty"`
}

type pilotCase struct {
	ID             string          `json:"id"`
	Snapshot       string          `json:"snapshot"`
	SnapshotSHA256 string          `json:"snapshot_sha256"`
	Prompt         asset           `json:"prompt"`
	Provenance     json.RawMessage `json:"provenance"`
	Prepare        []command       `json:"prepare,omitempty"`
}

type manifest struct {
	Version            int              `json:"version"`
	Profile            string           `json:"profile,omitempty"`
	Budget             *canaryBudget    `json:"captured_token_budget,omitempty"`
	ID                 string           `json:"id"`
	Topology           string           `json:"topology"`
	Model              string           `json:"model"`
	Effort             string           `json:"effort"`
	Seed               int64            `json:"seed"`
	Repeats            int              `json:"repeats"`
	Controllers        int              `json:"controllers"`
	TimeoutSeconds     int              `json:"timeout_seconds"`
	CoordinatorPrompt  asset            `json:"coordinator_prompt"`
	ObservationPrompts map[string]asset `json:"observation_prompts"`
	Host               pilotHost        `json:"host"`
	Prepare            []command        `json:"prepare"`
	Grader             pilotGrader      `json:"grader"`
	Calibration        asset            `json:"calibration"`
	Cases              []pilotCase      `json:"cases"`
}

type cell struct {
	ID         string `json:"id"`
	Order      int    `json:"order"`
	CaseID     string `json:"case_id"`
	Repeat     int    `json:"repeat"`
	Arm        string `json:"arm"`
	Controller int    `json:"controller"`
}

type plan struct {
	Version        int    `json:"version"`
	ManifestSHA256 string `json:"manifest_sha256"`
	Runs           []cell `json:"runs"`
}

var slug = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

func readJSON(path string, into any) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	d := json.NewDecoder(io.LimitReader(f, 8<<20))
	d.DisallowUnknownFields()
	if err := d.Decode(into); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		return fmt.Errorf("%s: expected exactly one JSON object", path)
	}
	return nil
}

func hashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return "", fmt.Errorf("not a regular asset: %s", path)
	}
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func snapshotHash(ctx context.Context, root string) (string, error) {
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		return "", fmt.Errorf("public snapshot is not a directory: %s", root)
	}
	h := sha256.New()
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if path == root {
			return nil
		}
		if entry.Name() == ".git" || entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("public snapshot contains history or a link: %s", path)
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() {
			return fmt.Errorf("public snapshot contains a non-regular file: %s", path)
		}
		rel, _ := filepath.Rel(root, path)
		fmt.Fprintf(h, "%s\x00%d\x00", filepath.ToSlash(rel), info.Mode().Perm()&0o111)
		digest, err := hashFile(path)
		if err != nil {
			return err
		}
		fmt.Fprintln(h, digest)
		return nil
	})
	return hex.EncodeToString(h.Sum(nil)), err
}

func sealAsset(a *asset, base string) error {
	if !filepath.IsAbs(a.Path) {
		a.Path = filepath.Join(base, a.Path)
	}
	a.Path = filepath.Clean(a.Path)
	hash, err := hashFile(a.Path)
	if err != nil {
		return err
	}
	if a.SHA256 != "" && a.SHA256 != hash {
		return fmt.Errorf("asset hash changed: %s", a.Path)
	}
	a.SHA256 = hash
	return nil
}

func sealManifest(ctx context.Context, m *manifest, base string) error {
	if m.Controllers == 0 {
		m.Controllers = 1
	}
	if m.Controllers < 1 || m.Controllers > 6 {
		return errors.New("controllers must be between one and six")
	}
	if !validStudyProfile(*m) && !validCanaryProfile(*m) {
		return errors.New("pilot requires the original 12-case study profile or explicit selective-canary-v1 with two cases, one repeat, Sol/high, one controller, 480 seconds and a captured-token reserve policy")
	}
	if len(m.Host.Argv) == 0 || !filepath.IsAbs(m.Host.Argv[0]) || len(m.Grader.Argv) == 0 || !filepath.IsAbs(m.Grader.Argv[0]) {
		return errors.New("host and grader argv require absolute executable paths")
	}
	if !filepath.IsLocal(m.Host.Rollouts) || filepath.ToSlash(filepath.Clean(m.Host.Rollouts)) != filepath.ToSlash(m.Host.Rollouts) {
		return errors.New("host.rollouts must be a clean relative path under the fresh home")
	}
	if len(m.ObservationPrompts) != 2 {
		return errors.New("exactly normal and compact observation prompts are required")
	}
	for _, arm := range []string{"normal", "compact"} {
		a, ok := m.ObservationPrompts[arm]
		if !ok {
			return fmt.Errorf("missing %s observation prompt", arm)
		}
		if err := sealAsset(&a, base); err != nil {
			return err
		}
		m.ObservationPrompts[arm] = a
	}
	for _, a := range []*asset{&m.CoordinatorPrompt, &m.Calibration} {
		if err := sealAsset(a, base); err != nil {
			return err
		}
	}
	for _, executable := range []struct {
		path   string
		assets *[]asset
	}{{m.Host.Argv[0], &m.Host.Assets}, {m.Grader.Argv[0], &m.Grader.Assets}} {
		found := false
		for _, a := range *executable.assets {
			found = found || a.Path == executable.path
		}
		if !found {
			*executable.assets = append(*executable.assets, asset{Path: executable.path})
		}
	}
	for _, assets := range [][]asset{m.Host.Assets, m.Grader.Assets} {
		for i := range assets {
			if err := sealAsset(&assets[i], base); err != nil {
				return err
			}
		}
	}
	for i := range m.Host.HomeFiles {
		f := &m.Host.HomeFiles[i]
		if !filepath.IsLocal(f.Destination) || filepath.Clean(f.Destination) != f.Destination {
			return fmt.Errorf("unsafe home file destination: %s", f.Destination)
		}
		a := asset{f.Source, f.SHA256}
		if err := sealAsset(&a, base); err != nil {
			return err
		}
		f.Source, f.SHA256 = a.Path, a.SHA256
	}
	seen := map[string]bool{}
	for i := range m.Cases {
		c := &m.Cases[i]
		if !slug.MatchString(c.ID) || seen[c.ID] || len(c.Provenance) == 0 {
			return fmt.Errorf("case requires a unique slug and provenance: %q", c.ID)
		}
		seen[c.ID] = true
		if !filepath.IsAbs(c.Snapshot) {
			c.Snapshot = filepath.Join(base, c.Snapshot)
		}
		if err := sealAsset(&c.Prompt, base); err != nil {
			return err
		}
		digest, err := snapshotHash(ctx, c.Snapshot)
		if err != nil {
			return err
		}
		if c.SnapshotSHA256 != "" && digest != c.SnapshotSHA256 {
			return fmt.Errorf("public snapshot changed: %s", c.ID)
		}
		c.SnapshotSHA256 = digest
	}
	return nil
}

func validStudyProfile(m manifest) bool {
	return m.Profile == "" && m.Budget == nil && m.Version == 1 && slug.MatchString(m.ID) && m.Topology == "kitsu-small-path" && m.Model == "gpt-6.1-sol" &&
		(m.Effort == "high" || m.Effort == "max") && (m.Repeats == 1 || m.Repeats == 3) && len(m.Cases) == 12 && m.TimeoutSeconds > 0
}

func schedule(m manifest, digest string) plan {
	if m.Controllers == 0 {
		m.Controllers = 1
	}
	rng := rand.New(rand.NewSource(m.Seed)) // fixed experiment seed; no security decision
	p := plan{Version: 1, ManifestSHA256: digest}
	for repeat := 1; repeat <= m.Repeats; repeat++ {
		for _, i := range rng.Perm(len(m.Cases)) {
			arms := []string{"normal", "compact"}
			if rng.Intn(2) == 1 {
				arms[0], arms[1] = arms[1], arms[0]
			}
			for _, arm := range arms {
				id := fmt.Sprintf("%s-r%d-%s", m.Cases[i].ID, repeat, arm)
				p.Runs = append(p.Runs, cell{id, len(p.Runs) + 1, m.Cases[i].ID, repeat, arm, (len(p.Runs) / 2) % m.Controllers})
			}
		}
	}
	return p
}

func writeJSON(path string, value any, immutable bool) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if immutable {
		if existing, err := os.ReadFile(path); err == nil {
			if string(existing) != string(data) {
				return fmt.Errorf("refusing to replace immutable %s", path)
			}
			return nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".pilot-*")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Rename(name, path); err != nil {
		return err
	}
	if immutable {
		return os.Chmod(path, 0o400)
	}
	return nil
}

func replacements(value string, vars map[string]string) string {
	for _, key := range []string{"worktree", "home", "run_dir", "grade_request", "task", "case_id"} {
		value = strings.ReplaceAll(value, "{"+key+"}", vars[key])
	}
	return value
}
