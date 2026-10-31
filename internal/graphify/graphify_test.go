package graphify

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

// fakeConfig scripts the stand-in runtime. The test binary re-executes itself as
// that runtime, because the guarded child environment passes nothing through.
type fakeConfig struct {
	Version   string `json:"version"`
	Mode      string `json:"mode"`
	QueryExit int    `json:"query_exit"`
	LongQuery bool   `json:"long_query"`
	Log       string `json:"log"`
	Late      string `json:"late"`
	Repo      string `json:"repo"`
}

type fakeCall struct {
	Args []string          `json:"args"`
	Env  map[string]string `json:"env"`
}

func TestMain(m *testing.M) {
	if len(os.Args) > 2 && os.Args[1] == "fake-graphify" {
		os.Exit(fakeGraphify(os.Args[2], os.Args[3:]))
	}
	os.Exit(m.Run())
}

func logCall(config fakeConfig, args []string) {
	env := map[string]string{}
	for _, item := range os.Environ() {
		name, value, _ := strings.Cut(item, "=")
		env[name] = value
	}
	data, _ := json.Marshal(fakeCall{args, env})
	log, err := os.OpenFile(config.Log, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err == nil {
		defer log.Close()
		fmt.Fprintln(log, string(data))
	}
}

func fakeExtract(config fakeConfig, args []string) int {
	if !slices.Equal(args[2:], []string{"--code-only", "--no-dedup"}) {
		return 8
	}
	switch config.Mode {
	case "fail":
		return 9
	case "timeout":
		// A delayed descendant proves the whole process group is stopped.
		child := exec.Command("/bin/sh", "-c", "sleep 0.7; echo late > '"+config.Late+"'")
		_ = child.Start()
		time.Sleep(5 * time.Second)
	}
	var files []string
	_ = filepath.WalkDir(args[1], func(path string, _ fs.DirEntry, err error) error {
		if err == nil && strings.HasSuffix(path, ".go") {
			files = append(files, path)
		}
		return nil
	})
	if config.Mode == "omit" {
		files = files[:1]
	}
	nodes := []map[string]any{}
	for index, file := range files {
		label := strings.TrimSuffix(filepath.Base(file), ".go")
		nodes = append(nodes, map[string]any{"id": strconv.Itoa(index), "label": label, "source_file": file, "source_location": "1"})
	}
	edges := []map[string]any{}
	if len(files) > 1 {
		edges = append(edges, map[string]any{"source": "0", "target": "1", "relation": "calls", "confidence": "INFERRED", "source_file": files[0], "source_location": "2"})
	}
	switch config.Mode {
	case "foreign":
		nodes[0]["source_file"] = "/outside/secret.go"
	case "location":
		nodes[0]["source_location"] = "L9999"
	}
	output := os.Getenv("GRAPHIFY_OUT")
	if err := os.MkdirAll(output, 0o700); err != nil {
		return 7
	}
	data, _ := json.Marshal(map[string]any{"nodes": nodes, "links": edges, "directed": false})
	if err := os.WriteFile(filepath.Join(output, "graph.json"), data, 0o600); err != nil {
		return 7
	}
	if config.Mode == "change" {
		_ = os.WriteFile(filepath.Join(config.Repo, "main.go"), []byte("package main\nfunc changed(){}\n"), 0o600)
	}
	return 0
}

func fakeQuery(config fakeConfig, args []string) int {
	if config.LongQuery {
		fmt.Println(strings.Repeat("x", 50000))
		return config.QueryExit
	}
	data, err := os.ReadFile(args[slices.Index(args, "--graph")+1])
	if err != nil {
		return 7
	}
	var graph struct {
		Nodes []struct {
			Label  string `json:"label"`
			Source string `json:"source_file"`
		} `json:"nodes"`
	}
	if err := json.Unmarshal(data, &graph); err != nil {
		return 7
	}
	for _, node := range graph.Nodes {
		fmt.Printf("NODE %s [src=%s loc=1]\n", node.Label, node.Source)
	}
	fmt.Println("EDGE main --calls [INFERRED]--> other")
	return config.QueryExit
}

func fakeGraphify(configPath string, args []string) int {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return 6
	}
	var config fakeConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return 6
	}
	logCall(config, args)
	switch {
	case slices.Equal(args, []string{"--version"}):
		fmt.Println("graphify " + config.Version)
		return 0
	case args[0] == "extract":
		return fakeExtract(config, args)
	case args[0] == "query":
		return fakeQuery(config, args)
	}
	return 8
}

type harness struct {
	root, repo, cache, runtime, log string
	config                          fakeConfig
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	root, err := os.MkdirTemp("", "graphify test ")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(root) })
	if root, err = filepath.EvalSymlinks(root); err != nil {
		t.Fatal(err)
	}
	h := &harness{root: root, repo: filepath.Join(root, "repository with spaces"), cache: filepath.Join(root, "cache"), runtime: filepath.Join(root, "graphify"), log: filepath.Join(root, "runtime-calls.jsonl")}
	h.config = fakeConfig{Version: Version, Log: h.log, Late: filepath.Join(root, "late-child"), Repo: h.repo}
	if err := os.Mkdir(h.repo, 0o700); err != nil {
		t.Fatal(err)
	}
	h.git(t, "init", "-q")
	h.git(t, "config", "user.name", "Fixture")
	h.git(t, "config", "user.email", "fixture@example.invalid")
	h.write(t, "go.mod", "module example.invalid/fixture\n\ngo 1.25\n")
	h.write(t, "main.go", "package main\nfunc main() { helper() }\nfunc helper() {}\n")
	h.write(t, "other.go", "package main\nfunc other() {}\n")
	h.git(t, "add", ".")
	h.git(t, "commit", "-qm", "fixture")
	h.install(t)
	return h
}

// install writes the runtime script and its current scripted behavior.
func (h *harness) install(t *testing.T) {
	t.Helper()
	data, err := json.Marshal(h.config)
	if err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(h.root, "fake.json")
	if err := os.WriteFile(configPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	script := fmt.Sprintf("#!/bin/sh\nexec '%s' fake-graphify '%s' \"$@\"\n", binary, configPath)
	if err := os.WriteFile(h.runtime, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
}

func (h *harness) write(t *testing.T, name, data string) string {
	t.Helper()
	target := filepath.Join(h.repo, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	return target
}

func (h *harness) git(t *testing.T, args ...string) string {
	t.Helper()
	output, err := exec.Command("git", append([]string{"-C", h.repo}, args...)...).Output()
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return strings.TrimSpace(string(output))
}

func (h *harness) options() Options {
	return Options{Repo: h.repo, Runtime: h.runtime, CacheRoot: h.cache, Timeout: 10 * time.Second, Budget: 2000, Question: "main helper",
		Home: func() (string, error) { return h.root, nil }, Executable: os.Executable}
}

func (h *harness) calls(t *testing.T) []fakeCall {
	t.Helper()
	data, err := os.ReadFile(h.log)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	var calls []fakeCall
	for line := range strings.Lines(string(data)) {
		var call fakeCall
		if err := json.Unmarshal([]byte(line), &call); err != nil {
			t.Fatal(err)
		}
		calls = append(calls, call)
	}
	return calls
}

func (h *harness) extractions(t *testing.T) int {
	t.Helper()
	count := 0
	for _, call := range h.calls(t) {
		if call.Args[0] == "extract" {
			count++
		}
	}
	return count
}

func wantCode(t *testing.T, err error, code string) {
	t.Helper()
	refused, ok := errors.AsType[*Error](err)
	if !ok || refused.Code != code {
		t.Fatalf("error = %v, want code %s", err, code)
	}
}

func globCache(t *testing.T, h *harness, name string) []string {
	t.Helper()
	var found []string
	_ = filepath.WalkDir(h.cache, func(path string, entry fs.DirEntry, err error) error {
		if err == nil {
			if matched, _ := filepath.Match(name, entry.Name()); matched {
				found = append(found, path)
			}
		}
		return nil
	})
	return found
}

func TestFreshBuildQueryMapsPathsAndPreservesInferredLabels(t *testing.T) {
	h := newHarness(t)
	result, err := Build(t.Context(), h.options())
	if err != nil {
		t.Fatal(err)
	}
	if result.Reused || result.ElapsedSeconds == nil || result.Status != "ok" || result.Command != "build" {
		t.Fatalf("result = %+v", result)
	}
	data, err := os.ReadFile(result.Graph)
	if err != nil {
		t.Fatal(err)
	}
	var graph struct {
		Nodes []struct {
			Source string `json:"source_file"`
		} `json:"nodes"`
		Links []struct {
			Confidence string `json:"confidence"`
		} `json:"links"`
	}
	if err := json.Unmarshal(data, &graph); err != nil {
		t.Fatal(err)
	}
	for _, node := range graph.Nodes {
		if !strings.HasPrefix(node.Source, h.repo) {
			t.Fatalf("unmapped source %s", node.Source)
		}
	}
	if graph.Links[0].Confidence != "INFERRED" || result.Summary.EdgeLabels["INFERRED"] != 1 || result.Summary.SourceFiles != 2 || result.Summary.Directed {
		t.Fatalf("summary = %+v", result.Summary)
	}
	query, err := Query(t.Context(), h.options())
	if err != nil {
		t.Fatal(err)
	}
	if query.Status != "ok" || !strings.Contains(query.Context, "INFERRED") || !strings.Contains(query.Qualification, "interfaces") || query.Truncated || query.TruncationNotice != nil {
		t.Fatalf("query = %+v", query)
	}
	if _, err := os.Stat(filepath.Join(h.repo, "graphify-out")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("graph output entered the repository")
	}
	reused, err := Build(t.Context(), h.options())
	if err != nil || !reused.Reused || reused.ElapsedSeconds != nil {
		t.Fatalf("reuse = %+v %v", reused, err)
	}
	if h.extractions(t) != 1 {
		t.Fatalf("%d extractions", h.extractions(t))
	}
}

func TestSourceFingerprintMatchesHelper(t *testing.T) {
	h := newHarness(t)
	state, _, err := h.options().source(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if state.SourceSHA256 != "66a4d4776e1d7162849563f362272c5a57e4164dcf751c9ee34dde4fb5a559a6" {
		t.Fatalf("source digest = %s", state.SourceSHA256)
	}
	body, err := encoded(state.Scope)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"languages":["go"],"module_files":["go.mod","go.sum"],"excluded_components":[".git",".venv","fixture","fixtures","node_modules","testdata","third_party","vendor"]}`
	if string(body) != want {
		t.Fatalf("scope = %s", body)
	}
}

func TestExclusionsAndUntrackedFilesNeverEnterSnapshot(t *testing.T) {
	h := newHarness(t)
	for _, name := range []string{"third_party/library/a.go", "vendor/b.go", "internal/testdata/c.go", "fixtures/d.go"} {
		h.write(t, name, "package excluded\n")
	}
	h.git(t, "add", ".")
	h.write(t, "untracked.go", "package secret\n")
	result, err := Build(t.Context(), h.options())
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(filepath.Dir(filepath.Dir(result.Graph)), "ready.json"))
	if err != nil {
		t.Fatal(err)
	}
	var ready readyEntry
	if err := json.Unmarshal(data, &ready); err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, file := range ready.Source.Files {
		names = append(names, file.Path)
	}
	if !slices.Equal(names, []string{"go.mod", "main.go", "other.go"}) {
		t.Fatalf("snapshot = %v", names)
	}
}

func TestMinimalEnvironmentBlocksCredentialsAndInheritedModes(t *testing.T) {
	h := newHarness(t)
	blocked := []string{"OPENAI_API_KEY", "TYPESAFE_API_KEY", "SUBQ_API_KEY", "ANTHROPIC_API_KEY", "GEMINI_API_KEY", "PYTHONPATH", "GRAPHIFY_QUERY_LOG", "GRAPHIFY_QUERY_LOG_ENABLE", "GRAPHIFY_OUT"}
	for _, name := range blocked {
		t.Setenv(name, "do-not-forward")
	}
	if _, err := Build(t.Context(), h.options()); err != nil {
		t.Fatal(err)
	}
	if _, err := Query(t.Context(), h.options()); err != nil {
		t.Fatal(err)
	}
	calls := h.calls(t)
	if len(calls) == 0 {
		t.Fatal("no runtime calls")
	}
	realHome, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	for _, call := range calls {
		for _, name := range blocked {
			if _, present := call.Env[name]; present && name != "GRAPHIFY_OUT" {
				t.Fatalf("%s was forwarded", name)
			}
		}
		for name, value := range map[string]string{"GRAPHIFY_NO_AUTO_REFRESH": "1", "GRAPHIFY_QUERY_LOG_DISABLE": "1", "PYTHONDONTWRITEBYTECODE": "1", "PYTHONHASHSEED": "0"} {
			if call.Env[name] != value {
				t.Fatalf("%s = %q", name, call.Env[name])
			}
		}
		home := call.Env["HOME"]
		if home == realHome || !strings.HasPrefix(call.Env["XDG_CONFIG_HOME"], home) || !strings.HasPrefix(call.Env["XDG_CACHE_HOME"], home) {
			t.Fatalf("home sandbox = %v", call.Env)
		}
		if _, err := os.Stat(home); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("temporary home survived")
		}
	}
}

func TestMissingAndWrongRuntimeFailBeforeExtraction(t *testing.T) {
	h := newHarness(t)
	options := h.options()
	options.Runtime = filepath.Join(h.root, "missing")
	_, err := Build(t.Context(), options)
	wantCode(t, err, "runtime_missing")
	if !strings.Contains(err.Error(), "Install graphifyy==0.9.91") {
		t.Fatalf("message = %v", err)
	}
	h.config.Version = "0.0.1"
	h.install(t)
	_, err = Build(t.Context(), h.options())
	wantCode(t, err, "runtime_version")
	if !strings.Contains(err.Error(), "requires graphifyy") || h.extractions(t) != 0 {
		t.Fatalf("message = %v, %d extractions", err, h.extractions(t))
	}
}

func TestMissingDirtyBranchAndCorruptGraphsAreRefused(t *testing.T) {
	h := newHarness(t)
	_, err := Query(t.Context(), h.options())
	wantCode(t, err, "stale_graph")
	result, err := Build(t.Context(), h.options())
	if err != nil {
		t.Fatal(err)
	}
	h.write(t, "main.go", "package main\nfunc changed() {}\n")
	if _, err := Query(t.Context(), h.options()); err == nil {
		t.Fatal("dirty source accepted")
	}
	h.git(t, "checkout", "--", "main.go")
	h.git(t, "checkout", "-qb", "another-branch")
	if _, err := Query(t.Context(), h.options()); err == nil {
		t.Fatal("other branch accepted")
	}
	h.git(t, "checkout", "-")
	if err := os.WriteFile(result.Graph, []byte(`{"nodes":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Query(t.Context(), h.options()); err == nil {
		t.Fatal("corrupt graph accepted")
	}
}

func TestTrackedDeletionAndNewTrackedSourceChangeFingerprint(t *testing.T) {
	h := newHarness(t)
	options := h.options()
	before, _, err := options.source(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(h.repo, "other.go")); err != nil {
		t.Fatal(err)
	}
	deleted, _, err := options.source(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if before.SourceSHA256 == deleted.SourceSHA256 {
		t.Fatal("deletion kept the fingerprint")
	}
	h.write(t, "new.go", "package main\n")
	h.git(t, "add", "new.go")
	added, _, err := options.source(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if deleted.SourceSHA256 == added.SourceSHA256 {
		t.Fatal("addition kept the fingerprint")
	}
}

func TestEscapingAndInternalSourceSymlinksAreRefused(t *testing.T) {
	h := newHarness(t)
	outside := filepath.Join(h.root, "outside.go")
	if err := os.WriteFile(outside, []byte("package outside\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{outside, filepath.Join(h.repo, "main.go")} {
		link := filepath.Join(h.repo, "linked.go")
		if err := os.Symlink(target, link); err != nil {
			t.Fatal(err)
		}
		h.git(t, "add", "linked.go")
		_, _, err := h.options().source(t.Context())
		wantCode(t, err, "source_symlink")
		h.git(t, "rm", "-f", "linked.go")
	}
}

func TestCacheInsideRepositoryOrSkillSnapshotIsRefused(t *testing.T) {
	h := newHarness(t)
	options := h.options()
	options.CacheRoot = filepath.Join(h.repo, ".cache")
	_, err := Build(t.Context(), options)
	wantCode(t, err, "cache_path")
	released := filepath.Join(h.root, "managed-release")
	if err := os.Mkdir(released, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(released, "skills-manifest.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	options.CacheRoot = filepath.Join(released, "cache")
	_, err = Build(t.Context(), options)
	wantCode(t, err, "cache_path")
}

func TestCacheChildRedirectsCannotWriteIntoRepositoryOrRelease(t *testing.T) {
	h := newHarness(t)
	options := h.options()
	state, _, err := options.source(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	parent, entry, err := options.cachePaths(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(h.cache, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(h.repo, parent); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(h.repo)
	if err != nil {
		t.Fatal(err)
	}
	_, err = Build(t.Context(), options)
	wantCode(t, err, "cache_path")
	if after, err := os.Stat(h.repo); err != nil || after.Mode() != before.Mode() {
		t.Fatalf("repository mode changed: %v", err)
	}
	if err := os.Remove(parent); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(parent, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(h.repo, entry); err != nil {
		t.Fatal(err)
	}
	_, err = Build(t.Context(), options)
	wantCode(t, err, "cache_path")
	if err := os.Remove(entry); err != nil {
		t.Fatal(err)
	}
	result, err := Build(t.Context(), options)
	if err != nil {
		t.Fatal(err)
	}
	ready := filepath.Join(filepath.Dir(filepath.Dir(result.Graph)), "ready.json")
	saved := filepath.Join(h.root, "saved-ready.json")
	if err := os.Rename(ready, saved); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(saved, ready); err != nil {
		t.Fatal(err)
	}
	_, err = Query(t.Context(), options)
	wantCode(t, err, "stale_graph")
}

func TestFailedPartialForeignAndBadLineBuildsNeverPublishReady(t *testing.T) {
	for mode, code := range map[string]string{"fail": "extraction_failed", "omit": "graph_incomplete", "foreign": "graph_reference", "location": "graph_reference", "change": "source_changed"} {
		t.Run(mode, func(t *testing.T) {
			h := newHarness(t)
			h.config.Mode = mode
			h.install(t)
			_, err := Build(t.Context(), h.options())
			wantCode(t, err, code)
			if len(globCache(t, h, "ready.json")) != 0 || len(globCache(t, h, ".build-*")) != 0 {
				t.Fatal("failed build left activated or pending output")
			}
		})
	}
}

func TestTimeoutKillsOwnedDelayedChildAndLeavesNoActiveGraph(t *testing.T) {
	h := newHarness(t)
	h.config.Mode = "timeout"
	h.install(t)
	options := h.options()
	options.Timeout = 150 * time.Millisecond
	_, err := Build(t.Context(), options)
	wantCode(t, err, "timeout")
	time.Sleep(900 * time.Millisecond)
	if _, err := os.Stat(h.config.Late); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("delayed child survived the timeout")
	}
	if len(globCache(t, h, "ready.json")) != 0 {
		t.Fatal("timed out build activated a graph")
	}
}

func TestCancellationKillsTheRuntimeGroup(t *testing.T) {
	h := newHarness(t)
	h.config.Mode = "timeout"
	h.install(t)
	ctx, cancel := context.WithCancel(t.Context())
	go func() {
		time.Sleep(150 * time.Millisecond)
		cancel()
	}()
	if _, err := Build(ctx, h.options()); !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v", err)
	}
	time.Sleep(900 * time.Millisecond)
	if _, err := os.Stat(h.config.Late); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("delayed child survived cancellation")
	}
}

func TestAmbiguousNoMatchAndOutputBudgetRemainExplicit(t *testing.T) {
	h := newHarness(t)
	if _, err := Build(t.Context(), h.options()); err != nil {
		t.Fatal(err)
	}
	for code, status := range map[int]string{3: "ambiguous", 4: "no_match"} {
		h.config.QueryExit = code
		h.install(t)
		result, err := Query(t.Context(), h.options())
		if err != nil || result.Status != status {
			t.Fatalf("exit %d = %+v %v", code, result, err)
		}
	}
	h.config.QueryExit = 7
	h.install(t)
	_, err := Query(t.Context(), h.options())
	wantCode(t, err, "query_failed")
	h.config.QueryExit, h.config.LongQuery = 0, true
	h.install(t)
	options := h.options()
	options.Budget = 100
	result, err := Query(t.Context(), options)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Truncated || result.ContextChars > 300 || result.TruncationNotice == nil || !strings.Contains(*result.TruncationNotice, "unknown") {
		t.Fatalf("result = %+v", result)
	}
}

func TestGraphReferencesMapLexicallyWithoutResolvingParents(t *testing.T) {
	state := sourceState{Repository: "/repo", Files: []sourceFile{{Path: "main.go"}, {Path: "pkg/a.go"}}}
	const snapshot = "/cache/.build-1/source"
	for value, want := range map[string]string{
		snapshot + "/main.go": "/repo/main.go", "/repo/main.go": "/repo/main.go", "main.go": "/repo/main.go", "./main.go": "/repo/main.go",
		"pkg//a.go": "/repo/pkg/a.go", "pkg/./a.go": "/repo/pkg/a.go", "pkg/a.go/": "/repo/pkg/a.go",
		snapshot + "//main.go": "/repo/main.go", snapshot + "/./main.go": "/repo/main.go",
	} {
		if got, err := mappedSource(value, snapshot, state); err != nil || got != want {
			t.Fatalf("%s = %s, %v", value, got, err)
		}
	}
	for value, detail := range map[string]string{
		"pkg/../main.go":                  "Graph references an unindexed source: pkg/../main.go",
		snapshot + "/pkg/../main.go":      "Graph references an unindexed source: pkg/../main.go",
		"/repo/pkg/../main.go":            "Graph references an unindexed source: pkg/../main.go",
		"/outside/secret.go":              "Graph contains an out-of-scope source reference",
		snapshot:                          "Graph references an unindexed source: .",
		"/cache/.build-1/sourceX/main.go": "Graph contains an out-of-scope source reference",
		"missing.go":                      "Graph references an unindexed source: missing.go",
		".":                               "Graph references an unindexed source: .",
		"/repo":                           "Graph references an unindexed source: .",
	} {
		_, err := mappedSource(value, snapshot, state)
		wantCode(t, err, "graph_reference")
		if err.Error() != detail {
			t.Fatalf("%s: %v", value, err)
		}
	}
}

func TestLineCountFollowsByteLineEndings(t *testing.T) {
	for text, want := range map[string]int{"": 1, "a": 1, "a\n": 1, "a\nb": 2, "a\nb\n": 2, "a\r\nb\r\n": 2, "a\rb": 2, "a\r": 1, "\n\n": 2} {
		if got := lineCount([]byte(text)); got != want {
			t.Fatalf("lineCount(%q) = %d, want %d", text, got, want)
		}
	}
}
