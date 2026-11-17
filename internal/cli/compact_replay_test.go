package cli

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/belevtsev/kitsu/v3/internal/contextpack"
	"github.com/belevtsev/kitsu/v3/internal/kitsutest"
)

const replayDiagnostic = "FAIL: middle-only diagnostic: preserve λ and 🐕"

// This subprocess is a declared check, not a model or agent CLI invocation.
func TestCompactReplayCheckChild(t *testing.T) {
	if os.Getenv("KITSU_TEST_COMPACT_REPLAY_CHILD") != "1" {
		return
	}
	fmt.Fprint(os.Stderr, strings.Repeat(workflowNoise, 120))
	fmt.Fprintln(os.Stderr, replayDiagnostic)
	fmt.Fprint(os.Stderr, strings.Repeat(workflowNoise, 120))
	os.Exit(7)
}

// These are deterministic offline transport replays, not solver-quality or token
// measurements. Selective uses native reads for short files and paged collection
// for noisy output. Every expansion, rejected page, fallback, saved parent
// envelope and worker's original read is charged. No provider-cache inference.
func BenchmarkCompactSelectiveReplay(b *testing.B) {
	for _, workload := range []string{"short-source", "last-search-match", "utf8-long-line", "buried-check", "saved-handoff"} {
		b.Run(workload, func(b *testing.B) {
			for _, selective := range []bool{false, true} {
				arm := "normal"
				if selective {
					arm = "selective"
				}
				b.Run(arm, func(b *testing.B) {
					m := replayMeter{workflowMeter: workflowMeter{t: b, repo: replayFixture(b, workload)}}
					for b.Loop() {
						replayRun(&m, workload, selective)
					}
					m.report(b)
					b.ReportMetric(float64(m.nativeReads)/float64(b.N), "native-reads/op")
					b.ReportMetric(float64(m.fallbacks)/float64(b.N), "fallbacks/op")
				})
			}
		})
	}
}

func TestCompactSelectiveReplayRequiredFacts(t *testing.T) {
	for _, workload := range []string{"short-source", "last-search-match", "utf8-long-line", "buried-check", "saved-handoff"} {
		t.Run(workload, func(t *testing.T) {
			t.Parallel()
			r := replayFixture(t, workload)
			normal := replayMeter{workflowMeter: workflowMeter{t: t, repo: r, retainTransports: true}}
			selective := replayMeter{workflowMeter: workflowMeter{t: t, repo: r, retainTransports: true}}
			want, got := replayRun(&normal, workload, false), replayRun(&selective, workload, true)
			verifyReplayLedger(t, &normal.workflowMeter)
			verifyReplayLedger(t, &selective.workflowMeter)
			if !maps.Equal(want, got) {
				t.Fatalf("required facts differ:\nnormal=%#v\nselective=%#v", want, got)
			}
			switch workload {
			case "short-source":
				if selective.calls != normal.calls || selective.expansions != 0 || selective.nativeReads != 1 {
					t.Fatal("short file did not take the native whole-file route")
				}
			case "last-search-match", "utf8-long-line", "buried-check":
				if selective.expansions == 0 || selective.responseBytes <= int64(len(got["body"])) {
					t.Fatal("required expansions or their transport bytes were not charged")
				}
				if workload == "utf8-long-line" && selective.fallbacks != 1 {
					t.Fatal("insufficient page budget did not exercise a charged retry")
				}
			case "saved-handoff":
				if selective.nativeReads != 2 || selective.calls != 4 {
					t.Fatal("parent envelope, complete worker brief, or original skill read was omitted")
				}
				last := normal.transports[len(normal.transports)-1]
				if last.Boundary != "fake-native-dispatch" || !strings.Contains(last.Request, `"assignment":`) || !strings.Contains(last.Request, `Preserve anonymous fallback and λ.`) {
					t.Fatal("normal assignment fact was not transported", last)
				}
				if selective.transports[1].Boundary != "fake-native-dispatch" || !strings.Contains(selective.transports[1].Request, `"sha256":`) || !strings.Contains(selective.transports[1].Request, `"model":`) {
					t.Fatal("saved packet was not sent through a charged native dispatch before worker reads")
				}
			}
			t.Logf("normal calls=%d request_bytes=%d response_bytes=%d; selective calls=%d request_bytes=%d response_bytes=%d expansions=%d fallbacks=%d native_reads=%d",
				normal.calls, normal.requestBytes, normal.responseBytes, selective.calls, selective.requestBytes, selective.responseBytes, selective.expansions, selective.fallbacks, selective.nativeReads)
		})
	}
}

type replayMeter struct {
	workflowMeter
	nativeReads int64
	fallbacks   int64
}

// Native reads use an explicitly fake JSON host envelope and the actual bytes
// returned by os.ReadFile. CLI requests use the separate canonical-cli format.
func (m *replayMeter) read(path string) string {
	m.t.Helper()
	m.nativeReads++
	body, err := os.ReadFile(path)
	if err != nil {
		m.t.Fatal(err)
	}
	response := stringMustJSON(map[string]string{"body": string(body)})
	m.recordTransport("fake-native-read", stringMustJSON(map[string]string{"tool": "read_file", "path": path}), response, "")
	return workflowJSON[map[string]string](m.t, response)["body"]
}

func replayFixture(t testing.TB, workload string) *kitsutest.Repo {
	t.Helper()
	r := workflowFixture(t, "exploration")
	r.Write("short.go", "package greeting\nfunc label() string { return \"anonymous\" }\n")
	var search strings.Builder
	for i := range 80 {
		fmt.Fprintf(&search, "resolveLabel match %03d: %s\n", i, strings.Repeat("nearby context ", 6))
	}
	search.WriteString("resolveLabel FINAL_REQUIRED_MATCH: preserve anonymous fallback\n")
	r.Write("search.txt", search.String())
	r.Write("long.txt", "prefix\n"+strings.Repeat("Ж🐕λ", 900)+"LAST_REQUIRED_UTF8_FACT\n")
	program, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if workload == "buried-check" {
		r.Setenv("KITSU_TEST_COMPACT_REPLAY_CHILD", "1")
		r.Setenv("GORACE", "atexit_sleep_ms=0")
		r.Write("kitsu.toml", fmt.Sprintf("version=1\n[[check]]\nid='buried'\nrun=[%q,'-test.run=^TestCompactReplayCheckChild$']\ntimeout='10s'\n", strings.ReplaceAll(program, `\`, "/")))
	}
	r.Git("add", "-A")
	r.Git("commit", "-qm", "adversarial replay fixture")
	return r
}

func replayCollect(m *replayMeter, operation contextpack.Operation, budget string) contextpack.Page {
	m.t.Helper()
	data, err := json.Marshal(contextpack.Request{Version: 1, Operations: []contextpack.Operation{operation}})
	if err != nil {
		m.t.Fatal(err)
	}
	return m.decodePage(m.run(string(data), "kitsu", 0, "context", "collect", "--request", "-", "--max-bytes", budget, "--json").Stdout)
}

func replayPages(m *replayMeter, page contextpack.Page, retry bool) string {
	m.t.Helper()
	if len(page.Items) != 1 || page.Items[0].Omitted == 0 || page.Items[0].Next == "" {
		m.t.Fatal("fixture did not omit the required later evidence", page.Items)
	}
	item := page.Items[0]
	var body strings.Builder
	appendLines := func() {
		if item.Error != "" || item.Skipped != 0 {
			m.t.Fatal("incomplete sources cannot supply equivalent facts", item)
		}
		for _, line := range item.Lines {
			if !utf8.ValidString(line.Text) {
				m.t.Fatal("pagination split a UTF-8 rune")
			}
			if !item.Preview {
				prior := body.String()
				if line.Number != strings.Count(prior, "\n")+1 || line.Offset != len(prior)-strings.LastIndex(prior, "\n")-1 {
					m.t.Fatal("pagination changed source line/byte anchors", line)
				}
				body.WriteString(line.Text)
			}
		}
	}
	appendLines()
	for count := 0; item.Omitted > 0; count++ {
		if count >= 200 || item.Next == "" {
			m.t.Fatal("pagination failed to make bounded progress")
		}
		if retry {
			// A page with a long chunk cannot fit. Charge the failed request and
			// its diagnostic before retrying with a budget that makes progress.
			m.expansions++
			result := m.run("", "kitsu", 2, "context", "show", page.ID, "--item", item.ID, "--cursor", item.Next, "--max-bytes", "1024", "--json")
			if !strings.Contains(result.Stderr, "increase --max-bytes") {
				m.t.Fatal("fallback did not follow an insufficient-budget diagnostic")
			}
			m.fallbacks++
			retry = false
		}
		next := m.expand(page.ID, item.ID, item.Next, "4096")
		item = next.Items[0]
		appendLines()
	}
	return body.String()
}

func replayRun(m *replayMeter, workload string, selective bool) map[string]string {
	m.t.Helper()
	if workload == "saved-handoff" {
		return replayHandoff(m, selective)
	}
	if workload == "buried-check" {
		var log string
		facts := map[string]string{}
		if selective {
			page := m.page(5, "check", "run", "--force", "--compact", "--json")
			meta := workflowJSON[compactCheckMeta](m.t, string(page.Metadata))
			if len(meta.Checks) != 1 || meta.Checks[0].Record == nil || meta.Checks[0].Record.ExitStatus == nil || strings.Contains(stringMustJSON(page.Items), replayDiagnostic) {
				m.t.Fatal("buried failure fixture or outcome lost", meta)
			}
			facts["outcome"] = meta.Checks[0].Record.Outcome + ":" + strconv.Itoa(*meta.Checks[0].Record.ExitStatus)
			facts["verified"] = strconv.FormatBool(meta.Verified)
			log = replayPages(m, page, false)
		} else {
			log = m.run("", "kitsu", 5, "check", "run", "--force").Output()
			status := workflowJSON[statusReport](m.t, m.run("", "kitsu", 5, "gate", "status", "--json").Stdout)
			if len(status.Checks) != 1 || status.Checks[0].ExitStatus == nil {
				m.t.Fatal("native check outcome missing", status)
			}
			facts["outcome"] = status.Checks[0].Outcome + ":" + strconv.Itoa(*status.Checks[0].ExitStatus)
			facts["verified"] = strconv.FormatBool(status.Verified)
		}
		if strings.Count(log, replayDiagnostic) != 1 || strings.Count(log, workflowNoise) != 240 || facts["outcome"] != "failed:7" || facts["verified"] != "false" {
			m.t.Fatal("buried failure or surrounding retained context lost")
		}
		facts["diagnostic"], facts["noise_lines"] = replayDiagnostic, "240"
		return facts
	}
	path, kind := "short.go", "read"
	if workload == "last-search-match" {
		path, kind = "search.txt", "search"
	} else if workload == "utf8-long-line" {
		path = "long.txt"
	}
	var body string
	if !selective || workload == "short-source" {
		if kind == "search" {
			// -h omits filenames; compact still validates line anchors below.
			body = m.run("", "git", 0, "grep", "-h", "-F", "resolveLabel", "--", path).Stdout
		} else {
			body = m.read(m.repo.Path(path))
		}
	} else {
		op := contextpack.Operation{ID: "facts", Kind: kind, Path: path}
		if kind == "search" {
			op.Pattern = "resolveLabel"
		}
		page := replayCollect(m, op, "4096")
		if strings.Contains(stringMustJSON(page.Items), "FINAL_REQUIRED_MATCH") || strings.Contains(stringMustJSON(page.Items), "LAST_REQUIRED_UTF8_FACT") {
			m.t.Fatal("required fact was not actually outside the initial page")
		}
		body = replayPages(m, page, workload == "utf8-long-line")
	}
	return map[string]string{"body": body}
}

func replayHandoff(m *replayMeter, selective bool) map[string]string {
	m.t.Helper()
	assignment := "Preserve anonymous fallback and λ.\nRead the complete original skill.\n"
	if !selective {
		facts := workflowHandoff(&m.workflowMeter, false)
		// The original baseline used to insert this fact without transporting
		// it. Send and decode the complete fake native dispatch instead.
		request := stringMustJSON(map[string]string{"tool": "spawn_worker", "assignment": assignment, "role": facts["role"], "instructions": facts["instructions"], "model": facts["model"], "effort": facts["effort"]})
		m.recordTransport("fake-native-dispatch", request, `{"worker":"offline-worker"}`, "")
		facts["assignment"] = workflowJSON[map[string]string](m.t, request)["assignment"]
		return facts
	}
	result := m.run(assignment, "kitsu", 0, "task", "brief", workflowTask, "--role", "developer", "--stage", "code", "--round", "1", "--host", "codex", "--workflow", "small", "--assignment", "-", "--save")
	d := workflowJSON[workerDispatch](m.t, result.Stdout)
	m.recordTransport("fake-native-dispatch", stringMustJSON(map[string]any{"tool": "spawn_worker", "label": d.Label, "path": d.Path, "sha256": d.SHA256, "model": d.Model, "prompt": d.Label + "\nRead and verify the complete original packet and skills."}), `{"worker":"offline-worker"}`, "")
	body := m.read(d.Path)
	if workerBriefHash([]byte(body)) != d.SHA256 || !strings.HasSuffix(body, "Original assignment:\n"+assignment) {
		m.t.Fatal("saved brief changed the original assignment or immutable identity")
	}
	_, metadata, ok := strings.Cut(body, "Worker brief metadata:\n")
	if !ok {
		m.t.Fatal("saved worker metadata missing")
	}
	var b workerBrief
	if err := json.NewDecoder(strings.NewReader(metadata)).Decode(&b); err != nil {
		m.t.Fatal(err)
	}
	_, role, ok := strings.Cut(body, "Original role instructions:\n")
	if !ok || len(b.Skills) != 1 || len(b.TestPaths) != 1 {
		m.t.Fatal("saved role/skill scope missing")
	}
	role, _, _ = strings.Cut(role, "\n\nOriginal assignment:\n")
	skill := m.read(b.Skills[0].Path)
	if b.Skills[0].SHA256 != fmt.Sprintf("%x", sha256.Sum256([]byte(skill))) || b.LoadStatus != "unverified" || b.Baseline != d.Baseline || b.Selection != d.Selection {
		m.t.Fatal("original skill read or authority facts lost")
	}
	return map[string]string{"role": b.Role, "model": b.Model.Model, "effort": b.Model.Effort, "writes": b.Writes,
		"instructions": role, "tests": b.TestPaths[0], "baseline": b.Baseline, "load_status": b.LoadStatus,
		"skill_id": b.Skills[0].ID, "skill_path": b.Skills[0].Path, "skill_sha256": b.Skills[0].SHA256, "skill_body": skill, "assignment": assignment}
}
