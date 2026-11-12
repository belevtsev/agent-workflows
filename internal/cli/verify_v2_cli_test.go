package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	agentworkflows "github.com/belevtsev/agent-workflows/v3"
	"github.com/belevtsev/agent-workflows/v3/internal/cli"
	"github.com/belevtsev/agent-workflows/v3/internal/langpack"
	"github.com/belevtsev/agent-workflows/v3/internal/taskflow"
)

// Independent end-to-end tests of go.package-test/v2 through cli.Execute
// (P3a-2 interface contract, B3 and B21-B23). Expected texts are written from
// the contract.

const vvV2 = "go.package-test/v2"

func vvT1(p string) string {
	return "changed package " + p + " has no passed go test run at the current source"
}
func vvT2(p string) string {
	return "changed package " + p + " has no passed go test -race run at the current source"
}

const (
	vvRerun   = "check unit passed after a failed run at the same source and GOFLAGS, so it earns no Go evidence credit"
	vvSkipped = "check unit: test example.com/m/b TestSkipped was skipped and did not run"
	vvPlain   = "check unit: skipped tests are not listed because it ran without -v or -json"
	vvUnread  = "check unit: skipped tests are not listed because its log could not be read"
	vvCut     = "check unit: skipped tests are not listed because its log was cut at the storage limit"
	vvB2      = "package b depends on changed package a and has no passed go test run at the current source"
)

// B3: the shipped Go pack names v2, and cw task start records it.
func TestShippedGoRuleIsV2B3(t *testing.T) {
	builtIn, err := langpack.LoadManifests(agentworkflows.Packs)
	if err != nil {
		t.Fatal(err)
	}
	i := slices.IndexFunc(builtIn, func(m langpack.Manifest) bool { return m.ID == "go" })
	if i < 0 || builtIn[i].EvidenceRule != vvV2 {
		t.Errorf("B3 violated: built-in go pack %+v, want evidence_rule %q", builtIn, vvV2)
	}
	e := pkNewEnv(t)
	_, packs, err := e.start(t, tpRepo(t, map[string]string{"go.mod": "module m\n"}), tpPlan("go", "version"))
	var set taskflow.PackSet
	if err != nil || json.Unmarshal(packs, &set) != nil || len(set.Applied) != 1 || set.Applied[0].EvidenceRule != vvV2 {
		t.Errorf("B3 violated: cw task start recorded packs %s (%v), want one binding with evidence_rule %q", packs, err, vvV2)
	}
}

// vvTask is a task started through cli.Execute with no packs.json in a module
// where b imports a; a/a.go changed after start. Start and check runs get a
// ctx without a deadline (the plan's timeout_seconds bounds a check); each
// verify gets its own ctx with a deadline 2 minutes away, its facts budget.
type vvTask struct {
	e                 *pkEnv
	repo, dir, realGo string
}

// vvModule: TestF fails while the file CWV2_MARKER names exists, blocks after
// creating the file CWV2_BLOCK names, and logs 140 KiB when CWV2_BIG is set;
// b's TestSkipped always skips.
var vvModule = map[string]string{
	"go.mod": "module example.com/m\n\ngo 1.21\n",
	"a/a.go": "package a\n\nfunc F() int { return 1 }\n",
	"a/a_test.go": "package a\n\nimport (\n\t\"os\"\n\t\"strings\"\n\t\"testing\"\n\t\"time\"\n)\n\nfunc TestF(t *testing.T) {\n" +
		"\tif m := os.Getenv(\"CWV2_MARKER\"); m != \"\" {\n\t\tif _, err := os.Stat(m); err == nil {\n\t\t\tt.Fatal(\"marker present\")\n\t\t}\n\t}\n" +
		"\tif p := os.Getenv(\"CWV2_BLOCK\"); p != \"\" {\n\t\t_ = os.WriteFile(p, nil, 0o600)\n\t\ttime.Sleep(time.Minute)\n\t}\n" +
		"\tif os.Getenv(\"CWV2_BIG\") != \"\" {\n\t\tt.Log(strings.Repeat(\"x\", 140<<10))\n\t}\n\t_ = F()\n}\n",
	"b/b.go":      "package b\n\nimport \"example.com/m/a\"\n\nfunc G() int { return a.F() }\n",
	"b/b_test.go": "package b\n\nimport \"testing\"\n\nfunc TestG(t *testing.T) { _ = G() }\n\nfunc TestSkipped(t *testing.T) { t.Skip(\"not here\") }\n",
}

func vvStart(t *testing.T, timeout int, git bool, argv ...string) *vvTask {
	t.Helper()
	realGo, err := exec.LookPath("go")
	if err != nil {
		t.Skip("go is not on PATH")
	}
	for k, v := range map[string]string{"GOTOOLCHAIN": "local", "GOWORK": "off", "GOENV": "off", "GOFLAGS": "", "CWV2_MARKER": "", "CWV2_BLOCK": "", "CWV2_BIG": ""} {
		t.Setenv(k, v)
	}
	v := &vvTask{e: pkNewEnv(t), repo: tpRepo(t, vvModule), realGo: realGo}
	if git {
		v.git(t, "init", "-q")
		v.git(t, "add", ".")
		v.git(t, "commit", "-qm", "baseline")
	}
	a, _ := json.Marshal(argv)
	plan := `{"title":"t","kind":"substantial","checkpoints":{"required":false,"reason":"test"},` +
		`"requirements":[{"id":"r1","description":"d","check_ids":["unit"],"allow_non_execution":false}],` +
		`"checks":[{"id":"unit","argv":` + string(a) + `,"timeout_seconds":` + strconv.Itoa(timeout) + `}]}`
	out, err := v.cw(context.Background(), plan, "task", "start", "--plan", "-", "--repo", v.repo, "--store", filepath.Join(v.e.base, "store"))
	var rec struct{ Directory string }
	if err != nil || json.Unmarshal([]byte(out), &rec) != nil || rec.Directory == "" {
		t.Fatalf("precondition: cw task start: %v %s", err, out)
	}
	v.dir = rec.Directory
	v.write(t, "package a\n\nfunc F() int { return 2 }\n")
	return v
}

func (v *vvTask) git(t *testing.T, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", v.repo, "-c", "user.email=f@example.invalid", "-c", "user.name=F", "-c", "commit.gpgsign=false"}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+os.DevNull)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v %s", args, err, out)
	}
}

func (v *vvTask) write(t *testing.T, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(v.repo, "a", "a.go"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func (v *vvTask) cw(ctx context.Context, stdin string, args ...string) (string, error) {
	var out, errOut bytes.Buffer
	err := cli.Execute(ctx, append(args, "--config-dir", v.e.cfg), v.e.config(stdin, &out, &errOut))
	return out.String(), err
}

// run runs the check and requires it to pass or not.
func (v *vvTask) run(t *testing.T, pass bool) {
	t.Helper()
	if _, err := v.cw(context.Background(), "", "task", "run", "--task", v.dir, "--check", "unit"); (err == nil) != pass {
		t.Fatalf("precondition: check unit gave %v, want passed=%v", err, pass)
	}
}

func (v *vvTask) verify(t *testing.T) taskflow.Verification {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	out, err := v.cw(ctx, "", "task", "verify", "--task", v.dir)
	var ver taskflow.Verification
	if json.Unmarshal([]byte(out), &ver) != nil || ver.TaskID == "" {
		t.Fatalf("verify printed no verification: %v %s", err, out)
	}
	return ver
}

// lastRun returns the latest stored run of the task.
func (v *vvTask) lastRun(t *testing.T) taskflow.CheckRun {
	t.Helper()
	rec, err := taskflow.Load(v.dir)
	if err != nil || len(rec.Checks) == 0 {
		t.Fatalf("precondition: load the task: %v", err)
	}
	return rec.Checks[len(rec.Checks)-1]
}

// vvRace skips the row when -race cannot run in the test environment; call
// it after vvStart, which sets that environment.
func vvRace(t *testing.T) {
	t.Helper()
	if err := exec.Command("go", "list", "-race", "runtime").Run(); err != nil {
		t.Skipf("go list -race runtime fails here: %v", err)
	}
}

// vvWrapper puts first on PATH a go shim that answers -race with stderr line
// and exit 2 (when line is set), sleeps 6 s before it answers `go env GOROOT`,
// which cw runs to verify it (when slow), reports its own directory as GOROOT
// (when ownRoot), and otherwise runs the real go. It returns the shim.
func (v *vvTask) vvWrapper(t *testing.T, line string, slow, ownRoot bool) string {
	t.Helper()
	root := t.TempDir()
	script := "#!/bin/sh\n"
	if line != "" {
		script += "case \" $* \" in *\" -race \"*) printf '%s\\n' '" + line + "' >&2; exit 2;; esac\n"
	}
	if slow {
		script += "if [ \"$*\" = 'env GOROOT' ]; then /bin/sleep 6; fi\n"
	}
	if ownRoot {
		script += "if [ \"$*\" = 'env GOROOT' ]; then echo '" + root + "'; exit 0; fi\n"
	}
	shim := filepath.Join(root, "bin", "go")
	if os.Mkdir(filepath.Dir(shim), 0o755) != nil || os.WriteFile(shim, []byte(script+"exec '"+v.realGo+"' \"$@\"\n"), 0o755) != nil {
		t.Fatal("cannot write the go shim")
	}
	t.Setenv("PATH", filepath.Dir(shim)+string(os.PathListSeparator)+os.Getenv("PATH"))
	return shim
}

func vvPrefixed(list []string, prefixes ...string) []string {
	var out []string
	for _, s := range list {
		for _, p := range prefixes {
			if strings.HasPrefix(s, p) {
				out = append(out, s)
				break
			}
		}
	}
	return out
}

// vvWant fails the row unless every wanted problem and limitation is present
// and every unwanted one absent (membership, never a complete list).
func vvWant(t *testing.T, id string, ver taskflow.Verification, problems, limitations, noLimitations []string) {
	t.Helper()
	for _, p := range problems {
		if !slices.Contains(ver.Problems, p) {
			t.Errorf("%s violated: problems lack %q\nproblems %q", id, p, ver.Problems)
		}
	}
	for _, l := range limitations {
		if !slices.Contains(ver.Limitations, l) {
			t.Errorf("%s violated: limitations lack %q\nlimitations %q", id, l, ver.Limitations)
		}
	}
	for _, l := range noLimitations {
		if slices.Contains(ver.Limitations, l) {
			t.Errorf("%s violated: limitations include %q", id, l)
		}
	}
}

// B23.a, B23.b, B23.e1, B23.e2, B23.f.
func TestVerifyV2EndToEndB23(t *testing.T) {
	t.Run("B23.a raced run of a only", func(t *testing.T) {
		v := vvStart(t, 300, false, "go", "test", "-race", "./a")
		vvRace(t)
		v.run(t, true)
		vvWant(t, "B23.a/B2", v.verify(t), []string{vvB2}, nil, nil)
	})
	// Section 3.3, J4: Verify passes its ctx to Evaluate unchanged, so the
	// 2-minute deadline on cli.Execute's ctx outlasts a go shim that takes 6 s
	// to report the real GOROOT; a fresh ctx would end the facts budget at 5 s
	// and lose the B2 problem.
	t.Run("3.3/J4 verify's ctx sets the facts budget", func(t *testing.T) {
		v := vvStart(t, 300, false, "go", "test", "-race", "./a")
		vvRace(t)
		v.run(t, true)
		v.vvWrapper(t, "", true, false)
		ver := v.verify(t)
		vvWant(t, "3.3/J4/B2", ver, []string{vvB2}, nil, nil)
		if got := vvPrefixed(ver.Limitations, "go.package-test/v2 could not compute the package graph"); len(got) != 0 {
			t.Errorf("3.3/J4 violated: the facts budget ignored verify's ctx: %q", got)
		}
	})
	t.Run("B23.b raced full run", func(t *testing.T) {
		v := vvStart(t, 300, false, "go", "test", "-race", "./...")
		vvRace(t)
		v.run(t, true)
		ver := v.verify(t)
		if got := vvPrefixed(ver.Problems, "changed package ", "package "); len(got) != 0 {
			t.Errorf("B23.b violated: problems %q", got)
		}
		vvWant(t, "B23.b/B12/B22", ver, nil, []string{vvPlain}, []string{vvRerun})
	})
	t.Run("B23.e1 verify's GOOS/GOARCH do not waive -race", func(t *testing.T) {
		v := vvStart(t, 300, false, "go", "test", "./...")
		vvRace(t)
		v.run(t, true)
		t.Setenv("GOOS", "linux")
		t.Setenv("GOARCH", "386")
		ver := v.verify(t)
		vvWant(t, "B23.e1/B18", ver, []string{vvT2("a")}, nil, nil)
		if got := vvPrefixed(ver.Limitations, "-race cannot run"); len(got) != 0 {
			t.Errorf("B23.e1 violated: limitations %q", got)
		}
	})
	// B6 through the verified go: CGO_ENABLED=0 at verify makes -race
	// unavailable where it needs cgo; on macOS it needs none (B18), so there
	// -race stays required.
	t.Run("B23.e2 -race unavailable", func(t *testing.T) {
		v := vvStart(t, 300, false, "go", "test", "./...")
		v.run(t, true)
		t.Setenv("CGO_ENABLED", "0")
		ver := v.verify(t)
		if runtime.GOOS == "darwin" {
			vvWant(t, "B23.e2/B18", ver, []string{vvT2("a")}, nil, nil)
			return
		}
		if got := vvPrefixed(ver.Problems, "changed package ", "package "); len(got) != 0 {
			t.Errorf("B23.e2 violated: problems %q", got)
		}
		vvWant(t, "B23.e2/B6", ver, nil, []string{"-race cannot run on this host (go: -race requires cgo; enable cgo by setting CGO_ENABLED=1), so plain go test runs are credited for changed packages"}, nil)
	})
	// G6b: a go shim first on PATH answers the -race probe with "not
	// supported". Oracle: verify reads its Go facts only from a go command it
	// verified, so that answer never waives -race: a shim that runs the real
	// go for `go env GOROOT` is followed to it, and one that reports its own
	// directory is refused, which a limitation names. A plain run then leaves
	// a's -race problem, unless the real go itself cannot race here.
	for _, ownRoot := range []bool{false, true} {
		t.Run(fmt.Sprintf("B23.e3 a shim's -race answer does not waive -race (own GOROOT %v)", ownRoot), func(t *testing.T) {
			v := vvStart(t, 300, false, "go", "test", "./...")
			v.run(t, true)
			raced := exec.Command("go", "list", "-race", "runtime").Run() == nil
			shim := v.vvWrapper(t, "-race is not supported on plan9/amd64", false, ownRoot)
			ver := v.verify(t)
			var problems []string
			if raced || ownRoot {
				problems = append(problems, vvT2("a"))
			}
			vvWant(t, "B23.e3", ver, problems, nil, []string{"-race cannot run on this host (-race is not supported on plan9/amd64), so plain go test runs are credited for changed packages"})
			refusal := vvPrefixed(ver.Limitations, "go.package-test/v2 could not compute the package graph: cannot verify the go command "+shim+":")
			if ownRoot != (len(refusal) == 1) {
				t.Errorf("B23.e3 violated: limitations refusing %s are %q, want one only when the shim reports its own GOROOT", shim, refusal)
			}
		})
	}
	t.Run("B23.f log cut at the storage limit", func(t *testing.T) {
		v := vvStart(t, 300, false, "go", "test", "-v", "./...")
		t.Setenv("CWV2_BIG", "1")
		v.run(t, true)
		if !v.lastRun(t).Truncated {
			t.Fatal("precondition: the run's log was not truncated")
		}
		vvWant(t, "B23.f/B21/B12", v.verify(t), nil, []string{vvCut}, nil)
		// B21: a stored log over 128 KiB is never read, even if its start matches.
		f, err := os.OpenFile(filepath.Join(v.dir, v.lastRun(t).Log), os.O_APPEND|os.O_WRONLY, 0)
		info, statErr := f.Stat()
		if err != nil || statErr != nil {
			t.Fatal("cannot open the stored log")
		}
		_, err = f.Write(bytes.Repeat([]byte("x"), int(max(1, 128<<10-info.Size()+1))))
		if f.Close() != nil || err != nil {
			t.Fatal("cannot append to the stored log")
		}
		vvWant(t, "B21 (log over 128 KiB)", v.verify(t), nil, []string{vvUnread}, []string{vvCut})
	})
}

// B23.d, B21: a -json run lists its skips from the stored log; a log that is
// not a private regular file, does not match its digest or is missing reads
// as nil, seen as the B12 "could not be read" line with no skip line.
func TestVerifyV2StoredLogB21(t *testing.T) {
	v := vvStart(t, 300, false, "go", "test", "-json", "./...")
	v.run(t, true)
	vvWant(t, "B23.d/B11", v.verify(t), nil, []string{vvSkipped}, []string{vvUnread})
	log := filepath.Join(v.dir, v.lastRun(t).Log)
	data, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	restore := func() {
		t.Helper()
		if os.Remove(log) != nil || os.WriteFile(log, data, 0o600) != nil {
			t.Fatal("cannot restore the stored log")
		}
		vvWant(t, "B21 (log restored)", v.verify(t), nil, []string{vvSkipped}, []string{vvUnread})
	}
	unread := func(step string) {
		t.Helper()
		vvWant(t, "B23.d/B21 ("+step+")", v.verify(t), nil, []string{vvUnread}, []string{vvSkipped})
	}
	if err := os.Chmod(log, 0o644); err != nil {
		t.Fatal(err)
	}
	unread("same bytes, mode 0644")
	restore()
	copied := filepath.Join(t.TempDir(), "log")
	if os.WriteFile(copied, data, 0o600) != nil || os.Remove(log) != nil || os.Symlink(copied, log) != nil {
		t.Fatal("cannot replace the stored log with a symlink")
	}
	unread("symlink to a 0600 copy of the same bytes")
	restore()
	if err := os.WriteFile(log, []byte("other bytes\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	unread("other bytes, mode 0600")
	if err := os.Remove(log); err != nil {
		t.Fatal(err)
	}
	unread("missing")
}

// vvCredited requires the latest pass of plain go test ./... to be a clean
// run under v2: B12 lists it as not verbose (B12 covers clean runs only), no
// B10 line names it, and changed package a has its T1 satisfied.
func vvCredited(t *testing.T, id string, ver taskflow.Verification) {
	t.Helper()
	vvWant(t, id, ver, nil, []string{vvPlain}, []string{vvRerun})
	if slices.Contains(ver.Problems, vvT1("a")) {
		t.Errorf("%s violated: the pass earned no credit: %q", id, ver.Problems)
	}
}

// B22, B23.c: only an earlier failed run with an exit code, at the same
// before-snapshot and recorded GOFLAGS, makes a pass a rerun.
func TestVerifyV2RerunB22(t *testing.T) {
	marker := func(t *testing.T) string {
		m := filepath.Join(t.TempDir(), "fail")
		if err := os.WriteFile(m, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		t.Setenv("CWV2_MARKER", m)
		return m
	}
	t.Run("B22.a/B23.c failure then pass at the same source and GOFLAGS", func(t *testing.T) {
		v := vvStart(t, 300, false, "go", "test", "./...")
		m := marker(t)
		v.run(t, false)
		_ = os.Remove(m)
		v.run(t, true)
		vvWant(t, "B22.a/B23.c/B10", v.verify(t), []string{vvT1("a")}, []string{vvRerun}, nil)
	})
	t.Run("B22.b failure at another snapshot", func(t *testing.T) {
		v := vvStart(t, 300, false, "go", "test", "./...")
		m := marker(t)
		v.run(t, false)
		_ = os.Remove(m)
		v.write(t, "package a\n\nfunc F() int { return 3 }\n")
		v.run(t, true)
		vvCredited(t, "B22.b", v.verify(t))
	})
	t.Run("B22.b failure without a recorded exit code", func(t *testing.T) {
		v := vvStart(t, 300, false, "go", "test", "./...")
		path := os.Getenv("PATH")
		t.Setenv("PATH", t.TempDir()) // go cannot start
		v.run(t, false)
		t.Setenv("PATH", path)
		if rec, err := taskflow.Load(v.dir); err != nil || len(rec.Checks) != 1 || rec.Checks[0].Status != "failed" || rec.Checks[0].ExitCode != nil {
			t.Skipf("cw does not record a check that cannot start as failed without an exit code (%v, %+v); the condition is not reachable here", err, rec.Checks)
		}
		v.run(t, true)
		vvCredited(t, "B22.b", v.verify(t))
	})
	t.Run("B22.b failure under other GOFLAGS", func(t *testing.T) {
		v := vvStart(t, 300, false, "go", "test", "./...")
		m := marker(t)
		t.Setenv("GOFLAGS", "-count=1")
		v.run(t, false)
		_ = os.Remove(m)
		t.Setenv("GOFLAGS", "")
		v.run(t, true)
		vvCredited(t, "B22.b", v.verify(t))
	})
	t.Run("B22.b cancelled and timed-out runs are not failures", func(t *testing.T) {
		v := vvStart(t, 20, false, "go", "test", "./...")
		// Warm the build cache outside cw, so no cw run compiles under the 20 s timeout.
		warm := exec.Command(v.realGo, "test", "./...")
		warm.Dir = v.repo
		if out, err := warm.CombinedOutput(); err != nil {
			t.Fatalf("precondition: warm go test ./...: %v %s", err, out)
		}
		started := filepath.Join(t.TempDir(), "started")
		t.Setenv("CWV2_BLOCK", started)
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() { _, err := v.cw(ctx, "", "task", "run", "--task", v.dir, "--check", "unit"); done <- err }()
		tick, limit := time.NewTicker(20*time.Millisecond), time.After(2*time.Minute) // limit bounds the wait only
		defer tick.Stop()
	wait:
		for {
			select {
			case err := <-done:
				cancel()
				t.Fatalf("precondition: the blocking run ended before it started blocking: %v", err)
			case <-limit:
				cancel()
				<-done //nolint:errcheck // only joins the cancelled run
				t.Fatal("precondition: the blocking run never started")
			case <-tick.C:
				if _, err := os.Stat(started); err == nil {
					break wait
				}
			}
		}
		cancel()
		<-done          //nolint:errcheck // only joins the cancelled run
		v.run(t, false) // CWV2_BLOCK still set: the check times out
		t.Setenv("CWV2_BLOCK", "")
		v.run(t, true)
		rec, err := taskflow.Load(v.dir)
		var statuses []string
		for _, c := range rec.Checks {
			statuses = append(statuses, c.Status)
		}
		if err != nil || !slices.Contains(statuses, "cancelled") || !slices.Contains(statuses, "timed_out") {
			t.Fatalf("precondition: run statuses %q (%v), want cancelled and timed_out", statuses, err)
		}
		vvCredited(t, "B22.b", v.verify(t))
	})
	t.Run("B22.c a commit between failure and pass", func(t *testing.T) {
		if _, err := exec.LookPath("git"); err != nil {
			t.Skip("git is not on PATH")
		}
		v := vvStart(t, 300, true, "go", "test", "./...")
		m := marker(t)
		v.run(t, false)
		_ = os.Remove(m)
		v.git(t, "commit", "-q", "--allow-empty", "-m", "empty")
		v.run(t, true)
		vvCredited(t, "B22.c", v.verify(t))
	})
}
