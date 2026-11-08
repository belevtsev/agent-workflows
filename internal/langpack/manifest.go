package langpack

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"

	"gopkg.in/yaml.v3"
)

// SchemaVersion is the pack.yaml schema this cw reads. A newer schema is
// refused rather than read partially.
const SchemaVersion = 1

// Pack kinds. A language pack applies to one language's source; a domain pack
// to a problem domain. Both use one manifest format.
const (
	KindLanguage = "language"
	KindDomain   = "domain"
)

// Manifest is one pack.yaml document (schema 1). Unknown keys are refused at
// every level. Optional lists are nil when absent.
type Manifest struct {
	Schema           int      `yaml:"schema"`
	ID               string   `yaml:"id"`
	Kind             string   `yaml:"kind"`
	Detect           []string `yaml:"detect"`
	Commands         Commands `yaml:"commands"`
	Tests            Tests    `yaml:"tests"`
	InterfaceExtract []string `yaml:"interface_extract"`
	Skills           Skills   `yaml:"skills"`
	Review           []string `yaml:"review"`
	EvidenceRule     string   `yaml:"evidence_rule"`
}

// Commands are the pack's command slots, each an argv.
type Commands struct {
	Build  []string `yaml:"build"`
	Test   []string `yaml:"test"`
	Race   []string `yaml:"race"`
	Lint   []string `yaml:"lint"`
	Format []string `yaml:"format"`
}

// Tests are the pack's test kinds; a nil kind is absent.
type Tests struct {
	Unit        *TestCommand `yaml:"unit"`
	Integration *TestCommand `yaml:"integration"`
	E2E         *TestCommand `yaml:"e2e"`
}

// TestCommand names the files a test kind covers and the argv that runs it.
type TestCommand struct {
	Files []string `yaml:"files"`
	Argv  []string `yaml:"argv"`
}

// Skills are skill references by task role: a bare skill name, or
// plugin:skill.
type Skills struct {
	Implement []string `yaml:"implement"`
	Test      []string `yaml:"test"`
	Security  []string `yaml:"security"`
}

// ParseManifest decodes and validates one pack.yaml document. The schema is
// read before any other field: a missing or invalid schema, or one newer than
// SchemaVersion, is an error regardless of the other keys.
func ParseManifest(data []byte) (Manifest, error) {
	if len(data) > maxManifestBytes {
		return Manifest{}, errors.New("pack manifest is larger than 64 KiB")
	}
	if !utf8.Valid(data) {
		return Manifest{}, errors.New("pack manifest is not UTF-8")
	}
	documents := yaml.NewDecoder(bytes.NewReader(data))
	var document yaml.Node
	if err := documents.Decode(&document); err != nil && !errors.Is(err, io.EOF) {
		return Manifest{}, fmt.Errorf("invalid pack manifest: %w", err)
	}
	if len(document.Content) != 1 || document.Content[0].Kind != yaml.MappingNode {
		return Manifest{}, errors.New("pack manifest is not a YAML mapping")
	}
	schema, err := readSchema(document.Content[0])
	if err != nil {
		return Manifest{}, err
	}
	if err := documents.Decode(&yaml.Node{}); !errors.Is(err, io.EOF) {
		return Manifest{}, errors.New("pack manifest holds more than one YAML document")
	}
	strict := yaml.NewDecoder(bytes.NewReader(data))
	strict.KnownFields(true)
	var manifest Manifest
	if err := strict.Decode(&manifest); err != nil {
		return Manifest{}, fmt.Errorf("invalid pack manifest: %w", err)
	}
	manifest.Schema = schema
	return manifest, validateManifest(manifest)
}

const (
	maxManifestBytes = 64 << 10
	maxDetect        = 16
	maxReviewBytes   = 512
)

var (
	skillPattern = regexp.MustCompile(`^([a-z0-9][a-z0-9-]*:)?[a-z0-9][a-z0-9-]*$`)
	rulePattern  = regexp.MustCompile(`^[a-z][a-z0-9-]*(\.[a-z][a-z0-9-]*)+/v[1-9][0-9]*$`)
	idPattern    = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)
)

// readSchema reads the schema key of a manifest mapping before anything else
// is decoded, so a newer schema is refused whatever its other keys hold.
func readSchema(mapping *yaml.Node) (int, error) {
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value != "schema" {
			continue
		}
		value := mapping.Content[i+1]
		var schema int
		if value.Kind != yaml.ScalarNode || value.ShortTag() != "!!int" || value.Decode(&schema) != nil {
			return 0, fmt.Errorf("pack manifest schema %q is not an integer", value.Value)
		}
		if schema > SchemaVersion {
			return 0, fmt.Errorf("pack manifest schema %d is newer than schema %d, which this cw reads", schema, SchemaVersion)
		}
		if schema < 1 {
			return 0, fmt.Errorf("pack manifest schema %d is not supported", schema)
		}
		return schema, nil
	}
	return 0, errors.New("pack manifest has no schema")
}

// namedList is a manifest list with the field name its errors report.
type namedList struct {
	field string
	list  []string
}

// validateManifest applies the schema-1 field rules to a decoded manifest.
func validateManifest(m Manifest) error {
	if !ValidID(m.ID) {
		return fmt.Errorf("invalid pack id %q", m.ID)
	}
	if m.Kind != KindLanguage && m.Kind != KindDomain {
		return fmt.Errorf("pack kind %q must be %s or %s", m.Kind, KindLanguage, KindDomain)
	}
	if len(m.Detect) == 0 || len(m.Detect) > maxDetect {
		return fmt.Errorf("detect must list 1 to %d file names", maxDetect)
	}
	for i, name := range m.Detect {
		if name == "" || name == "." || name == ".." || strings.ContainsAny(name, "/\\\x00") {
			return fmt.Errorf("detect entry %q is not a file name", name)
		}
		if slices.Contains(m.Detect[:i], name) {
			return fmt.Errorf("detect lists %q twice", name)
		}
	}
	argvs := []namedList{
		{"commands.build", m.Commands.Build}, {"commands.test", m.Commands.Test}, {"commands.race", m.Commands.Race},
		{"commands.lint", m.Commands.Lint}, {"commands.format", m.Commands.Format}, {"interface_extract", m.InterfaceExtract},
	}
	for _, kind := range []struct {
		name string
		test *TestCommand
	}{{"unit", m.Tests.Unit}, {"integration", m.Tests.Integration}, {"e2e", m.Tests.E2E}} {
		if kind.test == nil {
			continue
		}
		if kind.test.Argv == nil {
			return fmt.Errorf("tests.%s has no argv", kind.name)
		}
		argvs = append(argvs, namedList{"tests." + kind.name + ".argv", kind.test.Argv})
		for _, pattern := range kind.test.Files {
			if _, err := path.Match(pattern, ""); pattern == "" || err != nil {
				return fmt.Errorf("tests.%s.files entry %q is not a valid pattern", kind.name, pattern)
			}
		}
	}
	for _, slot := range argvs {
		if slot.list != nil && (len(slot.list) == 0 || slices.Contains(slot.list, "")) {
			return fmt.Errorf("%s must be a non-empty argv without empty arguments", slot.field)
		}
	}
	for _, slot := range []namedList{{"skills.implement", m.Skills.Implement}, {"skills.test", m.Skills.Test}, {"skills.security", m.Skills.Security}} {
		for i, skill := range slot.list {
			if !skillPattern.MatchString(skill) {
				return fmt.Errorf("%s entry %q is not a skill reference", slot.field, skill)
			}
			if slices.Contains(slot.list[:i], skill) {
				return fmt.Errorf("%s lists %q twice", slot.field, skill)
			}
		}
	}
	for i, item := range m.Review {
		if strings.TrimSpace(item) == "" || len(item) > maxReviewBytes || strings.Contains(item, "\n") {
			return fmt.Errorf("review item %d must be one non-blank line of at most %d bytes", i+1, maxReviewBytes)
		}
	}
	if m.EvidenceRule != "" && !rulePattern.MatchString(m.EvidenceRule) {
		return fmt.Errorf("evidence_rule %q is not a rule id", m.EvidenceRule)
	}
	return nil
}

// LoadManifests parses every packs/*/pack.yaml in fsys and returns them sorted
// by ID. Any invalid file fails the whole load with an error that starts with
// that file's path.
func LoadManifests(fsys fs.FS) ([]Manifest, error) {
	names, err := fs.Glob(fsys, "packs/*/pack.yaml")
	if err != nil {
		return nil, err
	}
	manifests := []Manifest{}
	for _, name := range names {
		data, err := fs.ReadFile(fsys, name)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		manifest, err := ParseManifest(data)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		if directory := path.Base(path.Dir(name)); manifest.ID != directory {
			return nil, fmt.Errorf("%s: pack id %q does not match its directory %q", name, manifest.ID, directory)
		}
		manifests = append(manifests, manifest)
	}
	slices.SortFunc(manifests, func(a, b Manifest) int { return strings.Compare(a.ID, b.ID) })
	return manifests, nil
}

// ValidID reports whether id is a valid pack id: ^[a-z][a-z0-9-]{0,31}$.
func ValidID(id string) bool { return idPattern.MatchString(id) }

// DefaultSelection is R-DEFAULT-SELECTION: the packs selected when the user
// has no packs.json. It returns, in input order, every manifest of kind
// language; a domain pack is never selected by default.
func DefaultSelection(builtIn []Manifest) []Manifest {
	selected := []Manifest{}
	for _, manifest := range builtIn {
		if manifest.Kind == KindLanguage {
			selected = append(selected, manifest)
		}
	}
	return selected
}

// Applied is a selected pack detected at the selected path: the detect entry
// that matched and the manifest's evidence rule.
type Applied struct {
	ID           string
	Detected     string
	EvidenceRule string
}

// Applicable returns, in selected order, one Applied per manifest whose detect
// list names an element of current (top-level file names of the selected
// path). Detected is the first matching entry in detect order.
func Applicable(selected []Manifest, current []string) []Applied {
	applied := []Applied{}
	for _, manifest := range selected {
		index := slices.IndexFunc(manifest.Detect, func(name string) bool {
			return !strings.Contains(name, "/") && slices.Contains(current, name)
		})
		if index >= 0 {
			applied = append(applied, Applied{ID: manifest.ID, Detected: manifest.Detect[index], EvidenceRule: manifest.EvidenceRule})
		}
	}
	return applied
}
