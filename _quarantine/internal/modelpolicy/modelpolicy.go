// Package modelpolicy reads the adviser portion of a task-orchestration model
// policy: the known consultation providers with their pinned models, and one
// policy's batch deadline and enabled providers. The runtime commands need
// only these fields, so the rest of the document is not validated here.
package modelpolicy

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"gopkg.in/yaml.v3"
)

// Version is the only policy schema version this cw reads.
const Version = 3

// MaxDeadlineSeconds bounds consultation.deadline_seconds; a batch never
// waits longer than an hour.
const MaxDeadlineSeconds = 3600

// maxDocumentBytes bounds a policy file; the shipped one is under 8 KiB.
const maxDocumentBytes = 1 << 20

const jevProvider = "jev"

var jevRelease = regexp.MustCompile(`^jev-[0-9]+\.[0-9]+\.[0-9]+$`)

// pinnedModels holds the one accepted model of each provider other than Jev,
// which accepts any exact jev-X.Y.Z release.
var pinnedModels = map[string]string{"subq_snap": "subq-snap-preview", "openai_decisions": "gpt-6-luna"}

// Providers returns the known consultation provider IDs (jev, subq_snap,
// openai_decisions) in their fixed record order. These are the only IDs a
// policy, a task binding or a consultation record may name. The result is a
// new slice on every call.
func Providers() []string { return []string{jevProvider, "subq_snap", "openai_decisions"} }

// Known reports whether provider is a known consultation provider ID.
func Known(provider string) bool { return slices.Contains(Providers(), provider) }

// PinnedModel returns the one accepted model of a provider that pins a single
// model: subq-snap-preview for subq_snap and gpt-6-luna for openai_decisions.
// ok is false for jev, which accepts any exact jev-X.Y.Z release, and for an
// unknown provider ID.
func PinnedModel(provider string) (model string, ok bool) {
	model, ok = pinnedModels[provider]
	return model, ok
}

// ValidModel applies the policy pin rules: it reports whether model is
// accepted for the known provider, an exact jev-X.Y.Z release for jev (aliases
// such as jev-latest are refused) or the provider's one pinned model
// otherwise. It is false for an unknown provider ID.
func ValidModel(provider, model string) bool {
	if provider == jevProvider {
		return jevRelease.MatchString(model)
	}
	pinned, known := pinnedModels[provider]
	return known && model == pinned
}

// Policy is the adviser portion of one validated policy.
type Policy struct {
	// Deadline bounds one consultation batch.
	Deadline time.Duration
	// Providers maps each enabled provider ID to its pinned model. It is
	// never nil; an empty map enables none.
	Providers map[string]string
}

// Source locates a policy: Path names a file, or, when Path is empty, Data is
// the document itself (the shipped policy embedded in cw).
type Source struct {
	Path string
	Data []byte
}

// Load reads the policy the source locates.
func (s Source) Load() (Policy, error) {
	if s.Path == "" {
		return Parse(s.Data)
	}
	return Load(s.Path)
}

// VersionError reports a policy whose schema version this cw does not read.
// Version is the value the policy declares, or nil when it has none.
type VersionError struct{ Version any }

func (e *VersionError) Error() string {
	if e.Version == nil {
		return fmt.Sprintf("model policy has no version; this cw requires version %d", Version)
	}
	// Show a non-integer version as written, so 3.0 or "3" never reads as 3.
	version := fmt.Sprint(e.Version)
	switch value := e.Version.(type) {
	case string:
		version = strconv.Quote(value)
	case float64:
		version = strconv.FormatFloat(value, 'f', -1, 64)
		if !strings.ContainsAny(version, ".IN") {
			version += ".0"
		}
	}
	return fmt.Sprintf("model policy version %s is not supported; this cw requires version %d", version, Version)
}

// Load reads and validates the policy file at path. A file that cannot be
// read, is larger than 1 MiB or is not UTF-8 is reported as unreadable.
func Load(path string) (Policy, error) {
	file, err := os.Open(path)
	if err != nil {
		return Policy{}, errors.New("cannot read model policy")
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxDocumentBytes+1))
	if err != nil || len(data) > maxDocumentBytes {
		return Policy{}, errors.New("cannot read model policy")
	}
	return Parse(data)
}

type document struct {
	Version      yaml.Node `yaml:"version"`
	Consultation yaml.Node `yaml:"consultation"`
}

type consultation struct {
	DeadlineSeconds yaml.Node `yaml:"deadline_seconds"`
	Providers       yaml.Node `yaml:"providers"`
}

// Parse validates one policy document. The version must be 3; consultation
// must hold deadline_seconds, an integer from 1 to MaxDeadlineSeconds, and
// providers, a mapping (possibly empty) from known provider IDs to their
// pinned models. Other fields are not read.
func Parse(data []byte) (Policy, error) {
	if !utf8.Valid(data) {
		return Policy{}, errors.New("cannot read model policy")
	}
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	var root yaml.Node
	if err := decoder.Decode(&root); err != nil {
		return Policy{}, invalidYAML(err)
	}
	if err := decoder.Decode(new(yaml.Node)); !errors.Is(err, io.EOF) {
		return Policy{}, errors.New("model policy must contain one YAML document")
	}
	if root.Kind != yaml.DocumentNode || len(root.Content) != 1 || root.Content[0].Kind != yaml.MappingNode {
		return Policy{}, errors.New("policy must be a mapping")
	}
	var policy document
	if err := root.Decode(&policy); err != nil {
		return Policy{}, invalidYAML(err)
	}
	if err := checkVersion(policy.Version); err != nil {
		return Policy{}, err
	}
	consultationNode := resolved(policy.Consultation)
	if consultationNode.Kind == 0 {
		return Policy{}, errors.New("policy does not configure adviser consultation")
	}
	if consultationNode.Kind != yaml.MappingNode {
		return Policy{}, errors.New("consultation must be a mapping")
	}
	var section consultation
	if err := consultationNode.Decode(&section); err != nil {
		return Policy{}, invalidYAML(err)
	}
	seconds, err := deadline(section.DeadlineSeconds)
	if err != nil {
		return Policy{}, err
	}
	providers, err := enabledProviders(section.Providers)
	if err != nil {
		return Policy{}, err
	}
	return Policy{Deadline: time.Duration(seconds) * time.Second, Providers: providers}, nil
}

func invalidYAML(err error) error { return fmt.Errorf("model policy is not valid YAML: %v", err) }

// resolved follows alias nodes (*anchor) to the node they name, so an aliased
// mapping passes the same kind checks as a literal one. The decoder has
// already refused alias cycles.
func resolved(node yaml.Node) yaml.Node {
	for node.Kind == yaml.AliasNode && node.Alias != nil {
		node = *node.Alias
	}
	return node
}

func checkVersion(node yaml.Node) error {
	if node.Kind == 0 {
		return &VersionError{}
	}
	var version any
	if err := node.Decode(&version); err != nil {
		return invalidYAML(err)
	}
	if value, ok := version.(int); !ok || value != Version {
		return &VersionError{Version: version}
	}
	return nil
}

func deadline(node yaml.Node) (int, error) {
	if node.Kind == 0 {
		return 0, errors.New(`consultation has invalid fields: missing "deadline_seconds"`)
	}
	var value any
	if err := node.Decode(&value); err != nil {
		return 0, invalidYAML(err)
	}
	seconds, ok := value.(int)
	if !ok || seconds < 1 || seconds > MaxDeadlineSeconds {
		return 0, fmt.Errorf("invalid consultation deadline: deadline_seconds must be an integer from 1 to %d", MaxDeadlineSeconds)
	}
	return seconds, nil
}

func enabledProviders(node yaml.Node) (map[string]string, error) {
	node = resolved(node)
	if node.Kind == 0 {
		return nil, errors.New(`consultation has invalid fields: missing "providers"`)
	}
	if node.Kind != yaml.MappingNode {
		return nil, errors.New("consultation providers must be a mapping")
	}
	var listed map[string]any
	if err := node.Decode(&listed); err != nil {
		return nil, invalidYAML(err)
	}
	providers := make(map[string]string, len(listed))
	for _, name := range slices.Sorted(maps.Keys(listed)) {
		if !Known(name) {
			return nil, fmt.Errorf("unknown consultation provider %q", name)
		}
		model, ok := listed[name].(string)
		if !ok {
			return nil, fmt.Errorf("consultation provider %q model must be a string", name)
		}
		if name == jevProvider && !ValidModel(name, model) {
			return nil, fmt.Errorf("consultation provider \"jev\" model %q is an alias or unpinned; use an exact jev-X.Y.Z release", model)
		}
		if pinned, single := PinnedModel(name); single && model != pinned {
			return nil, fmt.Errorf("consultation provider %q model %q is not supported; use %s", name, model, pinned)
		}
		providers[name] = model
	}
	return providers, nil
}
