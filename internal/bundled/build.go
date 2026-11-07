package bundled

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	json "encoding/json/v2"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

type resourceManifest struct {
	Version            int               `json:"version"`
	Registrations      map[string]string `json:"registrations"`
	GlobalInstructions string            `json:"global_instructions"`
	ClaudeAgents       map[string]string `json:"claude_agents,omitempty"`
	RequiredLicenses   []string          `json:"required_licenses"`
	ModelPolicy        string            `json:"model_policy"`
	ClaudeModelPolicy  string            `json:"claude_model_policy"`
	AdoptionCatalog    string            `json:"adoption_catalog"`
}

var registrationPattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

func decodeManifest(data []byte) (resourceManifest, error) {
	var manifest resourceManifest
	if len(data) == 0 || len(data) > 1<<20 {
		return manifest, errors.New("bundle has no bounded resource manifest")
	}
	if err := json.Unmarshal(data, &manifest, json.RejectUnknownMembers(true)); err != nil {
		return manifest, fmt.Errorf("invalid bundle resource manifest: %w", err)
	}
	if manifest.Version != 3 || len(manifest.Registrations) == 0 || len(manifest.RequiredLicenses) == 0 {
		return manifest, errors.New("unsupported or incomplete bundle resource manifest")
	}
	roots := make([]string, 0, len(manifest.Registrations))
	for name, root := range manifest.Registrations {
		if len(name) > 64 || !registrationPattern.MatchString(name) || !safeName(root) || excluded(root) || (!strings.HasPrefix(root, "skills/") && !strings.HasPrefix(root, "third_party/")) {
			return manifest, errors.New("bundle manifest contains an unsafe skill registration")
		}
		for _, previous := range roots {
			if root == previous || strings.HasPrefix(root, previous+"/") || strings.HasPrefix(previous, root+"/") {
				return manifest, errors.New("bundle manifest skill registrations overlap")
			}
		}
		roots = append(roots, root)
	}
	var fields map[string]any
	if err := json.Unmarshal(data, &fields); err != nil {
		return manifest, fmt.Errorf("invalid bundle resource manifest: %w", err)
	}
	if raw, present := fields["claude_agents"]; present {
		if agents, ok := raw.(map[string]any); !ok || len(agents) == 0 {
			return manifest, errors.New("bundle manifest Claude agent definitions must be a nonempty mapping")
		}
	}
	for name, file := range manifest.ClaudeAgents {
		if len(name) > 64 || !registrationPattern.MatchString(name) || file != "global/claude-agents/"+name+".md" {
			return manifest, errors.New("bundle manifest contains an unsafe Claude agent definition")
		}
	}
	for _, name := range manifest.requiredFiles() {
		if !safeName(name) || excluded(name) {
			return manifest, errors.New("bundle manifest contains an unsafe required resource")
		}
	}
	return manifest, nil
}

func (manifest resourceManifest) requiredFiles() []string {
	files := []string{"skills-manifest.json", manifest.GlobalInstructions, manifest.ModelPolicy, manifest.ClaudeModelPolicy, manifest.AdoptionCatalog}
	for _, name := range slices.Sorted(maps.Keys(manifest.ClaudeAgents)) {
		files = append(files, manifest.ClaudeAgents[name])
	}
	return append(files, manifest.RequiredLicenses...)
}

func (manifest resourceManifest) allows(name string) bool {
	if slices.Contains(manifest.requiredFiles(), name) || name == "LICENSE" || name == "THIRD_PARTY.md" || name == "README.md" || name == "go.mod" || name == "third_party/personal-skill-imports.json" || strings.HasPrefix(name, "docs/") || strings.HasPrefix(name, "licenses/") || strings.HasPrefix(name, "global/") {
		return true
	}
	for _, root := range manifest.Registrations {
		if strings.HasPrefix(name, root+"/") {
			return true
		}
	}
	return false
}

func excluded(name string) bool {
	parts := strings.Split(name, "/")
	asset := false
	for _, part := range parts {
		asset = asset || part == "assets"
		switch strings.ToLower(part) {
		case ".git", ".github", ".codex", ".claude", ".agents", ".private", ".bin", ".bin.lock", "node_modules", "__pycache__", ".venv", ".pytest_cache", ".mypy_cache", ".ruff_cache", "dist":
			return true
		case "credentials", "auth", "preferences", "cache", ".cache", "fixture":
			return true
		case "test", "tests", "testdata", "fixtures", "goldens":
			if !asset {
				return true
			}
		}
	}
	base := path.Base(name)
	return base == ".env" || strings.HasPrefix(base, ".env.") || base == ".DS_Store" || base == "auth.json" || base == "credentials.json" || base == "preferences.json" || base == "config.toml" || strings.HasSuffix(base, ".pyc") || strings.HasSuffix(base, ".pyo") || strings.HasSuffix(base, ".pem") || strings.HasSuffix(base, ".key") || (!asset && (strings.HasSuffix(base, "_test.go") || strings.Contains(base, ".test.")))
}

// Build creates a deterministic resource pack from a caller-owned exact source
// export. Only manifest registrations, portable global resources and notices
// are included; unrelated repository code and development tests are omitted.
func Build(root, version, revision string) ([]byte, error) {
	if !versionPattern.MatchString(version) || !revisionPattern.MatchString(revision) {
		return nil, errors.New("bundle requires a version token and exact source revision")
	}
	info, err := os.Lstat(root)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("bundle source must be a regular directory")
	}
	manifestBytes, err := os.ReadFile(filepath.Join(root, "skills-manifest.json"))
	if err != nil {
		return nil, err
	}
	manifest, err := decodeManifest(manifestBytes)
	if err != nil {
		return nil, err
	}
	names := map[string]bool{}
	add := func(name string, optional bool) error {
		target := filepath.Join(root, filepath.FromSlash(name))
		if _, err := os.Lstat(target); errors.Is(err, fs.ErrNotExist) && optional {
			return nil
		} else if err != nil {
			return err
		}
		return filepath.WalkDir(target, func(current string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			relative, err := filepath.Rel(root, current)
			if err != nil {
				return err
			}
			relative = filepath.ToSlash(relative)
			if excluded(relative) {
				if entry.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if !safeName(relative) || entry.Type()&os.ModeSymlink != 0 {
				return fmt.Errorf("unsafe bundle resource: %s", relative)
			}
			if entry.IsDir() {
				return nil
			}
			if !manifest.allows(relative) || !entry.Type().IsRegular() {
				return fmt.Errorf("unsupported bundle resource: %s", relative)
			}
			names[relative] = true
			if len(names) > maxFiles {
				return errors.New("bundle exceeds file count limit")
			}
			return nil
		})
	}
	for _, name := range manifest.requiredFiles() {
		if err := add(name, false); err != nil {
			return nil, err
		}
	}
	// The provenance record is a transitive notice resource linked directly
	// from THIRD_PARTY.md; it is source metadata, never device personal state.
	for _, name := range []string{"global", "licenses", "LICENSE", "THIRD_PARTY.md", "README.md", "go.mod", "docs", "third_party/personal-skill-imports.json"} {
		if err := add(name, true); err != nil {
			return nil, err
		}
	}
	for _, name := range manifest.Registrations {
		if err := add(name, false); err != nil {
			return nil, err
		}
	}
	ordered := make([]string, 0, len(names))
	for name := range names {
		ordered = append(ordered, name)
	}
	slices.Sort(ordered)
	metadata := packMetadata{Format: 1, Version: version, Revision: revision}
	contents := make(map[string][]byte, len(ordered))
	var total int64
	for _, name := range ordered {
		filename := filepath.Join(root, filepath.FromSlash(name))
		info, err := os.Lstat(filename)
		if err != nil || !info.Mode().IsRegular() || info.Size() > maxContentBytes-total {
			return nil, fmt.Errorf("bundle resource is unsafe or exceeds size limit: %s", name)
		}
		data, err := os.ReadFile(filename)
		if err != nil || int64(len(data)) != info.Size() {
			return nil, errors.Join(fmt.Errorf("cannot read stable bundle resource: %s", name), err)
		}
		mode := int64(0o644)
		if info.Mode().Perm()&0o111 != 0 {
			mode = 0o755
		}
		metadata.Files = append(metadata.Files, packFile{Path: name, Mode: mode, Size: int64(len(data)), SHA256: payloadDigest(data)})
		contents[name] = data
		total += int64(len(data))
	}
	var output bytes.Buffer
	zipped, err := gzip.NewWriterLevel(&output, gzip.BestCompression)
	if err != nil {
		return nil, err
	}
	zipped.Header.OS = 255
	writer := tar.NewWriter(zipped)
	write := func(name string, mode int64, data []byte) error {
		if err := writer.WriteHeader(&tar.Header{Name: name, Mode: mode, Size: int64(len(data)), Typeflag: tar.TypeReg, Format: tar.FormatPAX}); err != nil {
			return err
		}
		_, err := writer.Write(data)
		return err
	}
	encoded, err := json.Marshal(metadata, json.Deterministic(true))
	if err != nil {
		return nil, err
	}
	if err := write(metadataName, 0o644, encoded); err != nil {
		return nil, err
	}
	for _, file := range metadata.Files {
		if err := write(file.Path, file.Mode, contents[file.Path]); err != nil {
			return nil, err
		}
	}
	if err := errors.Join(writer.Close(), zipped.Close()); err != nil {
		return nil, err
	}
	data := output.Bytes()
	if _, err := Open(data); err != nil {
		return nil, err
	}
	return data, nil
}
