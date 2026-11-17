package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/belevtsev/kitsu/v3/internal/gitx"
	"github.com/belevtsev/kitsu/v3/internal/roles"
)

func TestWorkerHintsRecipesUseCurrentCLIAndDeclaredChecks(t *testing.T) {
	r := compactChecksFixture(t, "pass")
	t.Chdir(r.Dir)
	s := &session{repo: &gitx.Repo{Root: r.Dir}}
	hints, err := snapshotWorkerHints(s, workerBrief{Role: "quality-engineer", Host: "codex"})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(hints.Recipes["quiet"], []string{"kitsu", "check", "run", "pass"}) ||
		!slices.Equal(hints.Recipes["noisy"], []string{"kitsu", "check", "run", "--compact", "pass"}) {
		t.Fatal("recipes lost declared check identity", hints)
	}
	for name, argv := range hints.Recipes {
		t.Run(name, func(t *testing.T) {
			args := slices.Clone(argv[1:])
			// Help resolves the current command without executing the recipe.
			if literal := slices.Index(args, "--"); literal >= 0 {
				args = args[:literal]
			}
			var help bytes.Buffer
			if err := Execute(t.Context(), append(args, "--help"), Config{Out: &help, Err: io.Discard}); err != nil || !strings.Contains(help.String(), "usage:") {
				t.Fatal("recipe no longer resolves against command metadata", args, err, help.String())
			}
		})
	}
	// Exercise the two check modes, scoped fallback and original-log expansion
	// through the real CLI. They need no speculative help/discovery call.
	quiet := r.Kitsu(hints.Recipes["quiet"][1:]...)
	if quiet.Code != 0 {
		t.Fatal(quiet.Output())
	}
	page := compactResult(t, r.Kitsu(hints.Recipes["noisy"][1:]...), 0)
	if len(page.Items) != 1 {
		t.Fatal(page)
	}
	expand := replaceHintArgs(hints.Recipes["original_log"], map[string]string{
		"<artifact-id>": page.ID, "<item-id>": page.Items[0].ID,
	})
	full := compactResult(t, r.Kitsu(expand[1:]...), 0)
	if full.ID != page.ID || full.Items[0].ID != page.Items[0].ID {
		t.Fatal("original-log recipe changed identity", full)
	}
	if full.Items[0].Next == "" {
		t.Fatal("long original log unexpectedly lacks a continuation", full)
	}
	continuation := replaceHintArgs(hints.Recipes["continue_log"], map[string]string{
		"<artifact-id>": full.ID, "<item-id>": full.Items[0].ID, "<next_cursor>": full.Items[0].Next,
	})
	if next := compactResult(t, r.Kitsu(continuation[1:]...), 0); next.ID != full.ID {
		t.Fatal("continuation did not read the same original log", next)
	}
	r.Write("tests/search.txt", "needle\n")
	search := replaceHintArgs(hints.Recipes["search_fallback"], map[string]string{
		"<literal>": "needle", "<path>": "tests/search.txt",
	})
	found := compactResult(t, r.Kitsu(search[1:]...), 0)
	if !strings.Contains(stringMustJSON(found.Items), "needle") {
		t.Fatal("fallback lost a scoped match", found)
	}
	data, err := json.Marshal(hints)
	if err != nil || len(data) > 2200 {
		t.Fatal("unbounded QE hints", len(data), err)
	}
	t.Logf("QE hints: %d compact JSON bytes", len(data))
}

func replaceHintArgs(argv []string, values map[string]string) []string {
	result := slices.Clone(argv)
	for i, arg := range result {
		if value, ok := values[arg]; ok {
			result[i] = value
		}
	}
	return result
}

func TestWorkerHintsBoundLargeCheckDeclarationsWithoutLosingSuite(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	var declaration strings.Builder
	declaration.WriteString("version=1\n")
	for i := range 9 {
		fmt.Fprintf(&declaration, "[[check]]\nid='check-%d'\nrun=['unused']\n", i)
	}
	if err := os.WriteFile(filepath.Join(dir, "kitsu.toml"), []byte(declaration.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	hints, err := snapshotWorkerHints(&session{repo: &gitx.Repo{Root: dir}}, workerBrief{Role: "quality-engineer"})
	if err != nil || !slices.Equal(hints.Recipes["quiet"], []string{"kitsu", "check", "run"}) {
		t.Fatal("large declaration produced an incomplete suite", hints, err)
	}
	for _, role := range []string{"developer", "reviewer", "git", "lookup"} {
		got, err := snapshotWorkerHints(nil, workerBrief{Role: role})
		if err != nil || got != nil {
			t.Fatal("QE hints leaked into unrelated role", role, got, err)
		}
	}
	if _, err := workerHintArgv("removed command", nil, nil); err == nil {
		t.Fatal("removed command accepted")
	}
	for _, option := range [][2]string{{"removed", ""}, {"compact", "invalid"}, {"max-bytes", ""}} {
		if _, err := workerHintArgv("check run", nil, [][2]string{option}); err == nil {
			t.Fatal("invalid flag metadata accepted", option)
		}
	}
}

func TestWorkerHintsBoundSerializedIDOperands(t *testing.T) {
	t.Setenv("CODEX_THREAD_ID", "known-session")
	for _, tc := range []struct {
		name string
		ids  []string
	}{
		{name: "one oversized invalid ID", ids: []string{strings.Repeat("a", 60<<10)}},
		{name: "eight longest valid IDs", ids: func() []string {
			ids := []string{}
			for i := range 8 {
				ids = append(ids, fmt.Sprintf("%d%s", i, strings.Repeat("a", 39)))
			}
			return ids
		}()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			var declaration strings.Builder
			declaration.WriteString("version=1\n")
			for _, id := range tc.ids {
				fmt.Fprintf(&declaration, "[[check]]\nid='%s'\nrun=['unused']\n", id)
			}
			if err := os.WriteFile(filepath.Join(dir, "kitsu.toml"), []byte(declaration.String()), 0o600); err != nil {
				t.Fatal(err)
			}
			hints, err := snapshotWorkerHints(&session{repo: &gitx.Repo{Root: dir}}, workerBrief{Role: "quality-engineer", Host: "codex"})
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(hints.Recipes["quiet"], []string{"kitsu", "check", "run"}) ||
				!slices.Equal(hints.Recipes["noisy"], []string{"kitsu", "check", "run", "--compact"}) {
				t.Fatal("oversized operands published a partial suite", hints)
			}
			data, err := json.Marshal(hints)
			if err != nil || len(data) > 2200 || hints.RG.Scope.Session == "" {
				t.Fatal("oversized hints or lost known session", len(data), err)
			}
		})
	}
}

func TestWorkerHintsOptionalLookupIsScopedAndNeverExecutes(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PATH", dir)
	t.Setenv("CODEX_THREAD_ID", "worker-hints-test")
	t.Setenv("CODEX_SESSION_ID", "")
	t.Setenv("CLAUDE_SESSION_ID", "")
	if runtime.GOOS == "windows" {
		t.Setenv("PATHEXT", ".EXE")
	}
	scope := workerLookupScope("codex", dir)
	missing := observeWorkerTool(scope)
	if missing.State != "missing" || missing.Scope.Session == "" {
		t.Fatal("missing executable was not observed", missing)
	}
	name := "rg"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	// Deliberately not a functioning executable: lookup must not try to probe it.
	if err := os.WriteFile(filepath.Join(dir, name), []byte("not an executable image"), 0o700); err != nil {
		t.Fatal(err)
	}
	available := observeWorkerTool(scope)
	if available.State != "available" || available.Scope != missing.Scope {
		t.Fatal("lookup did not find the unexecuted file", available)
	}
	for _, change := range []struct{ name, value string }{
		{name: "PATH", value: filepath.Join(dir, "other")},
		{name: "CODEX_THREAD_ID", value: "another-session"},
		{name: "GODEBUG", value: "execerrdot=0"},
	} {
		t.Run(change.name, func(t *testing.T) {
			t.Setenv(change.name, change.value)
			if current := workerLookupScope("codex", dir); current == scope {
				t.Fatal("changed environment retained lookup scope", current)
			}
		})
	}
	if workerLookupScope("claude", dir) == scope || workerLookupScope("codex", dir+"-other") == scope {
		t.Fatal("host or working directory escaped scope")
	}
	t.Setenv("CODEX_THREAD_ID", "")
	if workerLookupScope("codex", dir).Session != "" {
		t.Fatal("unknown session was invented")
	}
	if workerLookupState(errors.New("lookup denied")) != "unknown" {
		t.Fatal("lookup error was treated as missing")
	}
	for _, policy := range []string{"no proof of execution or authority", "mismatch means unknown", "use search_fallback without retrying rg", "One permitted lookup", "no hash computation or discovery needed", "execution error", "never probe or install"} {
		if !strings.Contains(available.Policy, policy) {
			t.Errorf("lookup lost guard %q", policy)
		}
	}
}

func TestWorkerHintsLookupScopesActualDirectoryWithRelativePATH(t *testing.T) {
	r := compactChecksFixture(t, "pass")
	if err := os.MkdirAll(r.Path("tools"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(r.Path("subdir"), 0o700); err != nil {
		t.Fatal(err)
	}
	name := "rg"
	if runtime.GOOS == "windows" {
		name += ".exe"
		t.Setenv("PATHEXT", ".EXE")
	}
	if err := os.WriteFile(r.Path("tools/"+name), []byte("unexecuted lookup fixture"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", "tools")
	t.Setenv("GODEBUG", "execerrdot=0")
	t.Setenv("CODEX_THREAD_ID", "relative-path-session")
	t.Chdir(r.Dir)
	rootScope := workerLookupScope("codex", r.Dir)
	if lookup := observeWorkerTool(rootScope); lookup.State != "available" {
		t.Fatal("relative root tool was not observed", lookup)
	}
	t.Chdir(r.Path("subdir"))
	subdirScope := workerLookupScope("codex", r.Dir)
	if subdirScope == rootScope {
		t.Fatal("different actual directories shared a lookup scope", subdirScope)
	}
	if lookup := observeWorkerTool(subdirScope); lookup.State != "missing" {
		t.Fatal("subdirectory reused root's relative PATH lookup", lookup)
	}
	data, err := json.Marshal([]workerToolScope{rootScope, subdirScope})
	if err != nil || strings.Contains(string(data), r.Dir) {
		t.Fatal("lookup scope leaked a raw local path", string(data), err)
	}
	t.Chdir(r.Dir)
	t.Setenv("GODEBUG", "execerrdot=1")
	if lookup := observeWorkerTool(workerLookupScope("codex", r.Dir)); lookup.State != "unknown" {
		t.Fatal("relative executable lookup error became availability", lookup)
	}
}

func TestWorkerHintsUnknownDirectoryDoesNotPublishAvailability(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows cannot remove the current directory")
	}
	for _, name := range []string{"removed", "replaced"} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			directory := filepath.Join(root, "removed-cwd")
			if err := os.Mkdir(directory, 0o700); err != nil {
				t.Fatal(err)
			}
			t.Chdir(directory)
			if err := os.Remove(directory); err != nil {
				t.Fatal(err)
			}
			if name == "replaced" {
				// An existing path must still identify the actual current directory.
				if err := os.Mkdir(directory, 0o700); err != nil {
					t.Fatal(err)
				}
			}
			scope := workerLookupScope("codex", root)
			if scope.Runtime != "" || observeWorkerTool(scope).State != "unknown" {
				t.Fatal("missing actual cwd invented a reusable lookup", scope)
			}
		})
	}
}

func TestWorkerHintsSnapshotPreservesOriginalPacketAndFreshness(t *testing.T) {
	r := compactChecksFixture(t, "pass")
	t.Chdir(r.Dir)
	for _, kv := range r.Env() {
		if name, value, ok := strings.Cut(kv, "="); ok && name != "" {
			t.Setenv(name, value)
		}
	}
	t.Setenv("CODEX_THREAD_ID", "worker-hints-snapshot")
	r.Write("kitsu-roles.toml", "version=1\n[paths]\ntests=['tests/**']\n[codex.quality-engineer]\nskills=['local']\n")
	writeSkillFixture(t, r.Path(".agents/skills/local/SKILL.md"), "local")
	cfg := Config{Out: io.Discard, Err: io.Discard}
	b := workerBrief{Task: "fix-login", Role: "quality-engineer", Stage: "tests", Round: 1, Host: "codex", Workflow: "small"}
	assignment := "  Preserve λ and exact assignment bytes.\r\n"
	p, err := prepareWorkerBrief(t.Context(), cfg, b, assignment)
	if err != nil {
		t.Fatal(err)
	}
	if p.Brief.Hints == nil || len(p.Brief.Skills) != 1 {
		t.Fatal("hints or selected original skill missing", p)
	}
	before, err := canonicalWorkerBrief(p.Brief, assignment)
	if err != nil {
		t.Fatal(err)
	}
	body, err := roles.Text(b.Role)
	if err != nil || strings.Count(string(before), body) != 1 || !bytes.HasSuffix(before, []byte(assignment)) {
		t.Fatal("original role or assignment changed", err)
	}
	if got, err := prepareWorkerBrief(t.Context(), cfg, b, assignment); err != nil || got.Dispatch != p.Dispatch {
		t.Fatal("unchanged preparation lost deterministic identity", got.Dispatch, p.Dispatch, err)
	}
	encoded, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	var retained preparedWorkerBrief
	if err := json.Unmarshal(encoded, &retained); err != nil {
		t.Fatal(err)
	}
	// A new session makes the saved tool observation inapplicable; it must not
	// rewrite or invalidate the historical packet or selected-skill policy.
	t.Setenv("CODEX_THREAD_ID", "changed-session")
	if err := verifySavedWorkerBrief(t.Context(), cfg, retained); err != nil {
		t.Fatal("environment-only change rewrote immutable packet", err)
	}
	after, err := canonicalWorkerBrief(retained.Brief, assignment)
	if err != nil || !bytes.Equal(before, after) || workerBriefHash(after) != p.Dispatch.SHA256 {
		t.Fatal("canonical verification refreshed tool facts", err)
	}
	s, _, err := compactEngine(t.Context(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	forged := retained
	forged.Brief.Hints = &workerHints{Guidance: "forged authority"}
	if err := verifyWorkerBriefPolicy(s, forged); err == nil {
		t.Fatal("tampered tool hints accepted")
	}
	r.Write(".agents/skills/local/SKILL.md", "---\nname: local\ndescription: changed\n---\nChanged original selected skill.\n")
	if err := verifyWorkerBriefPolicy(s, retained); err == nil || !strings.Contains(err.Error(), "stale") {
		t.Fatal("tool snapshot bypassed skill freshness", err)
	}
}
