package graphify

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeConfig scripts the stand-in runtime. The test binary re-executes itself as
// that runtime, because the guarded child environment passes nothing through.
type fakeConfig struct {
	Version            string `json:"version"`
	Mode               string `json:"mode"`
	QueryExit          int    `json:"query_exit"`
	LongQuery          bool   `json:"long_query"`
	Log                string `json:"log"`
	Late               string `json:"late"`
	Repo               string `json:"repo"`
	ProductionGate     string `json:"production_gate,omitempty"`
	ProductionDeadline string `json:"production_deadline,omitempty"`
	// Running is created by an owned descendant once it is running.
	Running string `json:"running,omitempty"`
	// Delay is the delayed descendant's wait in seconds before it writes Late.
	Delay string `json:"delay,omitempty"`
	// Returned is created by the test after Build has returned; Markers is
	// where a fork-storm descendant that outlived Build reports itself.
	Returned string `json:"returned,omitempty"`
	Markers  string `json:"markers,omitempty"`
}

// fakeForwardedGORACE carries, from the runtime script to logCall, whether
// and how the product forwarded GORACE before the script set its own value.
const fakeForwardedGORACE = "CW_FAKE_FORWARDED_GORACE"

const (
	// delayedDescendantScript runs as an owned descendant: it creates $1 once
	// running, waits $3 seconds and then writes $2. Only a kill stops the write.
	delayedDescendantScript = `: > "$1"; sleep "$3"; echo late > "$2"`
	// fakeHold bounds a kill-test extraction that is never killed; it then
	// fails on its own without publishing a graph.
	fakeHold = 8 * time.Second

	// stormDescendantScript is one short-lived fork-storm descendant. If its
	// forking leader ($1) is still alive after a short wait it exits at once.
	// Otherwise it waits up to three seconds for the test to report that Build
	// returned ($2) and then creates its unique marker ($3). Only a process
	// that outlived the return of Build can create a marker.
	stormDescendantScript = `sleep 0.2
kill -0 "$1" 2>/dev/null && exit 0
i=0
while [ "$i" -lt 30 ]; do
	if [ -e "$2" ]; then : > "$3"; exit 0; fi
	sleep 0.1
	i=$((i + 1))
done`
	// The storm is bounded: at most stormForks descendants, at most
	// stormConcurrency alive at once, forking for at most stormDuration.
	stormForks       = 1000
	stormConcurrency = 64
	stormDuration    = 3 * time.Second
	// stormRunningAfter descendants started confirm the storm is running.
	stormRunningAfter = 8
)

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
	// Log the environment the product forwarded, not the race option the
	// runtime script added for the fake's own exit.
	if forwarded, recorded := env[fakeForwardedGORACE]; recorded {
		delete(env, fakeForwardedGORACE)
		delete(env, "GORACE")
		if value, set := strings.CutPrefix(forwarded, "set:"); set {
			env["GORACE"] = value
		}
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
	case "delayed-descendant":
		return fakeDelayedDescendant(config)
	case "fork-storm":
		return fakeForkStorm(config)
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
	if config.ProductionGate != "" && !productionExtractionGate(config) {
		return 26
	}
	if config.Mode == "change" {
		_ = os.WriteFile(filepath.Join(config.Repo, "main.go"), []byte("package main\nfunc changed(){}\n"), 0o600)
	}
	return 0
}

// fakeDelayedDescendant starts one owned descendant that reports itself
// running and writes config.Late after config.Delay seconds, then holds the
// extraction open so only a kill of the whole group can stop that write.
func fakeDelayedDescendant(config fakeConfig) int {
	child := exec.Command("/bin/sh", "-c", delayedDescendantScript, "sh", config.Running, config.Late, config.Delay)
	if err := child.Start(); err != nil {
		return 27
	}
	time.Sleep(fakeHold)
	return 27
}

// fakeForkStorm keeps forking short-lived descendants within fixed bounds, so
// a cancellation is likely to arrive while a fork is in progress. It reports
// itself running after stormRunningAfter descendants have started. Left
// alone it stops forking after stormDuration, reaps every descendant and
// fails without publishing a graph.
func fakeForkStorm(config fakeConfig) int {
	leader := strconv.Itoa(os.Getpid())
	slots := make(chan struct{}, stormConcurrency)
	var running sync.WaitGroup
	defer running.Wait()
	stop := time.Now().Add(stormDuration)
	for index := 0; index < stormForks && time.Now().Before(stop); index++ {
		slots <- struct{}{}
		marker := filepath.Join(config.Markers, fmt.Sprintf("survivor-%s-%d", leader, index))
		child := exec.Command("/bin/sh", "-c", stormDescendantScript, "sh", leader, config.Returned, marker)
		if err := child.Start(); err != nil {
			<-slots
			break
		}
		running.Add(1)
		go func() {
			defer running.Done()
			_ = child.Wait()
			<-slots
		}()
		if index+1 == stormRunningAfter {
			if err := os.WriteFile(config.Running, []byte("forking\n"), 0o600); err != nil {
				return 29
			}
		}
	}
	return 28
}

func productionExtractionGate(config fakeConfig) bool {
	deadline, err := time.Parse(time.RFC3339Nano, config.ProductionDeadline)
	if err != nil || !deadline.After(time.Now()) {
		return false
	}
	connection, err := net.DialTimeout("tcp", config.ProductionGate, 5*time.Second)
	if err != nil {
		return false
	}
	defer connection.Close()
	if err := connection.SetDeadline(deadline); err != nil {
		return false
	}
	if _, err := io.WriteString(connection, "extract\n"); err != nil {
		return false
	}
	var release [1]byte
	_, err = io.ReadFull(connection, release[:])
	return err == nil && release[0] == 'x'
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
	// Keep fixture repositories quiescent for whole-tree byte/mode snapshots.
	h.git(t, "config", "--local", "maintenance.auto", "false")
	h.git(t, "config", "--local", "gc.auto", "0")
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
	// The product does not forward GORACE, so the script sets the race option
	// that stops the fake's successful exits from sleeping about a second. It
	// first records whether the product forwarded GORACE, and how, so logCall
	// can log exactly the environment the product passed.
	script := fmt.Sprintf("#!/bin/sh\n"+
		"if [ \"${GORACE+set}\" = set ]; then %[1]s=\"set:$GORACE\"; else %[1]s=unset; fi\n"+
		"export %[1]s\n"+
		"GORACE=atexit_sleep_ms=0\n"+
		"export GORACE\n"+
		"exec '%[2]s' fake-graphify '%[3]s' \"$@\"\n", fakeForwardedGORACE, binary, configPath)
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

// unsetParentEnv removes name from the test process environment and restores it
// when the test ends.
func unsetParentEnv(t *testing.T, name string) {
	t.Helper()
	t.Setenv(name, "")
	if err := os.Unsetenv(name); err != nil {
		t.Fatal(err)
	}
}

// assertFixedGraphifyModes checks that every runtime child (version probe,
// extract and query) received the fixed Graphify modes, whatever the parent set.
func assertFixedGraphifyModes(t *testing.T, calls []fakeCall) {
	t.Helper()
	seen := map[string]bool{}
	for _, call := range calls {
		kind := call.Args[0]
		seen[kind] = true
		for name, want := range map[string]string{"GRAPHIFY_BACKEND": "none", "GRAPHIFY_QUERY_LOG_DISABLE": "1"} {
			value, present := call.Env[name]
			if !present || value != want {
				t.Fatalf("%s child: %s = %q (present %v), want %q", kind, name, value, present, want)
			}
		}
	}
	for _, kind := range []string{"--version", "extract", "query"} {
		if !seen[kind] {
			t.Fatalf("no %s runtime child was observed: %v", kind, calls)
		}
	}
}

func TestVersionPinIsCurrentRelease(t *testing.T) {
	// R1a
	if Version != "0.9.103" {
		t.Fatalf("Version = %q, want 0.9.103", Version)
	}
}

// TestGuardedModesAndQueryDepthAcrossParentAndRcStates shares one harness
// across two phases so every child kind is observed under each parent state.
func TestGuardedModesAndQueryDepthAcrossParentAndRcStates(t *testing.T) {
	h := newHarness(t)
	budgets := []int{2000, 500}

	// Phase 1 (R2a, R4a, R4b untracked): the parent does not set the modes and
	// the repository holds an untracked .graphifyrc asking for depth 6.
	unsetParentEnv(t, "GRAPHIFY_BACKEND")
	unsetParentEnv(t, "GRAPHIFY_QUERY_LOG_DISABLE")
	h.write(t, ".graphifyrc", "query_depth=6\n")
	if _, err := Build(t.Context(), h.options()); err != nil {
		t.Fatal(err)
	}
	options := h.options()
	options.Budget = budgets[0]
	if _, err := Query(t.Context(), options); err != nil {
		t.Fatal(err)
	}
	unset := h.calls(t)
	assertFixedGraphifyModes(t, unset)

	// Phase 2 (R2b, R4a, R4b tracked): the parent sets a case variant of the
	// backend and an empty log switch; .graphifyrc is now tracked. A tracked Go
	// change is committed with it so this Build must extract again.
	t.Setenv("GRAPHIFY_BACKEND", "None")
	t.Setenv("GRAPHIFY_QUERY_LOG_DISABLE", "")
	h.write(t, "other.go", "package main\nfunc other() { _ = 1 }\n")
	h.git(t, "add", ".graphifyrc", "other.go")
	h.git(t, "commit", "-qm", "graphify defaults")
	if _, err := Build(t.Context(), h.options()); err != nil {
		t.Fatal(err)
	}
	options.Budget = budgets[1]
	if _, err := Query(t.Context(), options); err != nil {
		t.Fatal(err)
	}
	all := h.calls(t)
	assertFixedGraphifyModes(t, all[len(unset):])

	// R4a/R4b across both phases; R4c: only query children are checked.
	assertQueryDepthPinned(t, all, budgets)
}

func TestMinimalEnvironmentBlocksCredentialsAndInheritedModes(t *testing.T) {
	h := newHarness(t)
	blocked := []string{"OPENAI_API_KEY", "TYPESAFE_API_KEY", "SUBQ_API_KEY", "ANTHROPIC_API_KEY", "GEMINI_API_KEY", "PYTHONPATH", "GRAPHIFY_QUERY_LOG", "GRAPHIFY_QUERY_LOG_ENABLE", "GRAPHIFY_OUT"}
	for _, name := range blocked {
		t.Setenv(name, "do-not-forward")
	}
	// R2b: inherited mode values must be replaced, not forwarded.
	t.Setenv("GRAPHIFY_BACKEND", "claude")
	t.Setenv("GRAPHIFY_QUERY_LOG_DISABLE", "0")
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
		for name, value := range map[string]string{"GRAPHIFY_NO_AUTO_REFRESH": "1", "GRAPHIFY_QUERY_LOG_DISABLE": "1", "GRAPHIFY_BACKEND": "none", "PYTHONDONTWRITEBYTECODE": "1", "PYTHONHASHSEED": "0"} {
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
	// R1b
	if !strings.Contains(err.Error(), "Install graphifyy==0.9.103") {
		t.Fatalf("message = %v", err)
	}
	// R1c: the old pin and an unrelated version are both refused before
	// extraction. One harness is reused; each case must add version probes only.
	for _, reported := range []string{"0.9.91", "0.0.1"} {
		before := len(h.calls(t))
		h.config.Version = reported
		h.install(t)
		_, err := Build(t.Context(), h.options())
		wantCode(t, err, "runtime_version")
		if !strings.Contains(err.Error(), "requires graphifyy") || !strings.Contains(err.Error(), "graphifyy==0.9.103") || h.extractions(t) != 0 {
			t.Fatalf("%s: message = %v, %d extractions", reported, err, h.extractions(t))
		}
		calls := h.calls(t)
		if len(calls) == before {
			t.Fatalf("%s: version probe did not run", reported)
		}
		for _, call := range calls[before:] {
			if !slices.Equal(call.Args, []string{"--version"}) {
				t.Fatalf("%s: runtime started beyond the version probe: %v", reported, call.Args)
			}
		}
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

type buildAttempt struct {
	err error
}

// startBuild runs Build in the background under a cancellable child of the
// test context. Cleanup cancels it and joins the build.
func startBuild(t *testing.T, options Options) (<-chan buildAttempt, context.CancelFunc) {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan buildAttempt, 1)
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		_, err := Build(ctx, options)
		done <- buildAttempt{err: err}
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-finished:
		case <-time.After(15 * time.Second):
			t.Error("background build did not return after the test context ended")
		}
	})
	return done, cancel
}

func awaitBuild(t *testing.T, done <-chan buildAttempt, limit time.Duration) buildAttempt {
	t.Helper()
	select {
	case attempt := <-done:
		return attempt
	case <-time.After(limit):
		t.Fatalf("build did not return within %v", limit)
		return buildAttempt{}
	}
}

// awaitRunning polls for the readiness file a fake extraction or its
// descendant creates, and returns when it was first observed. It fails the
// test if the build returns first or the bounded deadline passes, so a test
// cannot pass without the owned processes having started.
func awaitRunning(t *testing.T, path string, done <-chan buildAttempt, limit time.Duration) time.Time {
	t.Helper()
	deadline := time.After(limit)
	tick := time.NewTicker(5 * time.Millisecond)
	defer tick.Stop()
	for {
		if _, err := os.Stat(path); err == nil {
			return time.Now()
		} else if !errors.Is(err, os.ErrNotExist) {
			t.Fatal(err)
		}
		select {
		case attempt := <-done:
			t.Fatalf("build returned before %s appeared: %v", filepath.Base(path), attempt.err)
		case <-deadline:
			t.Fatalf("%s did not appear within %v", filepath.Base(path), limit)
		case <-tick.C:
		}
	}
}

func secondsArg(duration time.Duration) string {
	return strconv.FormatFloat(duration.Seconds(), 'f', -1, 64)
}

// requireDelayedDescendantScript runs the delayed descendant unkilled with no
// delay, proving it can report both running and its late write.
func requireDelayedDescendantScript(t *testing.T, root string) {
	t.Helper()
	running, late := filepath.Join(root, "control-running"), filepath.Join(root, "control-late")
	if err := exec.Command("/bin/sh", "-c", delayedDescendantScript, "sh", running, late, "0").Run(); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{running, late} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("unkilled delayed descendant did not write %s: %v", filepath.Base(path), err)
		}
	}
}

// requireNoLateWrite waits until the observation window ends and then checks
// that the delayed descendant never wrote its late marker.
func requireNoLateWrite(t *testing.T, late string, until time.Time, message string) {
	t.Helper()
	time.Sleep(time.Until(until))
	if _, err := os.Stat(late); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("%s: %v", message, err)
	}
}

func TestTimeoutKillsOwnedDelayedChildAndLeavesNoActiveGraph(t *testing.T) {
	// The descendant's delay exceeds the timeout, so only a kill stops it.
	const timeout, delay, margin = 2 * time.Second, 3 * time.Second, time.Second
	h := newHarness(t)
	requireDelayedDescendantScript(t, h.root)
	h.config.Mode, h.config.Running, h.config.Delay = "delayed-descendant", filepath.Join(h.root, "descendant-running"), secondsArg(delay)
	h.install(t)
	options := h.options()
	options.Timeout = timeout
	started := time.Now()
	done, _ := startBuild(t, options)
	running := awaitRunning(t, h.config.Running, done, 15*time.Second)
	// The timeout clock starts no earlier than Build, so this proves the
	// descendant was running before the timeout fired.
	if elapsed := running.Sub(started); elapsed >= timeout {
		t.Fatalf("descendant first observed %v after Build started, not before the %v timeout", elapsed, timeout)
	}
	attempt := awaitBuild(t, done, 10*time.Second)
	wantCode(t, attempt.err, "timeout")
	requireNoLateWrite(t, h.config.Late, running.Add(delay+margin), "delayed child survived the timeout")
	if len(globCache(t, h, "ready.json")) != 0 {
		t.Fatal("timed out build activated a graph")
	}
}

func TestCancellationKillsTheRuntimeGroup(t *testing.T) {
	const delay, margin = 2 * time.Second, time.Second
	h := newHarness(t)
	requireDelayedDescendantScript(t, h.root)
	h.config.Mode, h.config.Running, h.config.Delay = "delayed-descendant", filepath.Join(h.root, "descendant-running"), secondsArg(delay)
	h.install(t)
	done, cancel := startBuild(t, h.options())
	running := awaitRunning(t, h.config.Running, done, 15*time.Second)
	cancel()
	if attempt := awaitBuild(t, done, 10*time.Second); !errors.Is(attempt.err, context.Canceled) {
		t.Fatalf("error = %v", attempt.err)
	}
	requireNoLateWrite(t, h.config.Late, running.Add(delay+margin), "delayed child survived cancellation")
}

// requireStormDescendantScript proves the storm descendant reports a
// survivor whose leader is gone once Build has returned, and stays silent
// while its leader is alive.
func requireStormDescendantScript(t *testing.T, root string) {
	t.Helper()
	gone := exec.Command("/bin/sh", "-c", "exit 0")
	if err := gone.Run(); err != nil {
		t.Fatal(err)
	}
	returned := filepath.Join(root, "control-returned")
	if err := os.WriteFile(returned, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	survivor, owned := filepath.Join(root, "control-survivor"), filepath.Join(root, "control-owned")
	orphan := exec.Command("/bin/sh", "-c", stormDescendantScript, "sh", strconv.Itoa(gone.ProcessState.Pid()), returned, survivor)
	live := exec.Command("/bin/sh", "-c", stormDescendantScript, "sh", strconv.Itoa(os.Getpid()), returned, owned)
	for _, command := range []*exec.Cmd{orphan, live} {
		if err := command.Start(); err != nil {
			t.Fatal(err)
		}
	}
	for _, command := range []*exec.Cmd{orphan, live} {
		if err := command.Wait(); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Stat(survivor); err != nil {
		t.Fatalf("storm descendant cannot report a survivor: %v", err)
	}
	if _, err := os.Stat(owned); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("storm descendant reported a survivor while its leader was alive: %v", err)
	}
}

// TestCancellationDuringForkStormLeavesNoSurvivor cancels while the fake
// extraction is forking continuously. On macOS one group kill can miss a child
// being forked at that moment. Measured against a runner that kills only once,
// about 11-14% of trials leave a survivor; at 12% such a runner survives all
// trials with probability 0.88^26 < 4% (0.89^26 < 5% at 11%).
func TestCancellationDuringForkStormLeavesNoSurvivor(t *testing.T) {
	const trials, observation = 26, 1500 * time.Millisecond
	h := newHarness(t)
	requireStormDescendantScript(t, h.root)
	markers := filepath.Join(h.root, "storm-survivors")
	if err := os.Mkdir(markers, 0o700); err != nil {
		t.Fatal(err)
	}
	h.config.Mode, h.config.Markers = "fork-storm", markers
	for trial := range trials {
		h.config.Running = filepath.Join(h.root, fmt.Sprintf("storm-running-%d", trial))
		h.config.Returned = filepath.Join(h.root, fmt.Sprintf("storm-returned-%d", trial))
		h.install(t)
		done, cancel := startBuild(t, h.options())
		awaitRunning(t, h.config.Running, done, 15*time.Second)
		// Vary the phase of the fork loop the cancellation lands in.
		time.Sleep(time.Duration(trial%4) * 2 * time.Millisecond)
		cancel()
		attempt := awaitBuild(t, done, 10*time.Second)
		// From here on, any descendant of this trial still running outlived Build.
		if err := os.WriteFile(h.config.Returned, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		if !errors.Is(attempt.err, context.Canceled) {
			t.Fatalf("trial %d: error = %v", trial, attempt.err)
		}
	}
	// A survivor reports within about 0.3 s of its trial's return marker.
	time.Sleep(observation)
	survivors, err := os.ReadDir(markers)
	if err != nil {
		t.Fatal(err)
	}
	if len(survivors) != 0 {
		names := make([]string, 0, len(survivors))
		for _, survivor := range survivors {
			names = append(names, survivor.Name())
		}
		t.Fatalf("%d storm descendants outlived the cancelled Build: %v", len(names), names)
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

// argAfter returns the argument that immediately follows flag, if any.
func argAfter(args []string, flag string) (string, bool) {
	index := slices.Index(args, flag)
	if index < 0 || index+1 >= len(args) {
		return "", false
	}
	return args[index+1], true
}

// assertQueryDepthPinned checks that every query child received --depth 2
// alongside --budget and --graph. Other children are not checked (R4c).
func assertQueryDepthPinned(t *testing.T, calls []fakeCall, budgets []int) {
	t.Helper()
	var queries [][]string
	for _, call := range calls {
		if call.Args[0] == "query" {
			queries = append(queries, call.Args)
		}
	}
	if len(queries) != len(budgets) {
		t.Fatalf("%d query children, want %d: %v", len(queries), len(budgets), calls)
	}
	for index, args := range queries {
		// R4a: --depth immediately followed by 2, and never another depth value.
		depths := 0
		for position, arg := range args {
			if strings.HasPrefix(arg, "--depth=") {
				t.Fatalf("query child used inline depth %q: %v", arg, args)
			}
			if arg != "--depth" {
				continue
			}
			depths++
			if position+1 >= len(args) || args[position+1] != "2" {
				t.Fatalf("query child --depth is not followed by 2: %v", args)
			}
		}
		if depths == 0 {
			t.Fatalf("query child lacks --depth 2: %v", args)
		}
		// R4a: the existing budget and graph arguments are still passed.
		if budget, ok := argAfter(args, "--budget"); !ok || budget != strconv.Itoa(budgets[index]) {
			t.Fatalf("query child --budget = %q (%v), want %d: %v", budget, ok, budgets[index], args)
		}
		if graph, ok := argAfter(args, "--graph"); !ok || graph == "" || strings.HasPrefix(graph, "--") {
			t.Fatalf("query child --graph = %q (%v): %v", graph, ok, args)
		}
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
