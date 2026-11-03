// Package graphify runs guarded local discovery over tracked Go source with the
// externally installed Graphify tool. Every graph is bound to the exact worktree,
// revision and content it was built from, and queries refuse anything else.
package graphify

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	json1 "encoding/json"
	json "encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Version pins the external graphifyy release this runner was assessed against.
const Version = "0.9.91"

const (
	schema         = 1
	maxFileBytes   = 16 * 1024 * 1024
	maxSourceBytes = 128 * 1024 * 1024
	maxGraphBytes  = 128 * 1024 * 1024
	gitTimeout     = 15 * time.Second
	versionTimeout = 15 * time.Second
	qualification  = "Undirected structural discovery only. EXTRACTED/INFERRED/AMBIGUOUS are upstream " +
		"extraction labels, not verified correctness; even EXTRACTED call targets can " +
		"be wrong for same-named functions. Verify call direction, interfaces, " +
		"factories and dynamic dispatch against current source/gopls. Missing results do " +
		"not prove absence."
	// Fallback tells a caller how to continue when no current graph is available.
	Fallback = "Continue with direct source/rg, available jbcontext and gopls"
)

var (
	excludedComponents = []string{".git", ".venv", "fixture", "fixtures", "node_modules", "testdata", "third_party", "vendor"}
	moduleFiles        = []string{"go.mod", "go.sum"}
	extractionFailure  = regexp.MustCompile(`(?i)AST extraction failed|extraction is incomplete|walk.errors`)
	sourceLocation     = regexp.MustCompile(`^L?([0-9]+)(?:-L?([0-9]+))?$`)
)

// Error is a refused discovery step with a stable machine-readable code.
type Error struct{ Code, Detail string }

func (e *Error) Error() string { return e.Detail }

func refuse(code, detail string) error { return &Error{code, detail} }

// Options selects one repository and its external runtime and cache.
type Options struct {
	Repo, Runtime, CacheRoot, Question string
	Timeout                            time.Duration
	Budget                             int
	Home                               func() (string, error)
	// Executable locates this manager; its digest invalidates graphs built by a
	// different runner, including a graph written with another safety policy.
	Executable func() (string, error)
	// renameCache is a per-build publication dependency; production uses os.Rename.
	renameCache func(string, string) error
}

type scope struct {
	Languages          []string `json:"languages"`
	ModuleFiles        []string `json:"module_files"`
	ExcludedComponents []string `json:"excluded_components"`
}

type sourceFile struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Size   int    `json:"size"`
}

// sourceState identifies a graph's complete input; its encoding keys the cache.
type sourceState struct {
	AdapterSHA256 string       `json:"adapter_sha256"`
	Branch        *string      `json:"branch"`
	Files         []sourceFile `json:"files"`
	Repository    string       `json:"repository"`
	Revision      string       `json:"revision"`
	Schema        int          `json:"schema"`
	Scope         scope        `json:"scope"`
	SourceSHA256  string       `json:"source_sha256"`
	ToolVersion   string       `json:"tool_version"`
}

// Provenance names what a result was derived from.
type Provenance struct {
	Repository   string  `json:"repository"`
	Revision     string  `json:"revision"`
	Branch       *string `json:"branch"`
	ToolVersion  string  `json:"tool_version"`
	SourceSHA256 string  `json:"source_sha256"`
}

// Summary describes a validated graph.
type Summary struct {
	Nodes       int            `json:"nodes"`
	Edges       int            `json:"edges"`
	SourceFiles int            `json:"source_files"`
	EdgeLabels  map[string]int `json:"edge_labels"`
	Directed    bool           `json:"directed"`
}

// BuildResult reports a reused or newly activated graph.
type BuildResult struct {
	Status         string     `json:"status"`
	Command        string     `json:"command"`
	Reused         bool       `json:"reused"`
	Graph          string     `json:"graph"`
	Provenance     Provenance `json:"provenance"`
	Summary        Summary    `json:"summary"`
	ElapsedSeconds *float64   `json:"elapsed_seconds,omitempty"`
}

// QueryResult holds qualified context from a graph matching current source.
type QueryResult struct {
	Status           string     `json:"status"`
	Command          string     `json:"command"`
	Graph            string     `json:"graph"`
	Provenance       Provenance `json:"provenance"`
	Qualification    string     `json:"qualification"`
	Context          string     `json:"context"`
	ContextChars     int        `json:"context_chars"`
	Truncated        bool       `json:"truncated"`
	TruncationNotice *string    `json:"truncation_notice"`
	Summary          Summary    `json:"summary"`
	ElapsedSeconds   float64    `json:"elapsed_seconds"`
}

type readyEntry struct {
	Source      sourceState `json:"source"`
	GraphSHA256 string      `json:"graph_sha256"`
	Summary     Summary     `json:"summary"`
	Runtime     string      `json:"runtime"`
	BuiltAt     string      `json:"built_at"`
}

func digest(data []byte) string {
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}

func encoded(value any) ([]byte, error) { return json.Marshal(value, json.Deterministic(true)) }

func inside(target, root string) bool {
	relative, err := filepath.Rel(root, target)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func seconds(started time.Time) float64 {
	value, _ := strconv.ParseFloat(strconv.FormatFloat(time.Since(started).Seconds(), 'f', 6, 64), 64)
	return value
}

// resolvePath follows symlinks through the closest existing ancestor.
func resolvePath(target string) (string, error) {
	current, err := filepath.Abs(target)
	if err != nil {
		return "", err
	}
	var missing []string
	for {
		resolved, err := filepath.EvalSymlinks(current)
		if err == nil {
			slices.Reverse(missing)
			return filepath.Join(append([]string{resolved}, missing...)...), nil
		}
		if !errors.Is(err, os.ErrNotExist) && !errors.Is(err, syscall.ENOTDIR) || current == filepath.Dir(current) {
			return "", err
		}
		missing = append(missing, filepath.Base(current))
		current = filepath.Dir(current)
	}
}

func (o Options) expand(target string) (string, error) {
	if target != "~" && !strings.HasPrefix(target, "~/") {
		return target, nil
	}
	home, err := o.Home()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, strings.TrimPrefix(target, "~")), nil
}

func (o Options) adapterDigest() (string, error) {
	executable, err := o.Executable()
	if err != nil {
		return "", err
	}
	if executable, err = filepath.EvalSymlinks(executable); err != nil {
		return "", err
	}
	data, err := os.ReadFile(executable)
	if err != nil {
		return "", err
	}
	return digest(data), nil
}

func git(ctx context.Context, repo string, allowMissing bool, arguments ...string) (string, error) {
	executable, err := lookPath("git", defaultPath)
	if err != nil {
		return "", err
	}
	code, stdout, stderr, err := run(ctx, append([]string{executable, "-C", repo}, arguments...), repo, environment("", "", ""), gitTimeout)
	if err != nil {
		return "", err
	}
	if code != 0 && !(allowMissing && code == 1) {
		detail := strings.TrimSpace(stderr)
		if detail == "" {
			detail = "Git source inspection failed"
		}
		return "", refuse("git", detail)
	}
	return stdout, nil
}

// inScope applies the fixed language and exclusion scope to one tracked path.
func inScope(parts []string) bool {
	if slices.ContainsFunc(parts, func(part string) bool { return slices.Contains(excludedComponents, part) }) {
		return false
	}
	name := parts[len(parts)-1]
	return path.Ext(name) == ".go" && name != ".go" || slices.Contains(moduleFiles, name)
}

// readTracked returns one in-scope file's bytes, or nil when a tracked deletion
// leaves it absent. Any symlink along the path is refused.
func readTracked(root, name string, parts []string) ([]byte, error) {
	target := filepath.Join(root, filepath.FromSlash(name))
	if _, err := os.Lstat(target); errors.Is(err, os.ErrNotExist) || errors.Is(err, syscall.ENOTDIR) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	current := root
	for _, part := range parts {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if err != nil {
			return nil, err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil, refuse("source_symlink", "Tracked source traverses a symlink: "+name)
		}
	}
	info, err := os.Stat(target)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > maxFileBytes {
		return nil, refuse("source_limit", "Unsupported source file: "+name)
	}
	return os.ReadFile(target)
}

// source captures the tracked snapshot this run is allowed to index.
func (o Options) source(ctx context.Context) (sourceState, map[string][]byte, error) {
	var state sourceState
	expanded, err := o.expand(o.Repo)
	if err != nil {
		return state, nil, err
	}
	requested, err := resolvePath(expanded)
	if err != nil {
		return state, nil, err
	}
	if info, err := os.Stat(requested); err != nil || !info.IsDir() {
		return state, nil, refuse("repository", "Repository directory is missing")
	}
	top, err := git(ctx, requested, false, "rev-parse", "--show-toplevel")
	if err != nil {
		return state, nil, err
	}
	root, err := resolvePath(strings.TrimSpace(top))
	if err != nil {
		return state, nil, err
	}
	revision, err := git(ctx, root, false, "rev-parse", "HEAD")
	if err != nil {
		return state, nil, err
	}
	head, err := git(ctx, root, true, "symbolic-ref", "--quiet", "--short", "HEAD")
	if err != nil {
		return state, nil, err
	}
	tracked, err := git(ctx, root, false, "ls-files", "--cached", "-z")
	if err != nil {
		return state, nil, err
	}
	names := strings.Split(tracked, "\x00")
	slices.Sort(names)
	files, content, total := []sourceFile{}, map[string][]byte{}, 0
	hasGo := false
	for _, name := range slices.Compact(names) {
		if name == "" {
			continue
		}
		parts := strings.FieldsFunc(name, func(r rune) bool { return r == '/' })
		if strings.HasPrefix(name, "/") || slices.Contains(parts, "..") {
			return state, nil, refuse("source_path", "Invalid tracked source path")
		}
		if !inScope(parts) {
			continue
		}
		data, err := readTracked(root, name, parts)
		if err != nil {
			return state, nil, err
		}
		if data == nil {
			continue
		}
		if total += len(data); total > maxSourceBytes {
			return state, nil, refuse("source_limit", "Tracked Go snapshot exceeds the local size limit")
		}
		content[name] = data
		hasGo = hasGo || strings.HasSuffix(name, ".go")
		files = append(files, sourceFile{Path: name, SHA256: digest(data), Size: len(data)})
	}
	if !hasGo {
		return state, nil, refuse("empty_source", "No tracked first-party Go source is available in this scope")
	}
	adapter, err := o.adapterDigest()
	if err != nil {
		return state, nil, err
	}
	body, err := encoded(files)
	if err != nil {
		return state, nil, err
	}
	var branch *string
	if value := strings.TrimSpace(head); value != "" {
		branch = &value
	}
	return sourceState{
		Schema: schema, Repository: root, Revision: strings.TrimSpace(revision), Branch: branch, ToolVersion: Version,
		Scope: scope{Languages: []string{"go"}, ModuleFiles: moduleFiles, ExcludedComponents: excludedComponents},
		Files: files, AdapterSHA256: adapter, SourceSHA256: digest(body),
	}, content, nil
}

func provenance(state sourceState) Provenance {
	return Provenance{Repository: state.Repository, Revision: state.Revision, Branch: state.Branch, ToolVersion: state.ToolVersion, SourceSHA256: state.SourceSHA256}
}

func regular(target string) bool {
	info, err := os.Stat(target)
	return err == nil && info.Mode().IsRegular()
}

func symlink(target string) bool {
	info, err := os.Lstat(target)
	return err == nil && info.Mode()&os.ModeSymlink != 0
}

// cachePaths keeps caches outside repositories and managed releases, which carry
// skills-manifest.json, and refuses components that redirect elsewhere.
func (o Options) cachePaths(state sourceState) (string, string, error) {
	requested := o.CacheRoot
	if requested == "" {
		home, err := o.Home()
		if err != nil {
			return "", "", err
		}
		requested = filepath.Join(home, ".cache", "agent-workflows", "graphify")
	}
	expanded, err := o.expand(requested)
	if err != nil {
		return "", "", err
	}
	root, err := resolvePath(expanded)
	if err != nil {
		return "", "", err
	}
	unsafe := inside(root, state.Repository)
	for ancestor := root; ; ancestor = filepath.Dir(ancestor) {
		unsafe = unsafe || regular(filepath.Join(ancestor, "skills-manifest.json"))
		if ancestor == filepath.Dir(ancestor) {
			break
		}
	}
	if unsafe {
		return "", "", refuse("cache_path", "Graphify cache must be outside repositories and managed skill releases")
	}
	body, err := encoded(state)
	if err != nil {
		return "", "", err
	}
	worktree := filepath.Join(root, digest([]byte(state.Repository))[:20])
	entry := filepath.Join(worktree, digest(body))
	for _, component := range []string{worktree, entry} {
		resolved, err := resolvePath(component)
		if err != nil {
			return "", "", err
		}
		info, statErr := os.Lstat(component)
		if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
			return "", "", statErr
		}
		if !inside(resolved, root) || statErr == nil && !info.IsDir() {
			return "", "", refuse("cache_path", "Graphify cache components must be owned directories, never symlink redirects")
		}
	}
	return worktree, entry, nil
}

func graphifyProcess(ctx context.Context, runtime string, arguments []string, directory, output string, timeout time.Duration) (int, string, string, error) {
	home, err := os.MkdirTemp("", "agent-graphify-home-")
	if err != nil {
		return 0, "", "", err
	}
	defer os.RemoveAll(home)
	return run(ctx, append([]string{runtime}, arguments...), directory, environment(runtime, output, home), timeout)
}

// executable asks the operating system, because permission bits alone do not say
// whether this user may run a file. An absolute target skips any PATH search.
func executable(target string) bool {
	absolute, err := filepath.Abs(target)
	if err != nil {
		return false
	}
	info, err := os.Stat(absolute)
	if err != nil || !info.Mode().IsRegular() {
		return false
	}
	_, err = exec.LookPath(absolute)
	return err == nil
}

// resolveRuntime selects the override, the dedicated environment, or PATH, and
// verifies the pinned version before any source reaches the tool.
func (o Options) resolveRuntime(ctx context.Context) (string, error) {
	var candidates []string
	if o.Runtime != "" {
		candidate, err := o.expand(o.Runtime)
		if err != nil {
			return "", err
		}
		candidates = []string{candidate}
	} else {
		home, err := o.Home()
		if err != nil {
			return "", err
		}
		candidates = []string{filepath.Join(home, ".venvs", "agent-graphify", "bin", "graphify")}
		if found, err := lookPath("graphify", os.Getenv("PATH")); err == nil {
			candidates = append(candidates, found)
		}
	}
	for _, candidate := range candidates {
		if !executable(candidate) {
			continue
		}
		runtime, err := filepath.Abs(candidate)
		if err != nil {
			return "", err
		}
		code, stdout, _, err := graphifyProcess(ctx, runtime, []string{"--version"}, filepath.Dir(runtime), "", versionTimeout)
		if err != nil {
			return "", err
		}
		fields := strings.TrimPrefix(strings.TrimSpace(stdout), "graphify")
		if code != 0 || strings.TrimSpace(fields) != Version || fields == strings.TrimSpace(stdout) {
			return "", refuse("runtime_version", "Explicit use requires graphifyy=="+Version+"; install the pinned separate runtime")
		}
		return runtime, nil
	}
	return "", refuse("runtime_missing", "Install graphifyy=="+Version+" in ~/.venvs/agent-graphify; normal source discovery remains available")
}

func writeJSON(target string, value any) error {
	data, err := encoded(value)
	if err != nil {
		return err
	}
	if err := os.WriteFile(target, append(data, '\n'), 0o600); err != nil {
		return err
	}
	return os.Chmod(target, 0o600)
}

// decodeGraph retains number spellings, because a validated graph is rewritten
// with only its source paths changed.
func decodeGraph(data []byte) (any, error) {
	decoder := json1.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return nil, errors.New("trailing graph data")
	}
	return value, nil
}

func encodeGraph(value any) ([]byte, error) {
	var buffer bytes.Buffer
	encoder := json1.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

// components normalizes a reference without resolving "..", so a path that only
// reaches an indexed file through a parent component stays unindexed.
func components(value string) []string {
	return slices.DeleteFunc(strings.Split(value, "/"), func(part string) bool { return part == "" || part == "." })
}

func relativeTo(value, root string) (string, bool) {
	parts, base := components(value), components(root)
	if len(parts) < len(base) || !slices.Equal(parts[:len(base)], base) {
		return "", false
	}
	return strings.Join(parts[len(base):], "/"), true
}

func mappedSource(value string, snapshot string, state sourceState) (string, error) {
	name := strings.Join(components(value), "/")
	if filepath.IsAbs(value) {
		relative, ok := relativeTo(value, snapshot)
		if !ok {
			if relative, ok = relativeTo(value, state.Repository); !ok {
				return "", refuse("graph_reference", "Graph contains an out-of-scope source reference")
			}
		}
		name = relative
	}
	if name == "" {
		name = "."
	}
	if !slices.ContainsFunc(state.Files, func(file sourceFile) bool { return file.Path == name }) {
		return "", refuse("graph_reference", "Graph references an unindexed source: "+name)
	}
	return filepath.Join(state.Repository, filepath.FromSlash(name)), nil
}

// lineCount counts lines the way the indexed file's location labels do.
func lineCount(data []byte) int {
	data = bytes.ReplaceAll(data, []byte("\r\n"), []byte("\n"))
	count := bytes.Count(data, []byte("\n")) + bytes.Count(data, []byte("\r"))
	if len(data) > 0 && data[len(data)-1] != '\n' && data[len(data)-1] != '\r' {
		count++
	}
	return max(1, count)
}

func checkLocation(value any, target string, lines map[string]int) error {
	location := ""
	if value != nil {
		location = fmt.Sprint(value)
	}
	match := sourceLocation.FindStringSubmatch(location)
	if match == nil {
		return nil
	}
	start, startErr := strconv.Atoi(match[1])
	end := start
	var endErr error
	if match[2] != "" {
		end, endErr = strconv.Atoi(match[2])
	}
	if startErr != nil || endErr != nil || start < 1 || start > end || end > lines[target] {
		return refuse("graph_reference", "Graph source location is outside the indexed file")
	}
	return nil
}

func checkRecord(value any, state sourceState, snapshot string, rewrite bool, lines map[string]int, represented map[string]bool) error {
	item, ok := value.(map[string]any)
	if !ok {
		return refuse("graph_schema", "Invalid graph record")
	}
	source, ok := item["source_file"].(string)
	if !ok || source == "" {
		return nil
	}
	mapped, err := mappedSource(source, snapshot, state)
	if err != nil {
		return err
	}
	represented[mapped] = true
	if !rewrite && source != mapped {
		return refuse("graph_reference", "Cached graph reference is not mapped to the current workspace")
	}
	item["source_file"] = mapped
	return checkLocation(item["source_location"], mapped, lines)
}

func confidenceLabel(edge any) string {
	item, _ := edge.(map[string]any)
	value, present := item["confidence"]
	switch {
	case !present:
		return "UNSPECIFIED"
	case value == nil:
		return "None"
	default:
		return fmt.Sprint(value)
	}
}

// validateGraph proves a graph is complete for its snapshot. With rewrite it
// also maps sandbox source paths onto the workspace and stores the result.
func validateGraph(target string, state sourceState, snapshot string, rewrite bool) (Summary, error) {
	info, err := os.Lstat(target)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Size() > maxGraphBytes {
		return Summary{}, refuse("graph_incomplete", "Graph output is missing, unsafe or too large")
	}
	data, err := os.ReadFile(target)
	if err != nil {
		return Summary{}, refuse("graph_incomplete", "Graph output is unreadable")
	}
	value, err := decodeGraph(data)
	if err != nil {
		return Summary{}, refuse("graph_incomplete", "Graph output is unreadable")
	}
	graph, ok := value.(map[string]any)
	if !ok {
		return Summary{}, refuse("graph_incomplete", "Extraction did not produce graph nodes")
	}
	nodes, ok := graph["nodes"].([]any)
	if !ok || len(nodes) == 0 {
		return Summary{}, refuse("graph_incomplete", "Extraction did not produce graph nodes")
	}
	edgeValue, present := graph["links"]
	if !present {
		edgeValue = graph["edges"]
	}
	edges, ok := edgeValue.([]any)
	if !ok || graph["directed"] == true {
		return Summary{}, refuse("graph_schema", "Expected Graphify's undirected structural graph")
	}
	lines, required := map[string]int{}, []string{}
	for _, file := range state.Files {
		indexed := filepath.Join(state.Repository, filepath.FromSlash(file.Path))
		content, err := os.ReadFile(indexed)
		if err != nil {
			return Summary{}, err
		}
		lines[indexed] = lineCount(content)
		if strings.HasSuffix(file.Path, ".go") {
			required = append(required, indexed)
		}
	}
	represented := map[string]bool{}
	for _, record := range slices.Concat(nodes, edges) {
		if err := checkRecord(record, state, snapshot, rewrite, lines, represented); err != nil {
			return Summary{}, err
		}
	}
	missing := slices.DeleteFunc(slices.Clone(required), func(name string) bool { return represented[name] })
	if len(missing) > 0 {
		slices.Sort(missing)
		return Summary{}, refuse("graph_incomplete", fmt.Sprintf("Extraction omitted %d Go source file(s), including %s", len(missing), filepath.Base(missing[0])))
	}
	if rewrite {
		data, err := encodeGraph(graph)
		if err != nil {
			return Summary{}, err
		}
		if err := os.WriteFile(target, data, 0o600); err != nil {
			return Summary{}, err
		}
		if err := os.Chmod(target, 0o600); err != nil {
			return Summary{}, err
		}
	}
	labels := map[string]int{}
	for _, edge := range edges {
		labels[confidenceLabel(edge)]++
	}
	return Summary{Nodes: len(nodes), Edges: len(edges), SourceFiles: len(required), EdgeLabels: labels}, nil
}

func graphPath(entry string) string { return filepath.Join(entry, "graphify-out", "graph.json") }

func staleGraph() error {
	return refuse("stale_graph", "No complete graph matches this worktree/revision/content; run build or use normal discovery")
}

func currentEntry(state sourceState, entry string) (readyEntry, Summary, error) {
	var ready readyEntry
	for _, name := range []string{"", "ready.json", "graphify-out", "source"} {
		if symlink(filepath.Join(entry, name)) {
			return ready, Summary{}, staleGraph()
		}
	}
	data, err := os.ReadFile(filepath.Join(entry, "ready.json"))
	if err != nil {
		return ready, Summary{}, staleGraph()
	}
	if err := json.Unmarshal(data, &ready, json.RejectUnknownMembers(true)); err != nil {
		return ready, Summary{}, staleGraph()
	}
	graph, err := os.ReadFile(graphPath(entry))
	if err != nil || !reflect.DeepEqual(ready.Source, state) || ready.GraphSHA256 != digest(graph) {
		return ready, Summary{}, staleGraph()
	}
	summary, err := validateGraph(graphPath(entry), state, filepath.Join(entry, "source"), false)
	if err != nil {
		return ready, Summary{}, staleGraph()
	}
	return ready, summary, nil
}

func writeSnapshot(directory string, content map[string][]byte) error {
	for name, data := range content {
		target := filepath.Join(directory, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(target, data, 0o600); err != nil {
			return err
		}
	}
	return nil
}

// extract runs code-only extraction over a private snapshot and leaves a
// complete, rewritten graph in pending or an error.
func (o Options) extract(ctx context.Context, runtime, pending string, content map[string][]byte, state sourceState) (Summary, error) {
	snapshot, output := filepath.Join(pending, "source"), filepath.Join(pending, "graphify-out")
	if err := os.Mkdir(snapshot, 0o755); err != nil {
		return Summary{}, err
	}
	if err := writeSnapshot(snapshot, content); err != nil {
		return Summary{}, err
	}
	code, stdout, stderr, err := graphifyProcess(ctx, runtime, []string{"extract", snapshot, "--code-only", "--no-dedup"}, pending, output, o.Timeout)
	if err != nil {
		return Summary{}, err
	}
	if err := os.WriteFile(filepath.Join(pending, "extraction.stdout"), []byte(stdout), 0o644); err != nil {
		return Summary{}, err
	}
	if err := os.WriteFile(filepath.Join(pending, "extraction.stderr"), []byte(stderr), 0o644); err != nil {
		return Summary{}, err
	}
	if code != 0 || extractionFailure.MatchString(stdout+stderr) {
		return Summary{}, refuse("extraction_failed", "Code-only extraction failed; no graph was activated")
	}
	return validateGraph(filepath.Join(output, "graph.json"), state, snapshot, true)
}

func cacheDirectory(target string) (bool, error) {
	info, err := os.Lstat(target)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !info.IsDir() {
		return false, refuse("cache_path", "Graphify cache entries and recovery paths must be directories, never symlink redirects: "+target)
	}
	return true, nil
}

// activate serializes only publication and recovery, never extraction. A blank
// pending permits recovery/cleanup for a graph which was already reusable.
func (o Options) activate(ctx context.Context, state sourceState, parent, entry, pending string) (Summary, bool, error) {
	lockCtx, cancel := context.WithTimeout(ctx, o.Timeout)
	release, err := acquireCacheLock(lockCtx, entry+".lock")
	cancel()
	if err != nil {
		if ctx.Err() != nil {
			return Summary{}, false, ctx.Err()
		}
		if errors.Is(err, context.DeadlineExceeded) {
			return Summary{}, false, refuse("timeout", "Graphify cache publication timed out; no incomplete graph was activated")
		}
		return Summary{}, false, err
	}
	defer release()
	checkedParent, checkedEntry, err := o.cachePaths(state)
	if err != nil {
		return Summary{}, false, err
	}
	if checkedParent != parent || checkedEntry != entry {
		return Summary{}, false, refuse("cache_path", "Graphify cache location changed while waiting to publish")
	}
	again, _, err := o.source(ctx)
	if err != nil {
		return Summary{}, false, err
	}
	if !reflect.DeepEqual(again, state) {
		return Summary{}, false, refuse("source_changed", "Source changed before cache publication; build a fresh graph")
	}
	rename := o.renameCache
	if rename == nil {
		rename = os.Rename
	}
	repair := entry + ".repair"
	repairExists, err := cacheDirectory(repair)
	if err != nil {
		return Summary{}, false, err
	}
	entryExists, err := cacheDirectory(entry)
	if err != nil {
		return Summary{}, false, err
	}
	cleanupRepair := func() error {
		if _, err := cacheDirectory(repair); err != nil {
			return err
		}
		if err := os.RemoveAll(repair); err != nil {
			return fmt.Errorf("clean Graphify cache recovery directory %s: %w", repair, err)
		}
		repairExists = false
		return nil
	}
	winner := func() (Summary, bool, error) {
		present, err := cacheDirectory(entry)
		if err != nil {
			return Summary{}, false, err
		}
		if present {
			if _, summary, err := currentEntry(state, entry); err == nil {
				if repairExists {
					if err := cleanupRepair(); err != nil {
						return Summary{}, false, err
					}
				}
				return summary, true, nil
			}
		}
		return Summary{}, false, nil
	}
	// An interrupted publication must first put its preserved entry back. No
	// candidate, successful or failed, is allowed to strand the only old copy.
	if repairExists && !entryExists {
		if err := rename(repair, entry); err != nil {
			if summary, reused, winnerErr := winner(); reused {
				return summary, true, nil
			} else if winnerErr != nil {
				return Summary{}, false, fmt.Errorf("recover Graphify cache entry: %w; inspect destination: %w; preserved recovery directory: %s", err, winnerErr, repair)
			}
			return Summary{}, false, fmt.Errorf("recover Graphify cache entry: %w; preserved recovery directory: %s", err, repair)
		}
		repairExists, entryExists = false, true
	}
	if summary, reused, err := winner(); reused || err != nil {
		return summary, reused, err
	}
	if pending == "" {
		return Summary{}, false, nil
	}
	// Reuse the complete-entry validator before moving any corruption, even
	// after a potentially long wait. Both-invalid leftovers can then discard
	// only the auxiliary copy: the active old directory still remains intact.
	_, summary, err := currentEntry(state, pending)
	if err != nil {
		return Summary{}, false, err
	}
	if repairExists {
		if err := cleanupRepair(); err != nil {
			return Summary{}, false, err
		}
	}
	if entryExists {
		if err := rename(entry, repair); err != nil {
			return Summary{}, false, fmt.Errorf("quarantine invalid Graphify cache entry: %w", err)
		}
		repairExists = true
	}
	publicationErr := rename(pending, entry)
	if publicationErr == nil {
		if repairExists {
			if err := cleanupRepair(); err != nil {
				return Summary{}, false, err
			}
		}
		return summary, false, nil
	}
	// A writer which did not participate in our lock may have installed a
	// valid entry. Preserve it and report its actual graph, not our candidate.
	if summary, reused, winnerErr := winner(); reused {
		return summary, true, nil
	} else if winnerErr != nil {
		if !repairExists {
			return Summary{}, false, fmt.Errorf("publish Graphify cache entry: %w; inspect destination: %w", publicationErr, winnerErr)
		}
		return Summary{}, false, fmt.Errorf("publish Graphify cache entry: %w; inspect destination: %w; preserved recovery directory: %s", publicationErr, winnerErr, repair)
	}
	if !repairExists {
		return Summary{}, false, fmt.Errorf("publish Graphify cache entry: %w", publicationErr)
	}
	present, err := cacheDirectory(entry)
	if err != nil {
		return Summary{}, false, fmt.Errorf("publish Graphify cache entry: %w; inspect destination: %w; preserved recovery directory: %s", publicationErr, err, repair)
	}
	if present {
		return Summary{}, false, fmt.Errorf("publish Graphify cache entry: %w; destination was not absent, preserved recovery directory: %s", publicationErr, repair)
	}
	if restoreErr := rename(repair, entry); restoreErr != nil {
		if summary, reused, winnerErr := winner(); reused {
			return summary, true, nil
		} else if winnerErr != nil {
			return Summary{}, false, fmt.Errorf("publish Graphify cache entry: %w; restore previous entry: %w; inspect destination: %w; preserved recovery directory: %s", publicationErr, restoreErr, winnerErr, repair)
		}
		return Summary{}, false, fmt.Errorf("publish Graphify cache entry: %w; restore previous entry: %w; preserved recovery directory: %s", publicationErr, restoreErr, repair)
	}
	return Summary{}, false, fmt.Errorf("publish Graphify cache entry: %w; previous entry restored", publicationErr)
}

// Build reuses a current graph or extracts and activates a new one. A failed or
// incomplete extraction never replaces anything, and a concurrent success wins.
func Build(ctx context.Context, o Options) (BuildResult, error) {
	var result BuildResult
	state, content, err := o.source(ctx)
	if err != nil {
		return result, err
	}
	parent, entry, err := o.cachePaths(state)
	if err != nil {
		return result, err
	}
	runtime, err := o.resolveRuntime(ctx)
	if err != nil {
		return result, err
	}
	result = BuildResult{Status: "ok", Command: "build", Graph: graphPath(entry), Provenance: provenance(state)}
	if _, summary, err := currentEntry(state, entry); err == nil {
		repairExists, err := cacheDirectory(entry + ".repair")
		if err != nil {
			return result, err
		}
		if repairExists {
			if summary, reused, err := o.activate(ctx, state, parent, entry, ""); reused || err != nil {
				result.Summary, result.Reused = summary, reused
				return result, err
			}
		} else {
			result.Summary, result.Reused = summary, true
			return result, nil
		}
	}
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return result, err
	}
	if err := os.Chmod(parent, 0o700); err != nil {
		return result, err
	}
	pending, err := os.MkdirTemp(parent, ".build-")
	if err != nil {
		return result, err
	}
	defer os.RemoveAll(pending)
	started := time.Now()
	summary, err := o.extract(ctx, runtime, pending, content, state)
	if err != nil {
		return result, err
	}
	again, _, err := o.source(ctx)
	if err != nil {
		return result, err
	}
	if !reflect.DeepEqual(again, state) {
		return result, refuse("source_changed", "Source changed during extraction; build a fresh graph")
	}
	if err := os.WriteFile(filepath.Join(pending, "graphify-out", ".graphify_root"), []byte(state.Repository), 0o644); err != nil {
		return result, err
	}
	graph, err := os.ReadFile(filepath.Join(pending, "graphify-out", "graph.json"))
	if err != nil {
		return result, err
	}
	ready := readyEntry{Source: state, GraphSHA256: digest(graph), Summary: summary, Runtime: runtime, BuiltAt: time.Now().UTC().Format(time.RFC3339Nano)}
	if err := writeJSON(filepath.Join(pending, "ready.json"), ready); err != nil {
		return result, err
	}
	if _, _, err := currentEntry(state, pending); err != nil {
		return result, err
	}
	if result.Summary, result.Reused, err = o.activate(ctx, state, parent, entry, pending); err != nil {
		return result, err
	}
	elapsed := seconds(started)
	result.ElapsedSeconds = &elapsed
	return result, nil
}

// Query answers from the graph that exactly matches current source. Upstream may
// overrun its budget when all nodes fit, so the response is bounded here too;
// truncation never establishes the absence of omitted relationships.
func Query(ctx context.Context, o Options) (QueryResult, error) {
	var result QueryResult
	state, _, err := o.source(ctx)
	if err != nil {
		return result, err
	}
	_, entry, err := o.cachePaths(state)
	if err != nil {
		return result, err
	}
	ready, summary, err := currentEntry(state, entry)
	if err != nil {
		return result, err
	}
	runtime, err := o.resolveRuntime(ctx)
	if err != nil {
		return result, err
	}
	graph := graphPath(entry)
	started := time.Now()
	code, stdout, _, err := graphifyProcess(ctx, runtime, []string{"query", o.Question, "--budget", strconv.Itoa(o.Budget), "--graph", graph}, entry, filepath.Dir(graph), o.Timeout)
	if err != nil {
		return result, err
	}
	status, known := map[int]string{0: "ok", 3: "ambiguous", 4: "no_match"}[code]
	if !known {
		return result, refuse("query_failed", "Local Graphify query failed; use normal source discovery")
	}
	again, _, err := o.source(ctx)
	if err != nil {
		return result, err
	}
	data, err := os.ReadFile(graph)
	if err != nil {
		return result, err
	}
	if !reflect.DeepEqual(again, state) || digest(data) != ready.GraphSHA256 {
		return result, refuse("source_changed", "Source or graph changed during query; discard this result and rebuild")
	}
	output := []rune(stdout)
	limit := o.Budget * 3
	truncated := len(output) > limit
	var notice *string
	if truncated {
		// Prefer the last complete line within the bound.
		cut := limit
		for index := limit - 1; index > 0; index-- {
			if output[index] == '\n' {
				cut = index
				break
			}
		}
		output = output[:cut]
		value := "Context cut at the wrapper budget; omitted relationships remain unknown"
		notice = &value
	}
	return QueryResult{Status: status, Command: "query", Graph: graph, Provenance: provenance(state), Qualification: qualification,
		Context: string(output), ContextChars: len(output), Truncated: truncated, TruncationNotice: notice, Summary: summary, ElapsedSeconds: seconds(started)}, nil
}
