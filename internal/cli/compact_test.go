package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/belevtsev/kitsu/v3/internal/contextpack"
	"github.com/belevtsev/kitsu/v3/internal/evidence"
	"github.com/belevtsev/kitsu/v3/internal/gitx"
	"github.com/belevtsev/kitsu/v3/internal/kitsutest"
	"github.com/belevtsev/kitsu/v3/internal/roles"
	"github.com/belevtsev/kitsu/v3/internal/store"
)

func compactCheckChild() {
	if os.Getenv("KITSU_TEST_COMPACT_CHECK") != "1" {
		return
	}
	mode := os.Args[1]
	if strings.HasPrefix(mode, "bounded-") {
		compactFitCheckChild(mode)
	}
	if mode == "wait" {
		_ = os.WriteFile(os.Getenv("KITSU_TEST_COMPACT_READY"), []byte("ready"), 0o600)
		time.Sleep(time.Hour)
	}
	if mode == "change" {
		_ = os.WriteFile("source.txt", []byte("changed\n"), 0o600)
	}
	if mode == "first" {
		_ = os.WriteFile(os.Getenv("KITSU_TEST_COMPACT_READY"), []byte("ready"), 0o600)
	}
	if mode == "second" {
		if _, err := os.Stat(os.Getenv("KITSU_TEST_COMPACT_READY")); err != nil {
			os.Exit(9)
		}
	}
	if strings.HasPrefix(mode, "flood") {
		fmt.Print(strings.Repeat("x\n", 6<<20))
		os.Exit(0)
	}
	stream := os.Stdout
	if mode == "fail" {
		// Separate stdout/stderr pipes do not preserve cross-stream ordering.
		// Keep this tail-preview fixture on the diagnostic's stream.
		stream = os.Stderr
	}
	fmt.Fprint(stream, strings.Repeat("routine output that need not be sent to a model\n", 2000))
	if mode == "fail" {
		fmt.Fprintln(os.Stderr, "IMPORTANT: intentional regression diagnostic")
		os.Exit(7)
	}
	os.Exit(0)
}

func compactResult(t *testing.T, result kitsutest.Result, code int) contextpack.Page {
	t.Helper()
	if result.Code != code {
		t.Fatalf("exit %d, want %d: %s", result.Code, code, result.Output())
	}
	var page contextpack.Page
	if err := json.Unmarshal([]byte(result.Stdout), &page); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, result.Output())
	}
	if result.Stderr != "" {
		t.Fatalf("diagnostics escaped compact JSON: %s", result.Stderr)
	}
	if len(result.Stdout) > contextpack.DefaultBytes {
		t.Fatalf("compact output exceeds default: %d", len(result.Stdout))
	}
	return page
}

func compactChecksFixture(t *testing.T, modes ...string) *kitsutest.Repo {
	t.Helper()
	r, _ := skillRepo(t)
	program, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	program = strings.ReplaceAll(program, `\`, `/`)
	r.Setenv("KITSU_TEST_COMPACT_CHECK", "1")
	r.Setenv("GORACE", "atexit_sleep_ms=0")
	r.Setenv("KITSU_TEST_COMPACT_READY", r.Path(".git/sequence-ready"))
	var declaration strings.Builder
	declaration.WriteString("version=1\n")
	for _, mode := range modes {
		timeout := "1s"
		if strings.HasPrefix(mode, "flood") {
			timeout = "20s"
		}
		fmt.Fprintf(&declaration, "[[check]]\nid=%q\nrun=[%q,%q]\ntimeout=%q\n", mode, program, mode, timeout)
	}
	r.Write("kitsu.toml", declaration.String())
	r.Write("source.txt", "original\n")
	return r
}

func TestCompactChecksPreserveOutcomesLogsAndEvidence(t *testing.T) {
	for _, mode := range []string{"pass", "fail", "change", "wait"} {
		t.Run(mode, func(t *testing.T) {
			r := compactChecksFixture(t, mode)
			code := 0
			if mode != "pass" {
				code = 5
			}
			plain := r.Kitsu("check", "run", "--force")
			if plain.Code != code {
				t.Fatalf("plain: %s", plain.Output())
			}
			r.Write("source.txt", "original\n")
			page := compactResult(t, r.Kitsu("check", "run", "--json", "--force"), code)
			var meta compactCheckMeta
			if err := json.Unmarshal(page.Metadata, &meta); err != nil {
				t.Fatal(err)
			}
			outcome := map[string]string{"pass": "passed", "fail": "failed", "change": "source_changed", "wait": "timed_out"}[mode]
			if len(meta.Checks) != 1 || meta.Checks[0].Record == nil || meta.Checks[0].Record.Outcome != outcome {
				t.Fatal(meta)
			}
			record := meta.Checks[0].Record
			if mode == "fail" && (record.ExitStatus == nil || *record.ExitStatus != 7 || !strings.Contains(stringMustJSON(page.Items), "IMPORTANT")) {
				t.Fatal("exit or tail diagnostic lost", meta, page.Items)
			}
			if record.EndedAt == nil || record.Tree == "" || record.ContentKey == "" {
				t.Fatal("incomplete normal evidence", record)
			}
			if mode == "pass" {
				again := compactResult(t, r.Kitsu("check", "run", "--compact"), 0)
				var next compactCheckMeta
				if err := json.Unmarshal(again.Metadata, &next); err != nil {
					t.Fatal(err)
				}
				if !next.Verified || !next.Checks[0].Skipped || next.Checks[0].Record.RunID != record.RunID {
					t.Fatal("passing evidence not reused", next)
				}
				if result := r.Kitsu("gate", "status"); result.Code != 0 {
					t.Fatal(result.Output())
				}
			}
			if mode != "wait" {
				expanded := r.Kitsu("context", "show", page.ID, "--item", mode, "--max-bytes", "1048576", "--json")
				if expanded.Code != 0 {
					t.Fatal(expanded.Output())
				}
				var recovered contextpack.Page
				if err := json.Unmarshal([]byte(expanded.Stdout), &recovered); err != nil {
					t.Fatal(err)
				}
				if len(recovered.Items[0].Lines) < 2 || !strings.Contains(stringMustJSON(recovered.Items), "routine output") {
					t.Fatal("full log was not recoverable")
				}
				path := r.Path(".git/kitsu/" + record.Log)
				if _, err := os.Stat(path); err != nil {
					t.Fatalf("runner log missing: %v", err)
				}
			}
		})
	}
}

func TestCompactChecksKeepDeclarationOrderAndRejectStaleRecords(t *testing.T) {
	t.Parallel()
	r := compactChecksFixture(t, "first", "second")
	page := compactResult(t, r.Kitsu("check", "run", "second", "first", "--compact"), 0)
	var meta compactCheckMeta
	if err := json.Unmarshal(page.Metadata, &meta); err != nil {
		t.Fatal(err)
	}
	if meta.Checks[0].ID != "first" || meta.Checks[1].ID != "second" || !meta.Verified {
		t.Fatal("checks ran out of declaration order", meta)
	}
	if result := r.Kitsu("check", "run", "--force"); result.Code != 0 {
		t.Fatal(result.Output())
	}
	if result := r.Kitsu("context", "show", page.ID); result.Code != 1 || !strings.Contains(result.Stderr, "records changed") {
		t.Fatalf("stale check context accepted: %s", result.Output())
	}
}

func brief(t *testing.T, r *kitsutest.Repo, host, role, stage string) (contextpack.Page, workerBrief) {
	t.Helper()
	page := compactResult(t, r.Kitsu("task", "brief", "fix-login", "--role", role, "--stage", stage, "--round", "1", "--host", host, "--workflow", "small", "--json"), 0)
	var b workerBrief
	if err := json.Unmarshal(page.Metadata, &b); err != nil {
		t.Fatal(err)
	}
	return page, b
}

func TestCompactBriefRetainsAuthorityAndReceiptBeforeBaseline(t *testing.T) {
	t.Parallel()
	r, _ := skillRepo(t)
	r.Write("kitsu-roles.toml", "version=1\n[paths]\ntests=['tests/**']\n[codex.developer]\nmodel='chosen'\neffort='max'\nskills=['local']\n")
	writeSkillFixture(t, r.Path(".agents/skills/local/SKILL.md"), "local")
	page, b := brief(t, r, "codex", "developer", "code")
	if b.Label != "kitsu-task: fix-login code 1" || b.Model != (roles.Model{Model: "chosen", Effort: "max"}) || b.LoadStatus != "unverified" || len(b.Skills) != 1 || b.Skills[0].SHA256 == "" || !filepath.IsAbs(b.Skills[0].Path) || b.Instructions == "" || b.Baseline == "" || b.Selection == "" || !b.Compact {
		t.Fatal(b)
	}
	if !r.Exists("openspec/changes/.skills/fix-login/" + b.Selection + ".json") {
		t.Fatal("selection receipt missing")
	}
	if b.State.Stage != "unknown" || b.State.Workflow != "small" || len(b.Constraints) < 5 || len(b.Advisers) > 0 {
		t.Fatal("scope or uncertainty lost", b)
	}
	if result := r.Kitsu("role", "check", "developer", "--from", b.Baseline); result.Code != 0 {
		t.Fatalf("receipt was after baseline: %s", result.Output())
	}
	if result := r.Kitsu("context", "show", page.ID); result.Code != 0 {
		t.Fatal(result.Output())
	}
	_, native := brief(t, r, "claude", "reviewer", "review")
	if native.Instructions != "" || native.NativeRole != "kitsu-team:reviewer" {
		t.Fatal("native role text repeated", native)
	}
	if !strings.Contains(strings.Join(native.Constraints, " "), "independently") {
		t.Fatal("review independence absent")
	}
	for _, ref := range native.References {
		if strings.Contains(ref, "advice") || strings.Contains(ref, "reviews") {
			t.Fatal("reviewer received author/adviser record", ref)
		}
	}
	// External selected source changes do not affect Git; they still invalidate the brief.
	path := b.Skills[0].Path
	if err := os.WriteFile(path, []byte("---\nname: local\ndescription: changed\n---\nChanged instructions\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if result := r.Kitsu("context", "show", page.ID); result.Code != 1 {
		t.Fatal("stale skill brief accepted", result.Output())
	}
}

func TestCompactBriefValidatesBeforeWritingReceipts(t *testing.T) {
	t.Parallel()
	r, _ := skillRepo(t)
	r.Write("kitsu-roles.toml", "version=1\n[paths]\ntests=['tests/**']\n")
	for _, extra := range [][]string{{"--round", "0"}, {"--stage", "design"}, {"--role", "unknown"}, {"--max-bytes", "512"}, {"--workflow", "full"}} {
		args := append([]string{"task", "brief", "fix-login", "--role", "developer", "--stage", "code", "--round", "1", "--host", "codex", "--workflow", "small"}, extra...)
		if result := r.Kitsu(args...); result.Code == 0 {
			t.Fatalf("accepted %v", args)
		}
		if r.Exists("openspec/changes/.skills") {
			t.Fatal("invalid request wrote receipt")
		}
	}
	r.Write("kitsu-roles.toml", "version=1\n[paths]\ntests=['tests/**']\n[codex.developer]\nskills=['missing']\n")
	if result := r.Kitsu("task", "brief", "fix-login", "--role", "developer", "--stage", "code", "--round", "1", "--host", "codex", "--workflow", "small"); result.Code == 0 || r.Exists("openspec/changes/.skills") {
		t.Fatal(result.Output())
	}
}

func TestCompactCLIHelpCompletionAndLegacyHookStayIndependent(t *testing.T) {
	t.Parallel()
	r, _ := skillRepo(t)
	for _, args := range [][]string{{"context", "collect", "--help"}, {"context", "show", "--help"}, {"task", "brief", "--help"}, {"check", "run", "--help"}, {"task", "start", "--help"}, {"help", "context"}} {
		if result := r.Kitsu(args...); result.Code != 0 {
			t.Fatalf("%v: %s", args, result.Output())
		}
	}
	request := `{"version":1,"operations":[{"id":"source","kind":"read","path":"source.txt"}]}`
	r.Write("source.txt", "useful\n")
	result := r.Run(request, filepath.Join(kitsutest.Binary(t), compactBinaryName()), "context", "collect", "--request", "-", "--json")
	page := compactResult(t, result, 0)
	if page.Items[0].Lines[0].Text != "useful\n" {
		t.Fatal(page)
	}
	legacy := r.Run(`{"cwd":"`+filepath.ToSlash(r.Dir)+`"}`, filepath.Join(kitsutest.Binary(t), compactBinaryName()), "context", "--host", "claude")
	if legacy.Code != 0 || !strings.Contains(legacy.Stdout, "Kitsu verifies coding tasks") || strings.HasPrefix(legacy.Stdout, `{"version"`) {
		t.Fatal("legacy hook routed into collector", legacy.Output())
	}
	for _, line := range []string{"kitsu context co", "kitsu task br", "kitsu check run --com", "kitsu task start --com"} {
		if result := r.Kitsu("__complete", "--shell", "bash", "--line", line); result.Code != 0 || result.Stdout == "" {
			t.Fatal("missing completion", line, result.Output())
		}
	}
}

func compactBinaryName() string {
	if os.PathSeparator == '\\' {
		return "kitsu.exe"
	}
	return "kitsu"
}

func stringMustJSON(value any) string { data, _ := json.Marshal(value); return string(data) }

func TestTaskStartCompactModeIsExplicitAndHandoffOnly(t *testing.T) {
	t.Parallel()
	r, _ := skillRepo(t)
	for _, host := range []string{"codex", "claude"} {
		plain := r.Kitsu("task", "start", "Fix login", "--host", host, "--json")
		opted := r.Kitsu("task", "start", "Fix login", "--host", host, "--compact", "--json")
		if plain.Code != 0 || opted.Code != 0 {
			t.Fatal(plain.Output(), opted.Output())
		}
		if strings.Contains(plain.Stdout, "Kitsu compact mode") || !strings.Contains(opted.Stdout, "references/compact.md") {
			t.Fatal("compact rollout not opt-in")
		}
	}
	if r.Exists("openspec") {
		t.Fatal("handoff-only mode wrote records")
	}
}

func TestForcedBaseImpliedFailureRetainsDiagnostics(t *testing.T) {
	t.Parallel()
	r := compactChecksFixture(t, "fail")
	r.Git("add", "-A")
	r.Git("commit", "-qm", "declared base")
	page := compactResult(t, r.Kitsu("check", "run", "fail", "--base", "HEAD", "--force", "--compact"), 5)
	var meta compactCheckMeta
	if err := json.Unmarshal(page.Metadata, &meta); err != nil {
		t.Fatal(err)
	}
	if meta.Checks[0].Implied || meta.Checks[0].Skipped || page.Items[0].LogRunID == "" || !strings.Contains(stringMustJSON(page.Items), "IMPORTANT") {
		t.Fatal("forced run lost its evidence", meta)
	}
}

func TestCompactCheckLogPaginationReconstructsOriginal(t *testing.T) {
	t.Parallel()
	r := compactChecksFixture(t, "fail")
	page := compactResult(t, r.Kitsu("check", "run", "--compact"), 5)
	var meta compactCheckMeta
	if err := json.Unmarshal(page.Metadata, &meta); err != nil {
		t.Fatal(err)
	}
	original := r.Read(".git/kitsu/" + meta.Checks[0].Record.Log)
	var recovered strings.Builder
	cursor := ""
	for calls := 0; calls < 500; calls++ {
		args := []string{"context", "show", page.ID, "--item", "fail", "--max-bytes", "4096"}
		if cursor != "" {
			args = append(args, "--cursor", cursor)
		}
		result := r.Kitsu(args...)
		if result.Code != 0 || len(result.Stdout) > 4096 {
			t.Fatal(result.Output())
		}
		var next contextpack.Page
		if err := json.Unmarshal([]byte(result.Stdout), &next); err != nil {
			t.Fatal(err)
		}
		for _, line := range next.Items[0].Lines {
			prior := recovered.String()
			if line.Number != strings.Count(prior, "\n")+1 {
				t.Fatal("line reference changed", line)
			}
			offset := len(prior) - strings.LastIndex(prior, "\n") - 1
			if line.Offset != offset {
				t.Fatal("byte offset changed", line, offset)
			}
			recovered.WriteString(line.Text)
		}
		cursor = next.Items[0].Next
		if cursor == "" {
			break
		}
	}
	if cursor != "" || recovered.String() != original {
		t.Fatal("log pagination lost or repeated original retained text")
	}
}

func TestCompactMultipleLargeLogsDoNotReplaceCheckExit(t *testing.T) {
	t.Parallel()
	r := compactChecksFixture(t, "flood-a", "flood-b", "flood-c", "flood-d")
	page := compactResult(t, r.Kitsu("check", "run", "--compact"), 0)
	var meta compactCheckMeta
	if err := json.Unmarshal(page.Metadata, &meta); err != nil {
		t.Fatal(err)
	}
	if !meta.Verified || len(meta.Checks) != 4 {
		t.Fatal("large successful logs broke check report", meta)
	}
	for _, item := range page.Items {
		if item.LogChunks < 10000 || item.Total != item.Omitted || len(item.Lines) != 0 {
			t.Fatal("large successful log preview inflated", item)
		}
	}
	if !strings.Contains(r.Read(".git/kitsu/"+meta.Checks[0].Record.Log), "bytes of output were dropped") {
		t.Fatal("runner retention marker missing")
	}
	if result := r.Kitsu("context", "show", page.ID, "--item", "flood-a", "--max-bytes", "16384"); result.Code != 0 {
		t.Fatal(result.Output())
	}
}

func TestBriefExternalSkillChangesInvalidateOnlyItsPolicy(t *testing.T) {
	t.Parallel()
	r, _ := skillRepo(t)
	path := filepath.Join(r.Home, ".agents", "skills", "external", "SKILL.md")
	writeSkillFixture(t, path, "external")
	r.Write("kitsu-roles.toml", "version=1\n[codex.reviewer]\nskills=['external']\n")
	page, b := brief(t, r, "codex", "reviewer", "review")
	if len(b.Skills) != 1 || b.Skills[0].Path != path {
		t.Fatal(b.Skills)
	}
	before := r.Git("status", "--porcelain")
	writeSkillFixture(t, path, "external-changed")
	if r.Git("status", "--porcelain") != before {
		t.Fatal("external fixture changed source")
	}
	result := r.Kitsu("context", "show", page.ID)
	if result.Code != 1 {
		t.Fatal("external source hash change accepted", result.Output())
	}
}

func TestCompactReportRejectsEvidenceCompletionAfterVerdict(t *testing.T) {
	t.Parallel()
	r := compactChecksFixture(t, "pass")
	page := compactResult(t, r.Kitsu("check", "run", "--compact"), 0)
	var meta compactCheckMeta
	if err := json.Unmarshal(page.Metadata, &meta); err != nil {
		t.Fatal(err)
	}
	repo, err := gitx.Open(t.Context(), r.Dir)
	if err != nil {
		t.Fatal(err)
	}
	e, err := contextpack.Open(repo)
	if err != nil {
		t.Fatal(err)
	}
	s := &session{repo: repo, store: store.Open(repo.CommonDir), compact: &compactCheckCapture{compactCheckMeta: meta}}
	// Another finished run changes the evidence set after this verdict was captured.
	if result := r.Kitsu("check", "run", "--force"); result.Code != 0 {
		t.Fatal(result.Output())
	}
	if _, err := compactCheckArtifact(t.Context(), s, e, page.State, contextpack.DefaultBytes); err == nil || !strings.Contains(err.Error(), "records changed") {
		t.Fatal("accepted verdict with another evidence set", err)
	}
}

func TestCompactReportKeepsExpiryEvenWhenItPassesDuringPreparation(t *testing.T) {
	c := &compactCheckCapture{}
	ended := time.Now().Add(-evidence.MaxAge - time.Second)
	c.observeVerdict(&evidence.Verdict{Verified: true, Checks: []evidence.Check{{Deciding: &store.Record{EndedAt: &ended}}}})
	if c.ExpiresAt.IsZero() || c.ExpiresAt.After(time.Now()) {
		t.Fatal("expired boundary was dropped")
	}
	r, _ := skillRepo(t)
	repo, err := gitx.Open(t.Context(), r.Dir)
	if err != nil {
		t.Fatal(err)
	}
	s := &session{repo: repo, store: store.Open(repo.CommonDir)}
	c.EvidenceKey, err = compactEvidenceKey(s)
	if err != nil {
		t.Fatal(err)
	}
	metadata, _ := json.Marshal(c.compactCheckMeta)
	if err := validateCompactMetadata(t.Context(), s, contextpack.Artifact{Kind: "checks", Metadata: metadata}); err == nil || !strings.Contains(err.Error(), "expired") {
		t.Fatal("expired report accepted", err)
	}
}

func TestCompactCheckCancellationMatchesNormalRunner(t *testing.T) {
	r := compactChecksFixture(t, "wait", "pass")
	t.Chdir(r.Dir)
	for _, kv := range r.Env() {
		name, value, ok := strings.Cut(kv, "=")
		if ok && name != "" {
			t.Setenv(name, value)
		}
	}
	for _, compact := range []bool{false, true} {
		if err := os.Remove(r.Path(".git/sequence-ready")); err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(t.Context())
		var out, diagnostic bytes.Buffer
		args := []string{"check", "run", "--force"}
		if compact {
			args = append(args, "--compact")
		}
		done := make(chan error, 1)
		go func() { done <- Execute(ctx, args, Config{In: strings.NewReader(""), Out: &out, Err: &diagnostic}) }()
		deadline := time.Now().Add(5 * time.Second)
		for !r.Exists(".git/sequence-ready") && time.Now().Before(deadline) {
			time.Sleep(5 * time.Millisecond)
		}
		cancel()
		select {
		case err := <-done:
			if ExitCode(err) != 5 {
				t.Fatalf("compact=%t cancellation exit %d: %v", compact, ExitCode(err), err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("cancelled check did not terminate")
		}
		if compact {
			var page contextpack.Page
			if err := json.Unmarshal(out.Bytes(), &page); err != nil {
				t.Fatal(err, diagnostic.String())
			}
			var meta compactCheckMeta
			if err := json.Unmarshal(page.Metadata, &meta); err != nil {
				t.Fatal(err)
			}
			if meta.Checks[0].Record.Outcome != store.Interrupted || meta.Verified {
				t.Fatal("interruption lost normal evidence", meta)
			}
			if len(meta.Checks) != 2 || meta.Checks[1].State != "not_attempted" || meta.Checks[1].Record != nil || meta.Error == "" {
				t.Fatal("cancellation hid unattempted checks or judgement uncertainty", meta)
			}
		}
	}
}
