package workflow

import (
	"bytes"
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"math"
	"math/big"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"gopkg.in/yaml.v3"
)

var (
	suiteName        = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)
	suiteDrive       = regexp.MustCompile(`^[A-Za-z]:`)
	suiteWindowsPath = regexp.MustCompile(`^[A-Za-z]:[\\/]`)
	suitePlaceholder = regexp.MustCompile(`(?i)\{[a-z][a-z0-9_-]*\}`)
	suiteComments    = regexp.MustCompile(`(?s)<!--.*?-->`)
	suiteFence       = regexp.MustCompile("^ {0,3}(`{3,}|~{3,})")
	suiteList        = regexp.MustCompile(`^( *)(?:[-+*]|[0-9]+[.)])\s+`)
	suiteLinkEnd     = regexp.MustCompile(`^\s*(?:"[^"]*"|'[^']*'|\([^)]*\))?\s*\)`)
	suiteDefinition  = regexp.MustCompile(`(?m)^ {0,3}\[[^\]\n]+\]:\s*(?:<([^>\n]+)>|(\S+))`)
	suiteEscapes     = regexp.MustCompile("\\\\([\\\\`*_{}\\[\\]()#+.!<> -])")
	policyJev        = regexp.MustCompile(`^jev-[0-9]+\.[0-9]+\.[0-9]+$`)
	suiteSexagesimal = regexp.MustCompile(`^[+-]?(?:[1-9][0-9_]*(?::[0-5]?[0-9])+|[0-9][0-9_]*(?::[0-5]?[0-9])+\.[0-9_]*)$`)
)

var suitePrivateDirectories = map[string]bool{
	".bin": true, ".bin.lock": true, "dist": true,
	".agents": true, ".codex": true, ".private": true, ".baseline": true,
	"private-baseline": true, "baseline-private": true, "__pycache__": true,
	".pytest_cache": true, ".mypy_cache": true, ".ruff_cache": true, ".venv": true, "node_modules": true,
}

var suiteProjectDocuments = map[string]bool{
	"LICENSE": true, "LICENSE.md": true, "LICENSE.txt": true, "CONTRIBUTING.md": true,
	"CODE_OF_CONDUCT.md": true, "CHANGELOG.md": true, "SECURITY.md": true, "README.md": true,
}

func suiteMapping(value any, name string, fields ...string) (Object, error) {
	object, ok := value.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s must be a mapping", name)
	}
	if len(fields) > 0 {
		if len(object) != len(fields) {
			return nil, fmt.Errorf("%s has invalid fields", name)
		}
		for _, field := range fields {
			if _, ok := object[field]; !ok {
				return nil, fmt.Errorf("%s has invalid fields", name)
			}
		}
	}
	return object, nil
}

func suiteText(value any) bool {
	text, ok := value.(string)
	return ok && strings.TrimSpace(text) != ""
}

func suiteInteger(value any, expected int) bool {
	switch value := value.(type) {
	case int:
		return value == expected
	case int64:
		return value == int64(expected)
	case uint64:
		return value == uint64(expected)
	default:
		return false
	}
}

// JSON v2 rejects duplicate names and invalid UTF-8. Retain integer syntax as
// integers so that a schema version written as 1.0 or true cannot pass as 1.
func suiteJSON(data []byte) (any, error) {
	var value any
	numbers := json.UnmarshalFromFunc(func(decoder *jsontext.Decoder, out *any) error {
		if decoder.PeekKind() != '0' {
			return errors.ErrUnsupported
		}
		data, err := decoder.ReadValue()
		if err != nil {
			return err
		}
		if !bytes.ContainsAny(data, ".eE") {
			if integer, err := strconv.Atoi(string(data)); err == nil {
				*out = integer
				return nil
			}
			integer, ok := new(big.Int).SetString(string(data), 10)
			if !ok {
				return errors.New("invalid JSON number")
			}
			*out = integer
			return nil
		}
		float, err := strconv.ParseFloat(string(data), 64)
		if err != nil || math.IsInf(float, 0) || math.IsNaN(float) {
			return errors.New("nonfinite JSON number")
		}
		*out = float
		return nil
	})
	if err := json.Unmarshal(data, &value, json.WithUnmarshalers(numbers)); err != nil {
		return nil, errors.New("cannot read valid JSON input (invalid value or duplicate mapping key)")
	}
	return value, nil
}

func suiteRead(path, description string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil || !utf8.Valid(data) {
		return nil, fmt.Errorf("cannot read %s", description)
	}
	return data, nil
}

func suiteWithin(root, target string) bool {
	relative, err := filepath.Rel(root, target)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) && !filepath.IsAbs(relative)
}

// EvalSymlinks requires an existing target. Resolve the closest existing
// ancestor as well, because a missing file below a symlink can still escape.
func suiteResolve(target string) (string, error) {
	current := filepath.Clean(target)
	var missing []string
	for {
		resolved, err := filepath.EvalSymlinks(current)
		if err == nil {
			for index := len(missing) - 1; index >= 0; index-- {
				resolved = filepath.Join(resolved, missing[index])
			}
			return resolved, nil
		}
		if !errors.Is(err, os.ErrNotExist) || current == filepath.Dir(current) {
			return "", err
		}
		missing = append(missing, filepath.Base(current))
		current = filepath.Dir(current)
	}
}

func suiteRoot(root string, rejectLink bool) (string, error) {
	info, err := os.Lstat(root)
	if err != nil || !info.IsDir() && info.Mode()&os.ModeSymlink == 0 {
		return "", errors.New("source directory does not exist")
	}
	if rejectLink && info.Mode()&os.ModeSymlink != 0 {
		return "", errors.New("source directory must not be a symlink")
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return "", errors.New("cannot resolve source directory")
	}
	resolved, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", errors.New("cannot resolve source directory")
	}
	info, err = os.Stat(resolved)
	if err != nil || !info.IsDir() {
		return "", errors.New("source directory does not exist")
	}
	return resolved, nil
}

func suiteRelativePath(root string, value any, field string) (string, error) {
	text, ok := value.(string)
	if !ok || strings.TrimSpace(text) == "" {
		return "", fmt.Errorf("%s must be a relative path", field)
	}
	if strings.ContainsAny(text, "\\\x00") {
		return "", fmt.Errorf("%s has an unsafe path", field)
	}
	if path.IsAbs(text) || suiteDrive.MatchString(text) {
		return "", fmt.Errorf("%s must be a relative path", field)
	}
	parts := strings.Split(text, "/")
	if path.Clean(text) != text || text == "." {
		return "", fmt.Errorf("%s must be a canonical relative path", field)
	}
	for _, part := range parts {
		if part == "." || part == ".." || part == "" {
			return "", fmt.Errorf("%s must be a canonical relative path", field)
		}
		if suitePrivateDirectories[part] {
			return "", fmt.Errorf("%s must not reference a private or generated directory", field)
		}
	}
	target := filepath.Join(root, filepath.FromSlash(text))
	resolved, err := suiteResolve(target)
	if err != nil || !suiteWithin(root, resolved) {
		return "", fmt.Errorf("%s escapes the source directory", field)
	}
	if info, err := os.Lstat(target); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("%s must not be a symlink", field)
	}
	return target, nil
}

func suiteRegular(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

// LoadManifest validates the portable manifest and every path it owns.
func LoadManifest(root string) (Object, error) {
	root, err := suiteRoot(root, false)
	if err != nil {
		return nil, err
	}
	data, err := suiteRead(filepath.Join(root, "skills-manifest.json"), "skills-manifest.json")
	if err != nil {
		return nil, err
	}
	value, err := suiteJSON(data)
	if err != nil {
		return nil, fmt.Errorf("cannot read valid skills-manifest.json: %w", err)
	}
	manifest, err := suiteMapping(value, "manifest", "version", "registrations", "global_instructions", "required_licenses", "model_policy", "claude_model_policy", "adoption_catalog")
	if err != nil {
		return nil, err
	}
	if !suiteInteger(manifest["version"], 3) {
		return nil, errors.New("manifest version must be 3")
	}
	registrations, err := suiteMapping(manifest["registrations"], "registrations")
	if err != nil || len(registrations) == 0 {
		return nil, errors.New("registrations must be a nonempty mapping")
	}
	var roots []string
	for name, value := range registrations {
		if len(name) > 64 || !suiteName.MatchString(name) {
			return nil, errors.New("registration name is invalid")
		}
		source, err := suiteRelativePath(root, value, "registration source")
		if err != nil {
			return nil, err
		}
		info, err := os.Stat(source)
		if err != nil || !info.IsDir() {
			return nil, errors.New("registration source directory is missing")
		}
		for _, previous := range roots {
			if suiteWithin(previous, source) || suiteWithin(source, previous) {
				return nil, errors.New("registration source directories overlap")
			}
		}
		roots = append(roots, source)
	}
	global, err := suiteRelativePath(root, manifest["global_instructions"], "global_instructions")
	if err != nil {
		return nil, err
	}
	if !suiteRegular(global) || !strings.EqualFold(filepath.Ext(global), ".md") {
		return nil, errors.New("global_instructions must reference an existing Markdown file")
	}
	data, err = suiteRead(global, "global instructions")
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(string(data)) == "" {
		return nil, errors.New("global instructions are empty")
	}
	licenses, ok := manifest["required_licenses"].([]any)
	if !ok || len(licenses) == 0 {
		return nil, errors.New("required_licenses must be a nonempty list")
	}
	seen := map[string]bool{}
	for _, value := range licenses {
		license, err := suiteRelativePath(root, value, "required license")
		if err != nil {
			return nil, err
		}
		if seen[license] {
			return nil, errors.New("required_licenses contains a duplicate path")
		}
		seen[license] = true
		if !suiteRegular(license) {
			return nil, errors.New("required license is missing")
		}
		data, err := suiteRead(license, "required license")
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(string(data)) == "" {
			return nil, errors.New("required license is empty")
		}
	}
	for _, field := range []string{"model_policy", "claude_model_policy"} {
		policy, err := suiteRelativePath(root, manifest[field], field)
		if err != nil {
			return nil, err
		}
		if !suiteRegular(policy) || !slices.Contains([]string{".yaml", ".yml"}, strings.ToLower(filepath.Ext(policy))) {
			return nil, fmt.Errorf("%s must reference an existing YAML file", field)
		}
		if !slices.ContainsFunc(roots, func(source string) bool { return suiteWithin(source, policy) }) {
			return nil, fmt.Errorf("%s must be inside a registered skill source", field)
		}
	}
	if _, err = readPersonalCatalog(root, manifest); err != nil {
		return nil, err
	}
	return manifest, nil
}

func suiteIgnoredArtifacts(root string) func(string) bool {
	checked, checkout := false, false
	git := func(arguments ...string) ([]byte, bool) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		command := exec.CommandContext(ctx, "git", append([]string{"-c", "core.fsmonitor=false", "-c", "core.untrackedCache=false"}, arguments...)...)
		command.Dir = root
		for _, item := range os.Environ() {
			if !strings.HasPrefix(item, "GIT_") {
				command.Env = append(command.Env, item)
			}
		}
		command.Env = append(command.Env, "GIT_OPTIONAL_LOCKS=0")
		output, err := command.Output()
		return output, err == nil
	}
	return func(target string) bool {
		if !checked {
			checked = true
			if _, err := os.Stat(filepath.Join(root, ".git")); err == nil {
				if output, ok := git("rev-parse", "--show-toplevel"); ok {
					resolved, err := suiteResolve(strings.TrimSpace(string(output)))
					checkout = err == nil && resolved == root
				}
			}
		}
		if !checkout {
			return false
		}
		relative, err := filepath.Rel(root, target)
		if err != nil {
			return false
		}
		relative = filepath.ToSlash(relative)
		tracked, ok := git("ls-files", "-z", "--", relative)
		if !ok || len(tracked) > 0 {
			return false
		}
		_, ok = git("check-ignore", "--quiet", "--no-index", "--", relative)
		return ok
	}
}

func suiteInspectTree(root string) ([]string, error) {
	var files []string
	ignored := suiteIgnoredArtifacts(root)
	err := filepath.WalkDir(root, func(target string, entry os.DirEntry, walkError error) error {
		if walkError != nil {
			return errors.New("cannot inspect source tree")
		}
		if target == root {
			return nil
		}
		relative, _ := filepath.Rel(root, target)
		display := filepath.ToSlash(relative)
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlink in source tree: %s", display)
		}
		name := entry.Name()
		if name == ".git" {
			if filepath.Dir(target) != root {
				return fmt.Errorf("nested Git checkout: %s", display)
			}
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.IsDir() {
			releaseStage := strings.HasPrefix(name, ".cw-release-")
			if (releaseStage || slices.Contains([]string{".venv", "__pycache__", ".bin", ".bin.lock", "dist", ".claude"}, name)) && ignored(target) {
				return filepath.SkipDir
			}
			if releaseStage || suitePrivateDirectories[name] {
				return fmt.Errorf("private or generated directory: %s", display)
			}
			return nil
		}
		private := name == "auth.json" || name == "config.toml" || name == ".DS_Store" || strings.HasSuffix(name, ".pyc") || strings.HasSuffix(name, ".pyo")
		if name == ".env" || strings.HasPrefix(name, ".env.") {
			private = private || !slices.Contains([]string{".env.example", ".env.sample", ".env.template"}, name)
		}
		if private {
			return fmt.Errorf("private or generated file: %s", display)
		}
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() {
			return fmt.Errorf("unsupported filesystem entry: %s", display)
		}
		files = append(files, target)
		return nil
	})
	slices.Sort(files)
	return files, err
}

// Check effective YAML mappings, including merge keys, before decoding. The
// generic decoder accepts unknown tags and merge overrides, whereas the old
// safe loader rejected both unsafe tags and duplicate effective keys.
func suiteYAML(data []byte) (any, error) {
	if !utf8.Valid(data) {
		return nil, errors.New("invalid YAML UTF-8")
	}
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	var document yaml.Node
	if err := decoder.Decode(&document); err != nil {
		return nil, errors.New("cannot read valid YAML")
	}
	var extra yaml.Node
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return nil, errors.New("YAML must contain one document")
	}
	suiteYAMLNormalize(&document)
	budget := 100000
	if _, err := suiteYAMLKeys(&document, map[*yaml.Node]bool{}, &budget); err != nil {
		return nil, err
	}
	return suiteYAMLValue(&document)
}

// PyYAML's safe loader uses YAML 1.1 scalar resolution. Keep its boolean and
// sexagesimal-number boundaries when YAML 1.2 would treat them as plain text.
func suiteYAMLNormalize(node *yaml.Node) {
	if node.Kind == yaml.ScalarNode && node.Tag == "!!str" && node.Style == 0 {
		switch node.Value {
		case "yes", "Yes", "YES", "on", "On", "ON":
			node.Tag, node.Value = "!!bool", "true"
		case "no", "No", "NO", "off", "Off", "OFF":
			node.Tag, node.Value = "!!bool", "false"
		default:
			if suiteSexagesimal.MatchString(node.Value) {
				text := strings.ReplaceAll(node.Value, "_", "")
				sign := 1.0
				if strings.HasPrefix(text, "-") {
					sign = -1
				}
				text = strings.TrimLeft(text, "+-")
				var value float64
				for part := range strings.SplitSeq(text, ":") {
					component, _ := strconv.ParseFloat(part, 64)
					value = value*60 + component
				}
				if strings.Contains(text, ".") {
					node.Tag = "!!float"
				} else {
					node.Tag = "!!int"
				}
				node.Value = strconv.FormatFloat(sign*value, 'f', -1, 64)
			}
		}
	}
	for _, child := range node.Content {
		suiteYAMLNormalize(child)
	}
}

type suiteBinary string

// Decode recursively to retain safe-loader types such as binary values and
// sets. They are valid within opaque vendor metadata but cannot masquerade as
// the strings, mappings, or JSON descriptions required by the public schema.
func suiteYAMLValue(node *yaml.Node) (any, error) {
	switch node.Kind {
	case yaml.DocumentNode:
		if len(node.Content) == 0 {
			return nil, nil
		}
		return suiteYAMLValue(node.Content[0])
	case yaml.AliasNode:
		return suiteYAMLValue(node.Alias)
	case yaml.ScalarNode:
		if node.ShortTag() == "!!merge" {
			return nil, errors.New("invalid YAML merge value")
		}
		var value any
		if err := node.Decode(&value); err != nil {
			return nil, errors.New("cannot read valid YAML scalar")
		}
		if node.ShortTag() == "!!binary" {
			text, ok := value.(string)
			if !ok {
				return nil, errors.New("invalid YAML binary value")
			}
			return suiteBinary(text), nil
		}
		return value, nil
	case yaml.SequenceNode:
		if !slices.Contains([]string{"!!seq", "!!omap", "!!pairs"}, node.ShortTag()) {
			return nil, errors.New("invalid YAML sequence tag")
		}
		values := make([]any, 0, len(node.Content))
		for _, child := range node.Content {
			value, err := suiteYAMLValue(child)
			if err != nil {
				return nil, err
			}
			values = append(values, value)
		}
		return values, nil
	case yaml.MappingNode:
		if !slices.Contains([]string{"!!map", "!!set"}, node.ShortTag()) {
			return nil, errors.New("invalid YAML mapping tag")
		}
		values := map[any]any{}
		copyMerge := func(value any) error {
			switch value := value.(type) {
			case map[string]any:
				for key, item := range value {
					values[key] = item
				}
			case map[any]any:
				for key, item := range value {
					values[key] = item
				}
			default:
				return errors.New("invalid YAML merge")
			}
			return nil
		}
		for index := 0; index < len(node.Content); index += 2 {
			keyNode, valueNode := node.Content[index], node.Content[index+1]
			value, err := suiteYAMLValue(valueNode)
			if err != nil {
				return nil, err
			}
			if keyNode.ShortTag() == "!!merge" {
				if sequence, ok := value.([]any); ok {
					for _, item := range sequence {
						if err := copyMerge(item); err != nil {
							return nil, err
						}
					}
				} else if err := copyMerge(value); err != nil {
					return nil, err
				}
				continue
			}
			key, err := suiteYAMLValue(keyNode)
			if err != nil {
				return nil, err
			}
			values[key] = value
		}
		if node.ShortTag() == "!!set" {
			set := map[any]bool{}
			for key := range values {
				set[key] = true
			}
			return set, nil
		}
		object := Object{}
		for key, value := range values {
			text, ok := key.(string)
			if !ok {
				return values, nil
			}
			object[text] = value
		}
		return object, nil
	default:
		return nil, errors.New("invalid YAML node")
	}
}

func suiteYAMLKeys(node *yaml.Node, active map[*yaml.Node]bool, budget *int) (map[string]bool, error) {
	*budget--
	if *budget < 0 || active[node] {
		return nil, errors.New("invalid or excessive YAML aliases")
	}
	active[node] = true
	defer delete(active, node)
	if node.Kind == yaml.AliasNode {
		return suiteYAMLKeys(node.Alias, active, budget)
	}
	if node.Kind != yaml.DocumentNode && !slices.Contains([]string{"!!null", "!!bool", "!!int", "!!float", "!!str", "!!seq", "!!map", "!!timestamp", "!!binary", "!!merge", "!!set", "!!omap", "!!pairs"}, node.ShortTag()) {
		return nil, errors.New("unsupported YAML tag")
	}
	keys := map[string]bool{}
	if node.Kind == yaml.MappingNode {
		for index := 0; index < len(node.Content); index += 2 {
			key, value := node.Content[index], node.Content[index+1]
			if _, err := suiteYAMLKeys(key, active, budget); err != nil {
				return nil, err
			}
			childKeys, err := suiteYAMLKeys(value, active, budget)
			if err != nil {
				return nil, err
			}
			if key.ShortTag() == "!!merge" {
				merge := value
				if merge.Kind == yaml.AliasNode {
					merge = merge.Alias
				}
				if merge.Kind == yaml.SequenceNode {
					childKeys = map[string]bool{}
					for _, item := range merge.Content {
						if item.Kind == yaml.AliasNode {
							item = item.Alias
						}
						if item.Kind != yaml.MappingNode {
							return nil, errors.New("invalid YAML merge")
						}
						merged, err := suiteYAMLKeys(item, active, budget)
						if err != nil {
							return nil, err
						}
						for key := range merged {
							if childKeys[key] {
								return nil, errors.New("duplicate mapping key")
							}
							childKeys[key] = true
						}
					}
				} else if merge.Kind != yaml.MappingNode {
					return nil, errors.New("invalid YAML merge")
				}
				for key := range childKeys {
					if keys[key] {
						return nil, errors.New("duplicate mapping key")
					}
					keys[key] = true
				}
				continue
			}
			if key.Kind == yaml.AliasNode {
				key = key.Alias
			}
			if key.Kind != yaml.ScalarNode {
				return nil, errors.New("invalid YAML mapping key")
			}
			var decoded any
			if err := key.Decode(&decoded); err != nil {
				return nil, errors.New("invalid YAML mapping key")
			}
			identity := fmt.Sprintf("%T:%v", decoded, decoded)
			switch decoded := decoded.(type) {
			case int:
				identity = fmt.Sprintf("number:%v", decoded)
			case uint64:
				identity = fmt.Sprintf("number:%v", decoded)
			case float64:
				identity = fmt.Sprintf("number:%v", decoded)
			case bool:
				if decoded {
					identity = "number:1"
				} else {
					identity = "number:0"
				}
			}
			if keys[identity] {
				return nil, errors.New("duplicate mapping key")
			}
			keys[identity] = true
		}
	} else {
		for _, child := range node.Content {
			if _, err := suiteYAMLKeys(child, active, budget); err != nil {
				return nil, err
			}
		}
	}
	return keys, nil
}

func suiteFrontmatter(root, target string) (string, error) {
	relative, _ := filepath.Rel(root, target)
	display := filepath.ToSlash(relative)
	data, err := suiteRead(target, display)
	if err != nil {
		return "", err
	}
	lines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
	if len(lines) == 0 || lines[0] != "---" {
		return "", fmt.Errorf("missing skill frontmatter: %s", display)
	}
	end := slices.Index(lines[1:], "---")
	if end < 0 {
		return "", fmt.Errorf("unterminated skill frontmatter: %s", display)
	}
	value, err := suiteYAML([]byte(strings.Join(lines[1:end+1], "\n")))
	if err != nil {
		return "", fmt.Errorf("invalid skill frontmatter: %s (%w)", display, err)
	}
	fields, err := suiteMapping(value, "skill frontmatter")
	if err != nil {
		return "", fmt.Errorf("invalid skill frontmatter fields: %s", display)
	}
	allowed := []string{"name", "description", "license", "compatibility", "metadata", "allowed-tools"}
	for key := range fields {
		if !slices.Contains(allowed, key) {
			return "", fmt.Errorf("invalid skill frontmatter fields: %s", display)
		}
	}
	name, ok := fields["name"].(string)
	if !ok || len(name) > 64 || !suiteName.MatchString(name) {
		return "", fmt.Errorf("invalid skill name: %s", display)
	}
	if !suiteText(fields["description"]) {
		return "", fmt.Errorf("invalid skill description: %s", display)
	}
	for _, field := range []string{"license", "compatibility", "allowed-tools"} {
		if value, present := fields[field]; present && !suiteText(value) {
			return "", fmt.Errorf("invalid skill %s: %s", field, display)
		}
	}
	if value, present := fields["metadata"]; present {
		if _, err := suiteMapping(value, "skill metadata"); err != nil {
			return "", fmt.Errorf("invalid skill metadata: %s", display)
		}
	}
	return name, nil
}

func suiteProse(text string) string {
	text = suiteComments.ReplaceAllString(text, "")
	var prose strings.Builder
	fence := ""
	listIndent := -1
	for line := range strings.SplitAfterSeq(text, "\n") {
		marker := suiteFence.FindStringSubmatch(line)
		list := suiteList.FindStringSubmatch(line)
		if list != nil {
			listIndent = len(list[1])
		} else if strings.TrimSpace(line) != "" && !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "\t") {
			listIndent = -1
		}
		if fence != "" {
			if marker != nil && marker[1][0] == fence[0] && len(marker[1]) >= len(fence) {
				fence = ""
			}
			prose.WriteByte('\n')
		} else if marker != nil {
			fence = marker[1]
			prose.WriteByte('\n')
		} else if listIndent < 0 && (strings.HasPrefix(line, "    ") || strings.HasPrefix(line, "\t")) {
			prose.WriteByte('\n')
		} else {
			prose.WriteString(line)
		}
	}
	text = prose.String()
	var result strings.Builder
	for index := 0; index < len(text); {
		if text[index] != '`' {
			result.WriteByte(text[index])
			index++
			continue
		}
		end := index
		for end < len(text) && text[end] == '`' {
			end++
		}
		if close := strings.Index(text[end:], text[index:end]); close >= 0 {
			index = end + close + end - index
		} else {
			result.WriteString(text[index:end])
			index = end
		}
	}
	return result.String()
}

func suiteDestinations(text string) []string {
	text = suiteProse(text)
	var destinations []string
	for start := 0; start < len(text); start++ {
		if text[start] != '[' {
			continue
		}
		if start > 0 {
			previous, _ := utf8.DecodeLastRuneInString(text[:start])
			if previous == '\\' || previous == '_' || unicode.IsLetter(previous) || unicode.IsDigit(previous) {
				continue
			}
		}
		index, brackets := start+1, 1
		for index < len(text) && brackets > 0 {
			if text[index] == '\\' && index+1 < len(text) {
				index += 2
				continue
			}
			if text[index] == '[' {
				brackets++
			} else if text[index] == ']' {
				brackets--
			}
			index++
		}
		if brackets != 0 || index >= len(text) || text[index] != '(' {
			continue
		}
		index++
		for index < len(text) && unicode.IsSpace(rune(text[index])) {
			index++
		}
		if index < len(text) && text[index] == '<' {
			if offset := strings.IndexByte(text[index+1:], '>'); offset >= 0 {
				end := index + 1 + offset
				if suiteLinkEnd.MatchString(text[end+1:]) {
					destinations = append(destinations, text[index+1:end])
				}
			}
			continue
		}
		begin, depth := index, 0
		for index < len(text) {
			character := text[index]
			if character == '\\' && index+1 < len(text) {
				index += 2
				continue
			}
			if character == '(' {
				depth++
			} else if character == ')' {
				if depth == 0 {
					destinations = append(destinations, text[begin:index])
					break
				}
				depth--
			} else if unicode.IsSpace(rune(character)) && depth == 0 {
				if suiteLinkEnd.MatchString(text[index:]) {
					destinations = append(destinations, text[begin:index])
				}
				break
			}
			index++
		}
	}
	for _, match := range suiteDefinition.FindAllStringSubmatch(text, -1) {
		if match[1] != "" {
			destinations = append(destinations, match[1])
		} else {
			destinations = append(destinations, match[2])
		}
	}
	return destinations
}

func suiteMarkdown(root, target string) error {
	relative, _ := filepath.Rel(root, target)
	display := filepath.ToSlash(relative)
	data, err := suiteRead(target, display)
	if err != nil {
		return err
	}
	parts := strings.Split(display, "/")
	generated := false
	for index := 0; index+1 < len(parts); index++ {
		generated = generated || parts[index] == "assets" && parts[index+1] == "templates"
	}
	generated = generated && suitePlaceholder.Match(data)
	for _, destination := range suiteDestinations(string(data)) {
		destination = suiteEscapes.ReplaceAllString(destination, "$1")
		if suiteWindowsPath.MatchString(destination) {
			return fmt.Errorf("nonportable Markdown path: %s", display)
		}
		parsed, err := url.Parse(destination)
		if err != nil {
			return fmt.Errorf("invalid Markdown destination: %s", display)
		}
		if parsed.Scheme != "" || parsed.Host != "" {
			if strings.EqualFold(parsed.Scheme, "file") {
				return fmt.Errorf("nonportable Markdown file URL: %s", display)
			}
			continue
		}
		link := parsed.Path
		if link == "" {
			continue
		}
		if strings.HasPrefix(link, "/") || strings.ContainsAny(link, "\\\x00") {
			return fmt.Errorf("nonportable Markdown path: %s", display)
		}
		for _, part := range strings.Split(link, "/") {
			if suitePrivateDirectories[part] {
				return fmt.Errorf("Markdown reference targets a private or generated directory: %s", display)
			}
		}
		resolved, err := suiteResolve(filepath.Join(filepath.Dir(target), filepath.FromSlash(link)))
		if err != nil || !suiteWithin(root, resolved) {
			return fmt.Errorf("Markdown reference escapes source: %s", display)
		}
		clean := path.Clean(link)
		if generated && (suitePlaceholder.MatchString(link) || !strings.Contains(clean, "/") && suiteProjectDocuments[clean]) {
			continue
		}
		if _, err := os.Stat(resolved); err != nil {
			return fmt.Errorf("missing local Markdown reference: %s -> %s", display, link)
		}
	}
	return nil
}

var policyModelEfforts = map[string][]string{
	"gpt-6.1-sol": {"low", "medium", "high", "xhigh", "max", "ultra"},
	"gpt-6-sol":   {"low", "medium", "high", "xhigh", "max", "ultra"},
	"gpt-6-astra": {"low", "medium", "high", "xhigh", "max", "ultra"},
	"gpt-6-luna":  {"low", "medium", "high", "xhigh", "max"},
}

var policySubstantiveRoles = []string{"coordinator", "coding", "testing", "planning", "architecture", "review", "diagnosis", "documentation", "issue_management", "pr_authoring"}
var policyExecutionRoles = []string{"commit_execution", "pr_authoring_routine", "pr_submission"}
var policyEvidenceRoles = []string{"lookup", "summarization"}

func policyList(value any, message string, allowed func(string) bool, nonempty bool) ([]string, error) {
	list, ok := value.([]any)
	if !ok || nonempty && len(list) == 0 {
		return nil, errors.New(message)
	}
	result := make([]string, 0, len(list))
	seen := map[string]bool{}
	for _, value := range list {
		text, ok := value.(string)
		if !ok || !allowed(text) || seen[text] {
			return nil, errors.New(message)
		}
		seen[text] = true
		result = append(result, text)
	}
	return result, nil
}

func policyProfile(value any) (Object, error) {
	profile, err := suiteMapping(value, "profile", "model", "reasoning_effort", "user_requested_efforts", "scope")
	if err != nil {
		return nil, err
	}
	model, ok := profile["model"].(string)
	efforts, known := policyModelEfforts[model]
	if !ok || !known {
		return nil, errors.New("unknown worker model")
	}
	effort, ok := profile["reasoning_effort"].(string)
	if !ok || !slices.Contains(efforts, effort) {
		return nil, errors.New("unsupported default reasoning effort")
	}
	if _, err := policyList(profile["user_requested_efforts"], "unsupported or duplicate user-requested efforts", func(item string) bool { return slices.Contains(efforts, item) }, false); err != nil {
		return nil, err
	}
	scope, ok := profile["scope"].(string)
	if !ok || !slices.Contains([]string{"substantive", "bounded_evidence", "bounded_execution"}, scope) {
		return nil, errors.New("invalid profile scope")
	}
	if scope == "substantive" && model == "gpt-6-luna" {
		return nil, errors.New("substantive profiles require a non-Luna model")
	}
	return profile, nil
}

func policyReferenced(profiles Object, value any) (Object, error) {
	name, ok := value.(string)
	if !ok {
		return nil, errors.New("invalid profile reference")
	}
	profile, present := profiles[name]
	if !present {
		return nil, errors.New("invalid profile reference")
	}
	return policyProfile(profile)
}

func policyRoleScope(role string, scope any) bool {
	if scope == "substantive" {
		return true
	}
	return slices.Contains(policyExecutionRoles, role) && scope == "bounded_execution" || slices.Contains(policyEvidenceRoles, role) && scope == "bounded_evidence"
}

func policyJSONValue(value any) bool {
	switch value := value.(type) {
	case nil, string, bool, int, int64, uint64, *big.Int:
		return true
	case float64:
		return !math.IsInf(value, 0) && !math.IsNaN(value)
	case []any:
		return !slices.ContainsFunc(value, func(item any) bool { return !policyJSONValue(item) })
	case map[string]any:
		for _, item := range value {
			if !policyJSONValue(item) {
				return false
			}
		}
		return true
	default:
		return false
	}
}

func policyDescription(value any) bool {
	if !policyJSONValue(value) {
		return false
	}
	switch value := value.(type) {
	case string:
		return strings.TrimSpace(value) != ""
	case []any:
		return len(value) > 0
	case map[string]any:
		return len(value) > 0
	default:
		return false
	}
}

func policyQuestions(policyPath string, reference any) error {
	if !suiteText(reference) {
		return errors.New("invalid question reference")
	}
	text := reference.(string)
	if path.IsAbs(text) || suiteDrive.MatchString(text) || strings.ContainsAny(text, "\\\x00") {
		return errors.New("question reference must be relative")
	}
	parent := filepath.Dir(policyPath)
	resolved, err := suiteResolve(filepath.Join(parent, filepath.FromSlash(text)))
	if err != nil || resolved == parent || !suiteWithin(parent, resolved) {
		return errors.New("question reference escapes skill directory")
	}
	data, err := suiteRead(resolved, "question reference")
	if err != nil {
		return err
	}
	value, err := suiteJSON(data)
	if err != nil {
		return err
	}
	questions, err := suiteMapping(value, "question reference")
	if err != nil {
		return err
	}
	if !suiteInteger(questions["version"], 1) {
		return errors.New("question reference version has an invalid value")
	}
	templates, err := suiteMapping(questions["questions"], "question templates")
	if err != nil || len(templates) == 0 {
		return errors.New("question templates are empty or invalid")
	}
	for name, value := range templates {
		if strings.TrimSpace(name) == "" {
			return errors.New("invalid question ID")
		}
		question, err := suiteMapping(value, "question", "type", "instructions", "criteria")
		if err != nil {
			return err
		}
		if question["type"] != "choice" {
			return errors.New("only Choice questions are supported")
		}
		if !policyDescription(question["instructions"]) {
			return errors.New("empty or invalid description")
		}
		criteria, err := suiteMapping(question["criteria"], "criteria")
		if err != nil || len(criteria) < 2 {
			return errors.New("Choice needs at least two options")
		}
		for option, description := range criteria {
			if strings.TrimSpace(option) == "" {
				return errors.New("invalid option ID")
			}
			if !policyDescription(description) {
				return errors.New("empty or invalid description")
			}
		}
	}
	return nil
}

func suiteModelPolicy(policyPath string) (Object, error) {
	data, err := suiteRead(policyPath, "model policy")
	if err != nil {
		return nil, err
	}
	value, err := suiteYAML(data)
	if err != nil {
		return nil, err
	}
	policy, err := suiteMapping(value, "policy", "version", "activation", "profiles", "effort_policy", "effort_guidance", "roles", "lookup_boundary", "unknown_role_profile", "fixed_specialists", "fallbacks", "consultation", "publication")
	if err != nil {
		return nil, err
	}
	if !suiteInteger(policy["version"], 2) {
		return nil, errors.New("policy version has an invalid value")
	}
	activation, err := suiteMapping(policy["activation"], "activation", "any_of", "bypass")
	if err != nil {
		return nil, err
	}
	if _, err := policyList(activation["any_of"], "invalid or duplicate activation conditions", func(item string) bool {
		return slices.Contains([]string{"independent_workstreams", "cross_component_change", "consequential_uncertainty"}, item)
	}, true); err != nil {
		return nil, err
	}
	if activation["bypass"] != "small_edits_and_direct_factual_answers" {
		return nil, errors.New("invalid activation bypass")
	}
	profiles, err := suiteMapping(policy["profiles"], "profiles")
	if err != nil {
		return nil, err
	}
	for _, name := range []string{"sol", "luna", "luna_execution"} {
		if _, present := profiles[name]; !present {
			return nil, errors.New("missing required profile")
		}
	}
	var maxProfiles []string
	for name, value := range profiles {
		if strings.TrimSpace(name) == "" {
			return nil, errors.New("invalid profile name")
		}
		profile, err := policyProfile(value)
		if err != nil {
			return nil, err
		}
		if profile["reasoning_effort"] == "max" {
			maxProfiles = append(maxProfiles, name)
		}
	}
	effortPolicy, err := suiteMapping(policy["effort_policy"], "effort_policy", "default_substantive", "max_for_sol", "configured_max_profiles", "automatic_escalation")
	if err != nil {
		return nil, err
	}
	maxRule, ok := effortPolicy["max_for_sol"].(string)
	if !ok || !slices.Contains([]string{"explicit_user_request", "configured_or_explicit_user_request"}, maxRule) {
		return nil, errors.New("invalid Sol max rule")
	}
	if maxRule == "explicit_user_request" {
		for _, name := range maxProfiles {
			if slices.Contains([]string{"gpt-6.1-sol", "gpt-6-sol"}, profiles[name].(Object)["model"].(string)) {
				return nil, errors.New("Sol max requires an explicit user request")
			}
		}
	}
	configured, err := policyList(effortPolicy["configured_max_profiles"], "invalid configured max profiles", func(name string) bool { _, present := profiles[name]; return present }, false)
	if err != nil {
		return nil, err
	}
	slices.Sort(configured)
	slices.Sort(maxProfiles)
	if !slices.Equal(configured, maxProfiles) {
		return nil, errors.New("invalid configured max profiles")
	}
	if effortPolicy["automatic_escalation"] != false {
		return nil, errors.New("automatic effort escalation must be disabled")
	}
	guidance, err := suiteMapping(policy["effort_guidance"], "effort_guidance")
	if err != nil {
		return nil, err
	}
	guidanceFields := []string{"ultra", "max", "comparison"}
	if _, present := guidance["xhigh"]; present {
		guidanceFields = append(guidanceFields, "xhigh")
	}
	guidance, err = suiteMapping(guidance, "effort_guidance", guidanceFields...)
	if err != nil {
		return nil, err
	}
	for _, value := range guidance {
		if !suiteText(value) {
			return nil, errors.New("effort guidance must be nonempty text")
		}
	}
	roles, err := suiteMapping(policy["roles"], "roles")
	if err != nil {
		return nil, err
	}
	for _, required := range slices.Concat(policySubstantiveRoles, policyExecutionRoles, policyEvidenceRoles) {
		if _, present := roles[required]; !present {
			return nil, errors.New("missing required role")
		}
	}
	for role, name := range roles {
		if strings.TrimSpace(role) == "" {
			return nil, errors.New("invalid role name")
		}
		profile, err := policyReferenced(profiles, name)
		if err != nil {
			return nil, err
		}
		if !policyRoleScope(role, profile["scope"]) {
			return nil, errors.New("invalid role routing")
		}
	}
	coordinator, err := policyReferenced(profiles, roles["coordinator"])
	if err != nil {
		return nil, err
	}
	if effortPolicy["default_substantive"] != coordinator["reasoning_effort"] {
		return nil, errors.New("substantive default must match coordinator effort")
	}
	fallback, err := policyReferenced(profiles, policy["unknown_role_profile"])
	if err != nil || fallback["scope"] != "substantive" {
		return nil, errors.New("unknown-role fallback must be substantive")
	}
	if policy["lookup_boundary"] != "bounded_read_only_evidence_without_consequential_conclusions" {
		return nil, errors.New("invalid lookup boundary")
	}
	specialists, err := suiteMapping(policy["fixed_specialists"], "fixed_specialists", "context_explorer")
	if err != nil {
		return nil, err
	}
	specialist, err := suiteMapping(specialists["context_explorer"], "context_explorer", "respect_runtime_model", "scope")
	if err != nil {
		return nil, err
	}
	if specialist["respect_runtime_model"] != true || specialist["scope"] != "evidence_gathering_only" {
		return nil, errors.New("invalid fixed-specialist model or scope")
	}
	fallbacks, err := suiteMapping(policy["fallbacks"], "fallbacks", "luna_unavailable", "sol_unavailable", "jev_unavailable_or_inconclusive")
	if err != nil {
		return nil, err
	}
	fallback, err = policyReferenced(profiles, fallbacks["luna_unavailable"])
	if err != nil || fallback["scope"] != "substantive" {
		return nil, errors.New("worker fallback must be substantive")
	}
	if fallbacks["sol_unavailable"] != "compatible_coordinator_or_report_blocker" || fallbacks["jev_unavailable_or_inconclusive"] != "continue_with_source_evidence_and_conservative_routing" {
		return nil, errors.New("invalid fallback routing")
	}
	consultation, err := suiteMapping(policy["consultation"], "consultation")
	if err != nil {
		return nil, err
	}
	consultationFields := []string{"model", "deadline_seconds", "attempts_per_evidence_batch", "automatic_retries", "questions", "ownership", "reuse", "sharing"}
	_, hasProviders := consultation["providers"]
	_, hasComparison := consultation["comparison"]
	_, hasOptionalProviders := consultation["optional_providers"]
	if hasProviders != hasComparison {
		return nil, errors.New("consultation providers and comparison must be configured together")
	}
	if hasOptionalProviders && !hasProviders {
		return nil, errors.New("optional consultation providers require paired providers and comparison")
	}
	if hasProviders {
		consultationFields = append(consultationFields, "providers", "comparison")
	}
	if hasOptionalProviders {
		consultationFields = append(consultationFields, "optional_providers")
	}
	consultation, err = suiteMapping(consultation, "consultation", consultationFields...)
	if err != nil {
		return nil, err
	}
	model, ok := consultation["model"].(string)
	if !ok || !policyJev.MatchString(model) {
		return nil, errors.New("consultation model must be an exact versioned Jev ID")
	}
	if hasProviders {
		providers, err := suiteMapping(consultation["providers"], "consultation providers")
		if err != nil {
			return nil, err
		}
		providerFields := []string{"jev", "subq_snap"}
		_, hasRequiredOpenAI := providers["openai_decisions"]
		if hasRequiredOpenAI {
			providerFields = append(providerFields, "openai_decisions")
		}
		providers, err = suiteMapping(providers, "consultation providers", providerFields...)
		if err != nil {
			return nil, err
		}
		if providers["jev"] != model || providers["subq_snap"] != "subq-snap-preview" {
			return nil, errors.New("consultation providers must match the Jev model and use subq-snap-preview")
		}
		if consultation["comparison"] != "coordinator_evidence_and_verification" {
			return nil, errors.New("consultation comparison must use Codex evidence and verification")
		}
		if hasRequiredOpenAI {
			if providers["openai_decisions"] != "gpt-6-luna" {
				return nil, errors.New("required OpenAI consultation provider must use gpt-6-luna")
			}
			if hasOptionalProviders {
				return nil, errors.New("OpenAI consultation provider cannot be both required and optional")
			}
		}
	}
	if hasOptionalProviders {
		providers, err := suiteMapping(consultation["optional_providers"], "optional consultation providers", "openai_decisions")
		if err != nil {
			return nil, err
		}
		if providers["openai_decisions"] != "gpt-6-luna" {
			return nil, errors.New("optional consultation provider must use gpt-6-luna")
		}
	}
	if !suiteInteger(consultation["deadline_seconds"], 30) || !suiteInteger(consultation["attempts_per_evidence_batch"], 1) {
		return nil, errors.New("invalid consultation deadline or attempts")
	}
	if consultation["automatic_retries"] != false {
		return nil, errors.New("automatic retries must be disabled")
	}
	if consultation["ownership"] != "task_coordinator" || consultation["reuse"] != "unchanged_evidence_decisions_question_meaning_and_model" || consultation["sharing"] != "minimal_evidence_within_task_authorization" {
		return nil, errors.New("invalid consultation ownership, reuse, or sharing rule")
	}
	if err := policyQuestions(policyPath, consultation["questions"]); err != nil {
		return nil, err
	}
	publication, err := suiteMapping(policy["publication"], "publication", "authority", "analysis_output", "refresh_before_write", "readback_after_write", "uncertain_outcome")
	if err != nil {
		return nil, err
	}
	if publication["authority"] != "user_request_and_carried_authorization" || publication["analysis_output"] != "draft" || publication["refresh_before_write"] != true || publication["readback_after_write"] != true || publication["uncertain_outcome"] != "reconcile_before_retry" {
		return nil, errors.New("invalid publication safeguards")
	}
	return policy, nil
}

// modelSettings projects defaults and profiles from the same validated policy
// snapshot, including historical policies stored with an installed release.
func modelSettings(root string, manifest Object) (map[string]string, Object, error) {
	root, err := suiteRoot(root, false)
	if err != nil {
		return nil, nil, err
	}
	policyPath, err := suiteRelativePath(root, manifest["model_policy"], "model_policy")
	if err != nil {
		return nil, nil, err
	}
	policy, err := suiteModelPolicy(policyPath)
	if err != nil {
		return nil, nil, fmt.Errorf("cannot resolve validated coordinator model defaults: %w", err)
	}
	profiles := policy["profiles"].(map[string]any)
	roles := policy["roles"].(map[string]any)
	profile, err := policyReferenced(profiles, roles["coordinator"])
	if err != nil || profile["scope"] != "substantive" {
		return nil, nil, errors.New("invalid coordinator model routing")
	}
	settings := Object{}
	for name, value := range profiles {
		profile := value.(Object)
		settings[name] = Object{"model": profile["model"], "reasoning_effort": profile["reasoning_effort"], "scope": profile["scope"]}
	}
	return map[string]string{"model": profile["model"].(string), "model_reasoning_effort": profile["reasoning_effort"].(string)}, settings, nil
}

// ModelDefaults resolves the coordinator's two managed Codex config keys after
// validating the complete editable policy and question contract.
func ModelDefaults(root string, manifest Object) (map[string]string, error) {
	defaults, _, err := modelSettings(root, manifest)
	return defaults, err
}

// ValidateSuite validates a portable source tree without executing source code,
// following source links, or changing files, Git metadata, or installed settings.
func ValidateSuite(root string) (Object, error) {
	root, err := suiteRoot(root, true)
	if err != nil {
		return nil, err
	}
	files, err := suiteInspectTree(root)
	if err != nil {
		return nil, err
	}
	manifest, err := LoadManifest(root)
	if err != nil {
		return nil, err
	}
	// Frozen legacy releases retain their former automation for rollback and
	// recovery. New manager releases declare their resources under third_party.
	for _, source := range manifest["registrations"].(map[string]any) {
		if strings.HasPrefix(text(source), "third_party/") {
			if err := ValidateAutomation(root); err != nil {
				return nil, err
			}
			break
		}
	}
	names := map[string]bool{}
	for registration, value := range manifest["registrations"].(map[string]any) {
		source, err := suiteRelativePath(root, value, "registration source")
		if err != nil {
			return nil, err
		}
		entrypoints := 0
		for _, target := range files {
			if filepath.Base(target) != "SKILL.md" || !suiteWithin(source, target) {
				continue
			}
			entrypoints++
			name, err := suiteFrontmatter(root, target)
			if err != nil {
				return nil, err
			}
			if names[name] {
				return nil, errors.New("duplicate skill name")
			}
			names[name] = true
			if target == filepath.Join(source, "SKILL.md") && name != registration {
				return nil, errors.New("registration does not match skill name")
			}
		}
		if entrypoints == 0 {
			return nil, errors.New("registration has no skill entrypoints")
		}
	}
	markdown := 0
	for _, target := range files {
		if strings.EqualFold(filepath.Ext(target), ".md") {
			if err := suiteMarkdown(root, target); err != nil {
				return nil, err
			}
			markdown++
		}
	}
	policyPath, err := suiteRelativePath(root, manifest["model_policy"], "model_policy")
	if err != nil {
		return nil, err
	}
	if _, err := suiteModelPolicy(policyPath); err != nil {
		return nil, fmt.Errorf("model policy is invalid: %w", err)
	}
	if _, err := ClaudeModelDefaults(root, manifest); err != nil {
		return nil, fmt.Errorf("Claude model policy is invalid: %w", err)
	}
	if _, err := SharedConfigDefaults(root); err != nil {
		return nil, fmt.Errorf("shared Codex defaults are invalid: %w", err)
	}
	return Object{
		"version": manifest["version"], "registrations": manifest["registrations"],
		"global_instructions": manifest["global_instructions"], "model_policy": manifest["model_policy"],
		"claude_model_policy": manifest["claude_model_policy"],
		"skill_count":         len(names), "markdown_files_checked": markdown,
		"required_licenses": len(manifest["required_licenses"].([]any)),
	}, nil
}
