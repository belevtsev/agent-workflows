package advisers

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/belevtsev/kitsu/v3/internal/roles"
	"github.com/pelletier/go-toml/v2"
)

var configName = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,39}$`)

type configDocument struct {
	Version     int                      `toml:"version"`
	Advisers    map[string]configAdviser `toml:"advisers"`
	Access      map[string][]string      `toml:"access"`
	Checkpoints []Checkpoint             `toml:"checkpoints"`
}

type configAdviser struct {
	Model   string   `toml:"model"`
	Command []string `toml:"command"`
	Timeout *string  `toml:"timeout"`
}

// Load combines personal definitions with this repository's definitions and
// policy. A repository definition replaces the entire personal definition.
func Load(root string) (*Config, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return nil, fmt.Errorf("locate personal adviser configuration: %w", err)
	}
	personal, err := loadConfig(filepath.Join(dir, "kitsu", "advisers.toml"), true)
	if err != nil {
		return nil, err
	}
	repository, err := loadConfig(filepath.Join(root, FileName), false)
	if err != nil {
		return nil, err
	}
	for name, definition := range repository.Advisers {
		personal.Advisers[name] = definition
	}
	personal.Access, personal.Checkpoints = repository.Access, repository.Checkpoints
	if err := ValidateConfig(personal); err != nil {
		return nil, fmt.Errorf("%s: %w", FileName, err)
	}
	return personal, nil
}

func loadConfig(path string, personal bool) (*Config, error) {
	file, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return &Config{Advisers: map[string]Definition{}}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read adviser configuration %s: %w", path, err)
	}
	defer func() { _ = file.Close() }()
	data, err := io.ReadAll(io.LimitReader(file, MaxInput+1))
	if err != nil {
		return nil, fmt.Errorf("read adviser configuration %s: %w", path, err)
	}
	cfg, err := Parse(data, personal)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	for name, definition := range cfg.Advisers {
		definition.Source = path
		cfg.Advisers[name] = definition
	}
	return cfg, nil
}

// Parse reads one strict version 1 TOML configuration. References to adviser
// definitions are checked after personal and repository configurations merge.
func Parse(data []byte, personal bool) (*Config, error) {
	if len(data) > MaxInput || !utf8.Valid(data) {
		return nil, errors.New("adviser configuration must be UTF-8 and at most 1 MiB")
	}
	var document configDocument
	decoder := toml.NewDecoder(bytes.NewReader(data)).DisallowUnknownFields()
	if err := decoder.Decode(&document); err != nil {
		return nil, fmt.Errorf("invalid adviser TOML: %w", err)
	}
	if document.Version != 1 {
		return nil, errors.New("adviser configuration version must be 1")
	}
	if personal {
		// An empty TOML table leaves the struct's map nil. Inspect top-level
		// keys as well so even an empty [access] is repository policy.
		var fields map[string]any
		if err := toml.Unmarshal(data, &fields); err != nil {
			return nil, fmt.Errorf("invalid adviser TOML: %w", err)
		}
		_, access := fields["access"]
		_, checkpoints := fields["checkpoints"]
		if access || checkpoints {
			return nil, errors.New("personal adviser configuration may hold only version and adviser definitions")
		}
	}
	cfg := &Config{Advisers: map[string]Definition{}, Access: document.Access, Checkpoints: document.Checkpoints}
	for _, name := range configKeys(document.Advisers) {
		raw := document.Advisers[name]
		definition := Definition{Model: raw.Model, Command: raw.Command, Timeout: DefaultTimeout}
		if raw.Timeout != nil {
			var err error
			definition.Timeout, err = time.ParseDuration(*raw.Timeout)
			if err != nil {
				return nil, fmt.Errorf("adviser %q timeout must be a duration from 1s to 1h", name)
			}
		}
		if !configName.MatchString(name) {
			return nil, fmt.Errorf("invalid adviser name %q; use 1 to 40 lowercase letters, digits, underscores or hyphens", name)
		}
		if err := validateDefinition(definition); err != nil {
			return nil, fmt.Errorf("adviser %q: %w", name, err)
		}
		cfg.Advisers[name] = definition
	}
	if err := validatePolicy(cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

func validateDefinition(d Definition) error {
	if strings.TrimSpace(d.Model) == "" || !utf8.ValidString(d.Model) || strings.ContainsRune(d.Model, 0) {
		return errors.New("model must be nonempty UTF-8 text without NUL")
	}
	if len(d.Command) < 2 || strings.TrimSpace(d.Command[0]) == "" {
		return errors.New("command must name a program and contain one literal {model} argument")
	}
	models, files := 0, 0
	for i, arg := range d.Command {
		if !utf8.ValidString(arg) || strings.ContainsRune(arg, 0) {
			return errors.New("command arguments must be UTF-8 without NUL")
		}
		if arg == "{model}" && i > 0 {
			models++
		} else if arg == "{answer_file}" && i > 0 {
			files++
		} else if strings.Contains(arg, "{model}") || strings.Contains(arg, "{answer_file}") {
			return errors.New("{model} and {answer_file} must each occupy a complete argument after the program")
		}
	}
	if models != 1 || files > 1 {
		return errors.New("command needs exactly one {model} argument and at most one {answer_file} argument")
	}
	if d.Timeout < time.Second || d.Timeout > time.Hour {
		return errors.New("timeout must be from 1s to 1h")
	}
	return nil
}

func validatePolicy(cfg *Config) error {
	for _, role := range configKeys(cfg.Access) {
		if role != "default" && !ValidRole(role) {
			return fmt.Errorf("unknown access role %q", role)
		}
		if err := uniqueAdvisers(cfg.Access[role]); err != nil {
			return fmt.Errorf("access %q: %w", role, err)
		}
	}
	seen := map[string]bool{}
	for _, checkpoint := range cfg.Checkpoints {
		if !configName.MatchString(checkpoint.ID) || seen[checkpoint.ID] {
			return fmt.Errorf("invalid or duplicate checkpoint id %q", checkpoint.ID)
		}
		seen[checkpoint.ID] = true
		if !slices.Contains([]string{"spec", "design", "review"}, checkpoint.Stage) {
			return fmt.Errorf("checkpoint %q stage must be spec, design or review", checkpoint.ID)
		}
		if !ValidRole(checkpoint.Role) {
			return fmt.Errorf("checkpoint %q has unknown role %q", checkpoint.ID, checkpoint.Role)
		}
		if len(checkpoint.Advisers) == 0 {
			return fmt.Errorf("checkpoint %q must name at least one adviser", checkpoint.ID)
		}
		if err := uniqueAdvisers(checkpoint.Advisers); err != nil {
			return fmt.Errorf("checkpoint %q: %w", checkpoint.ID, err)
		}
	}
	return nil
}

func uniqueAdvisers(names []string) error {
	seen := map[string]bool{}
	for _, name := range names {
		if !configName.MatchString(name) || seen[name] {
			return fmt.Errorf("invalid or duplicate adviser name %q", name)
		}
		seen[name] = true
	}
	return nil
}

// ValidateConfig checks a complete merged configuration, including every
// reference and whether each checkpoint's role may consult its advisers.
func ValidateConfig(cfg *Config) error {
	if cfg == nil {
		return errors.New("adviser configuration is nil")
	}
	if err := validatePolicy(cfg); err != nil {
		return err
	}
	for _, name := range configKeys(cfg.Advisers) {
		if !configName.MatchString(name) {
			return fmt.Errorf("invalid adviser name %q", name)
		}
		if err := validateDefinition(cfg.Advisers[name]); err != nil {
			return fmt.Errorf("adviser %q: %w", name, err)
		}
	}
	for _, role := range configKeys(cfg.Access) {
		for _, name := range cfg.Access[role] {
			if _, exists := cfg.Advisers[name]; !exists {
				return fmt.Errorf("access %q names undefined adviser %q", role, name)
			}
		}
	}
	for _, checkpoint := range cfg.Checkpoints {
		for _, name := range checkpoint.Advisers {
			if _, exists := cfg.Advisers[name]; !exists {
				return fmt.Errorf("checkpoint %q names undefined adviser %q", checkpoint.ID, name)
			}
			if !Allowed(cfg.Access, checkpoint.Role, name) {
				return fmt.Errorf("checkpoint %q: role %q is not allowed to consult %q", checkpoint.ID, checkpoint.Role, name)
			}
		}
	}
	return nil
}

// Fingerprint identifies a definition without including where it was loaded.
func Fingerprint(d Definition) string {
	data, _ := json.Marshal(struct {
		Model   string
		Command []string
		Timeout time.Duration
	}{d.Model, d.Command, d.Timeout})
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

// Allowed applies the role's explicit list, or the default list when absent.
func Allowed(access map[string][]string, role, adviser string) bool {
	if !ValidRole(role) {
		return false
	}
	list, exists := access[role]
	if !exists {
		list = access["default"]
	}
	return slices.Contains(list, adviser)
}

// ValidRole includes the coordinator and the seven specialist roles.
func ValidRole(role string) bool { return role == roles.Coordinator || roles.Known(role) }

func configKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for name := range m {
		keys = append(keys, name)
	}
	slices.Sort(keys)
	return keys
}
