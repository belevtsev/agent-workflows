package cli

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/belevtsev/kitsu/v3/internal/contextpack"
	"github.com/belevtsev/kitsu/v3/internal/kitsutest"
	"github.com/belevtsev/kitsu/v3/internal/roles"
	"github.com/belevtsev/kitsu/v3/internal/skills"
)

const (
	workflowTask     = "fix-label"
	workflowSkill    = ".agents/skills/local/SKILL.md"
	workflowBudget   = "4096"
	workflowNoise    = "routine output that need not be sent to a model\n"
	workflowFailure  = "IMPORTANT: intentional regression diagnostic"
	workflowRoleFile = "version=1\n[paths]\ntests=['tests/**']\n[codex.developer]\nmodel='benchmark-model'\neffort='max'\nskills=['local']\n"
)

// These are offline CLI workflows, not model/token or quality benchmarks. Both
// arms receive the same committed fixture and must recover the same needed facts.
// Git supplies portable unbounded file/search output without requiring a shell,
// sed, or rg. The handoff additionally reads the original skill in both arms.
// Every measured CLI invocation, including expansions and status queries, counts.
// ns/op includes command execution, decoding, and needed-fact verification.
func BenchmarkCompactWorkflows(b *testing.B) {
	for _, workload := range []string{"exploration", "handoff", "checks"} {
		b.Run(workload, func(b *testing.B) {
			for _, compact := range []bool{false, true} {
				arm := "normal"
				if compact {
					arm = "compact"
				}
				b.Run(arm, func(b *testing.B) {
					r := workflowFixture(b, workload)
					meter := workflowMeter{t: b, repo: r}
					for b.Loop() {
						workflowRun(&meter, workload, compact)
					}
					meter.report(b)
				})
			}
		})
	}
}

func TestCompactWorkflowNeededFacts(t *testing.T) {
	for _, workload := range []string{"exploration", "handoff", "checks"} {
		t.Run(workload, func(t *testing.T) {
			r := workflowFixture(t, workload)
			normal, compact := workflowMeter{t: t, repo: r}, workflowMeter{t: t, repo: r}
			want := workflowRun(&normal, workload, false)
			got := workflowRun(&compact, workload, true)
			if !maps.Equal(got, want) {
				t.Fatalf("needed facts differ:\nnormal: %#v\ncompact: %#v", want, got)
			}
			if workload == "exploration" {
				if compact.expansions == 0 {
					t.Fatal("fixture did not exercise required expansion calls")
				}
				workflowRun(&compact, workload, true)
				if compact.cacheHits != 1 {
					t.Fatalf("repeated collection should report one local artifact hit, got %d", compact.cacheHits)
				}
			}
		})
	}
}

func TestCompactWorkflowNoisyCheckLogRecoverable(t *testing.T) {
	r := workflowFixture(t, "checks")
	meter := workflowMeter{t: t, repo: r}
	page := meter.page(5, "check", "run", "--force", "--compact", "--json")
	full := meter.expand(page.ID, "fail", "", "1048576")
	var recovered strings.Builder
	for _, line := range full.Items[0].Lines {
		recovered.WriteString(line.Text)
	}
	if full.Items[0].Omitted != 0 || strings.Count(recovered.String(), workflowNoise) != 2000 || !strings.Contains(recovered.String(), workflowFailure) {
		t.Fatal("complete retained failure log was not recoverable")
	}
	if meter.calls != 2 || meter.expansions != 1 || meter.responseBytes < int64(len(recovered.String())) {
		t.Fatal("expansion request/response was not included in transport accounting", meter)
	}
}

type workflowMeter struct {
	t                testing.TB
	repo             *kitsutest.Repo
	calls            int64
	requestBytes     int64
	responseBytes    int64
	expansions       int64
	cacheHits        int64
	transports       []replayTransport
	retainTransports bool
}

func (m *workflowMeter) run(input, program string, code int, args ...string) kitsutest.Result {
	m.t.Helper()
	// Request bytes are UTF-8 CLI words separated by one space, a final newline,
	// and the complete stdin JSON. No shell-quoting or token estimate is added.
	request := program + " " + strings.Join(args, " ") + "\n" + input
	executable := program
	if program == "kitsu" {
		executable = filepath.Join(kitsutest.Binary(m.t), compactBinaryName())
	}
	result := m.repo.Run(input, executable, args...)
	m.recordTransport("canonical-cli", request, result.Stdout, result.Stderr)
	if result.Code != code {
		m.t.Fatalf("%s %v: exit %d, want %d\n%s", program, args, result.Code, code, result.Output())
	}
	return result
}

func (m *workflowMeter) page(code int, args ...string) contextpack.Page {
	m.t.Helper()
	return m.decodePage(m.run("", "kitsu", code, args...).Stdout)
}

func (m *workflowMeter) decodePage(data string) contextpack.Page {
	m.t.Helper()
	page := workflowJSON[contextpack.Page](m.t, data)
	if page.CacheHit {
		m.cacheHits++
	}
	return page
}

func (m *workflowMeter) expand(id, item, cursor, budget string) contextpack.Page {
	m.t.Helper()
	m.expansions++
	args := []string{"context", "show", id, "--item", item, "--max-bytes", budget, "--json"}
	if cursor != "" {
		args = append(args, "--cursor", cursor)
	}
	return m.page(0, args...)
}

func (m *workflowMeter) report(b *testing.B) {
	b.Helper()
	for unit, count := range map[string]int64{
		"calls/op": m.calls, "request-B/op": m.requestBytes, "response-B/op": m.responseBytes,
		"expansions/op": m.expansions, "cache-hits/op": m.cacheHits,
	} {
		b.ReportMetric(float64(count)/float64(b.N), unit)
	}
}

// cache-hits/op counts only cache_hit=true reported by the LOCAL artifact
// collector. First collections are cold; later iterations may reuse that local
// artifact. Showing an artifact is an expansion, not a reported collection hit.
// Fixtures, binary compilation, and initial commits are outside the timed loop.
// Checks use --force in both arms, so passing-check reuse cannot skew their work.
func workflowFixture(t testing.TB, workload string) *kitsutest.Repo {
	t.Helper()
	r := kitsutest.NewRepo(t)
	r.Setenv("CODEX_HOME", filepath.Join(r.Home, ".codex"))
	r.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(r.Home, ".claude"))
	r.Setenv("XDG_CONFIG_HOME", filepath.Join(r.Home, "config"))
	r.Setenv("APPDATA", filepath.Join(r.Home, "config"))
	r.Write("kitsu-roles.toml", workflowRoleFile)
	r.Write("kitsu.toml", "version=1\n")
	r.Write(workflowSkill, "---\nname: local\ndescription: Follow the local greeting conventions\n---\nKeep the anonymous-name fallback.\n")
	noise := strings.Repeat("// Nearby source context that may be relevant during repository exploration.\n", 36)
	r.Write("src/service.go", "package greeting\n\n"+noise+"func resolveLabel(name string) string {\n\tif name == \"\" {\n\t\treturn \"anonymous\"\n\t}\n\treturn name\n}\n")
	r.Write("tests/service_test.go", "package greeting\n\nimport \"testing\"\n\n"+noise+"func TestEmptyLabel(t *testing.T) {\n\tif got := resolveLabel(\"\"); got != \"anonymous\" {\n\t\tt.Fatal(got)\n\t}\n}\n")
	if workload == "checks" {
		program, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		r.Setenv("KITSU_TEST_COMPACT_CHECK", "1")
		r.Setenv("GORACE", "atexit_sleep_ms=0")
		var declaration strings.Builder
		declaration.WriteString("version=1\n")
		for _, mode := range []string{"pass", "fail"} {
			fmt.Fprintf(&declaration, "[[check]]\nid=%q\nrun=[%q,%q]\ntimeout='5s'\n", mode, filepath.ToSlash(program), mode)
		}
		r.Write("kitsu.toml", declaration.String())
	}
	r.Git("add", "-A")
	r.Git("commit", "-q", "-m", "benchmark fixture")
	return r
}

func workflowRun(m *workflowMeter, workload string, compact bool) map[string]string {
	m.t.Helper()
	switch workload {
	case "exploration":
		return workflowExplore(m, compact)
	case "handoff":
		return workflowHandoff(m, compact)
	case "checks":
		return workflowChecks(m, compact)
	default:
		m.t.Fatalf("unknown workflow %q", workload)
		return nil
	}
}

func workflowExplore(m *workflowMeter, compact bool) map[string]string {
	m.t.Helper()
	facts := map[string]string{}
	if !compact {
		result := m.run("", "git", 0, "grep", "-n", "-F", "-e", "resolveLabel", "--", "src", "tests")
		for line := range strings.SplitSeq(strings.TrimSpace(result.Stdout), "\n") {
			parts := strings.SplitN(line, ":", 3)
			if len(parts) != 3 {
				m.t.Fatalf("invalid grep evidence %q", line)
			}
			facts["search:"+parts[0]+":"+strings.TrimSpace(parts[2])] = parts[1]
		}
		for _, path := range []string{"src/service.go", "tests/service_test.go"} {
			data := m.run("", "git", 0, "show", "HEAD:"+path).Stdout
			for i, line := range strings.Split(strings.TrimSuffix(data, "\n"), "\n") {
				workflowSourceFact(facts, path, i+1, line)
			}
		}
	} else {
		request := contextpack.Request{Version: 1, Operations: []contextpack.Operation{
			{ID: "symbols", Kind: "search", Pattern: "resolveLabel"},
			{ID: "source", Kind: "read", Path: "src/service.go"},
			{ID: "tests", Kind: "read", Path: "tests/service_test.go"},
		}}
		data, err := json.Marshal(request)
		if err != nil {
			m.t.Fatal(err)
		}
		page := m.decodePage(m.run(string(data), "kitsu", 0, "context", "collect", "--request", "-", "--max-bytes", workflowBudget, "--json").Stdout)
		for _, item := range page.Items {
			for pages := 0; ; pages++ {
				if item.Status != "ok" || item.Skipped != 0 || item.Error != "" {
					m.t.Fatal("incomplete exploration", item)
				}
				for _, line := range item.Lines {
					path := line.Path
					if path == "" {
						path = item.Path
					}
					if item.ID == "symbols" {
						facts["search:"+path+":"+strings.TrimSpace(line.Text)] = strconv.Itoa(line.Number)
					} else {
						workflowSourceFact(facts, path, line.Number, line.Text)
					}
				}
				if item.Omitted == 0 {
					break
				}
				if item.Next == "" || pages >= 16 {
					m.t.Fatal("exploration cannot make bounded retrieval progress", item)
				}
				next := m.expand(page.ID, item.ID, item.Next, workflowBudget)
				item = next.Items[0]
			}
		}
	}
	if len(facts) != 6 || facts["src/service.go:return \"anonymous\""] != "41" || facts["tests/service_test.go:func TestEmptyLabel(t *testing.T) {"] != "41" {
		m.t.Fatalf("needed source facts/anchors missing: %#v", facts)
	}
	return facts
}

func workflowSourceFact(facts map[string]string, path string, number int, text string) {
	text = strings.TrimSpace(text)
	if strings.HasPrefix(text, "func resolveLabel(") || text == `return "anonymous"` || strings.HasPrefix(text, "func TestEmptyLabel(") || strings.Contains(text, `got != "anonymous"`) {
		facts[path+":"+text] = strconv.Itoa(number)
	}
}

func workflowHandoff(m *workflowMeter, compact bool) map[string]string {
	m.t.Helper()
	facts := map[string]string{}
	var skill compactSkill
	if compact {
		page := m.page(0, "task", "brief", workflowTask, "--role", "developer", "--stage", "code", "--round", "1", "--host", "codex", "--workflow", "small", "--json")
		brief := workflowJSON[workerBrief](m.t, string(page.Metadata))
		if len(brief.Skills) != 1 || len(brief.TestPaths) != 1 || brief.Label != "kitsu-task: fix-label code 1" || brief.State.Stage != "unknown" || len(brief.Constraints) < 5 {
			m.t.Fatal("brief lost the fresh worker contract", brief)
		}
		skill = brief.Skills[0]
		facts["role"], facts["model"], facts["effort"] = brief.Role, brief.Model.Model, brief.Model.Effort
		facts["writes"], facts["instructions"], facts["tests"] = brief.Writes, brief.Instructions, brief.TestPaths[0]
		facts["baseline"], facts["load_status"] = brief.Baseline, brief.LoadStatus
	} else {
		facts["instructions"] = m.run("", "kitsu", 0, "role", "text", "developer").Stdout
		shown := m.run("", "kitsu", 0, "role", "show", "developer", "--host", "codex", "--json")
		settings := workflowJSON[struct {
			Role   string                 `json:"role"`
			Writes string                 `json:"writes"`
			Models map[string]roles.Model `json:"models"`
		}](m.t, shown.Stdout)
		facts["role"], facts["writes"] = settings.Role, settings.Writes
		facts["model"], facts["effort"] = settings.Models["codex"].Model, settings.Models["codex"].Effort
		resolved := m.run("", "kitsu", 0, "skill", "resolve", "developer", "--host", "codex", "--task", workflowTask, "--workflow", "small", "--json")
		selection := workflowJSON[struct {
			Skills     []skills.Entry `json:"skills"`
			LoadStatus string         `json:"load_status"`
		}](m.t, resolved.Stdout)
		if len(selection.Skills) != 1 {
			m.t.Fatal("expected one selected skill", selection)
		}
		entry := selection.Skills[0]
		skill = compactSkill{ID: entry.ID, Path: entry.Path, SHA256: entry.SHA256}
		facts["load_status"] = selection.LoadStatus
		config := m.run("", "git", 0, "show", "HEAD:kitsu-roles.toml")
		if !strings.Contains(config.Stdout, "tests=['tests/**']") {
			m.t.Fatal("test write scope missing")
		}
		facts["tests"] = "tests/**"
		status := m.run("", "kitsu", 0, "gate", "status", "--json")
		facts["baseline"] = workflowJSON[statusReport](m.t, status.Stdout).Tree
	}
	facts["skill_id"], facts["skill_path"], facts["skill_sha256"] = skill.ID, skill.Path, skill.SHA256
	body := m.run("", "git", 0, "show", "HEAD:"+workflowSkill).Stdout
	facts["skill_body"] = body
	if facts["role"] != "developer" || facts["model"] != "benchmark-model" || facts["effort"] != "max" || facts["tests"] != "tests/**" || facts["load_status"] != "unverified" || facts["baseline"] == "" || facts["writes"] == "" || facts["instructions"] == "" || skill.Path != m.repo.Path(workflowSkill) || skill.SHA256 != fmt.Sprintf("%x", sha256.Sum256([]byte(body))) {
		m.t.Fatal("needed handoff facts missing", facts)
	}
	return facts
}

func workflowChecks(m *workflowMeter, compact bool) map[string]string {
	m.t.Helper()
	facts := map[string]string{}
	if compact {
		page := m.page(5, "check", "run", "--force", "--compact", "--json")
		meta := workflowJSON[compactCheckMeta](m.t, string(page.Metadata))
		facts["verified"] = strconv.FormatBool(meta.Verified)
		for _, check := range meta.Checks {
			if check.Record == nil || check.Record.ExitStatus == nil || check.Skipped {
				m.t.Fatal("forced check evidence missing", check)
			}
			facts[check.ID] = check.Record.Outcome + ":" + strconv.Itoa(*check.Record.ExitStatus)
		}
		if strings.Contains(stringMustJSON(page.Items), workflowFailure) {
			facts["diagnostic"] = workflowFailure
		}
	} else {
		result := m.run("", "kitsu", 5, "check", "run", "--force")
		if strings.Contains(result.Output(), workflowFailure) {
			facts["diagnostic"] = workflowFailure
		}
		result = m.run("", "kitsu", 5, "gate", "status", "--json")
		status := workflowJSON[statusReport](m.t, result.Stdout)
		facts["verified"] = strconv.FormatBool(status.Verified)
		for _, check := range status.Checks {
			if check.ExitStatus == nil {
				m.t.Fatal("check status missing exit status", check)
			}
			facts[check.ID] = check.Outcome + ":" + strconv.Itoa(*check.ExitStatus)
		}
	}
	if facts["pass"] != "passed:0" || facts["fail"] != "failed:7" || facts["verified"] != "false" || facts["diagnostic"] != workflowFailure {
		m.t.Fatal("needed check outcomes or diagnostic missing", facts)
	}
	return facts
}

func workflowJSON[T any](t testing.TB, data string) T {
	t.Helper()
	var result T
	if err := json.Unmarshal([]byte(data), &result); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, data)
	}
	return result
}
