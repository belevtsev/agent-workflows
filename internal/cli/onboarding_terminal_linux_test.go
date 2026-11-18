package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/belevtsev/kitsu/v3/internal/harness"
	"github.com/belevtsev/kitsu/v3/internal/kitsutest"

	"golang.org/x/sys/unix"
)

func onboardingPTY(t *testing.T) *os.File {
	t.Helper()
	fd, err := unix.Open("/dev/ptmx", unix.O_RDWR|unix.O_NOCTTY|unix.O_CLOEXEC|unix.O_NONBLOCK, 0)
	if err != nil {
		t.Skipf("pseudo terminal unavailable: %v", err)
	}
	master := os.NewFile(uintptr(fd), "onboarding-pty")
	t.Cleanup(func() { _ = master.Close() })
	if err := unix.IoctlSetPointerInt(fd, unix.TIOCSPTLCK, 0); err != nil {
		t.Fatal(err)
	}
	number, err := unix.IoctlGetInt(fd, unix.TIOCGPTN)
	if err != nil {
		t.Fatal(err)
	}
	slave, err := os.OpenFile("/dev/pts/"+strconv.Itoa(number), os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = slave.Close() })
	done := make(chan struct{})
	go func() {
		_, _ = io.Copy(io.Discard, master)
		close(done)
	}()
	t.Cleanup(func() {
		_ = master.Close()
		<-done
	})
	return slave
}

func TestOnboardingInteractiveStartPreservesChildExitTwo(t *testing.T) {
	_, capture := onboardingReadyFixture(t)
	t.Setenv("KITSU_TEST_ONBOARDING_TTY", "1")
	t.Setenv("KITSU_TEST_ONBOARDING_EXIT", "2")
	terminal := onboardingPTY(t)
	err := Execute(t.Context(), []string{"task", "start", "A literal request", "--host", "codex"}, Config{In: terminal, Out: terminal, Err: terminal})
	if ExitCode(err) != 2 || err.Error() != "" {
		t.Fatalf("host's exit 2 was rewritten as command usage: %v", err)
	}
	data, err := os.ReadFile(capture)
	if err != nil || !strings.Contains(string(data), "A literal request") {
		t.Fatalf("interactive start did not pass the request: %s, %v", data, err)
	}
}

func TestInteractiveHarnessPinsScopeAndResumesOriginalTask(t *testing.T) {
	repo := kitsutest.NewRepo(t)
	_, capture := onboardingReadyFixture(t)
	t.Chdir(repo.Dir)
	terminal := onboardingPTY(t)
	cfg := Config{In: terminal, Out: terminal, Err: terminal}
	t.Setenv("KITSU_TEST_ONBOARDING_TTY", "1")
	if err := Execute(t.Context(), []string{"task", "start", "Review current patch", "--until", "review", "--host", "codex", "--review-limit", "3", "--time-limit", "12m", "--finish-reserve", "2m"}, cfg); err != nil {
		t.Fatal(err)
	}
	files, err := filepath.Glob(repo.Path(".git/kitsu-stages/task-harness-*.json"))
	if err != nil || len(files) != 1 {
		t.Fatal(files, err)
	}
	id := harnessRecordName(filepath.Base(files[0]))
	pinned, err := os.ReadFile(files[0])
	var task harness.Task
	if err != nil || json.Unmarshal(pinned, &task) != nil || task.ReviewLimit != 3 || task.FinishReserve != 2*time.Minute || task.Deadline.IsZero() {
		t.Fatal("missing pinned limits", err, string(pinned))
	}
	data, err := os.ReadFile(capture)
	if err != nil || !strings.Contains(string(data), id) {
		t.Fatal("host did not receive pinned ID", err)
	}
	var out bytes.Buffer
	cfg2 := Config{Out: &out, Err: io.Discard}
	if err := Execute(t.Context(), []string{"task", "open", id, "--until", "deliver", "--entry", "implement"}, cfg2); err == nil {
		t.Fatal("host could broaden the stopping point")
	}
	if err := Execute(t.Context(), []string{"task", "open", id, "--until", "review", "--review-limit", "4"}, cfg2); err == nil {
		t.Fatal("scope binding broadened the review limit")
	}
	if err := Execute(t.Context(), []string{"task", "extend", id, "--until", "deliver"}, cfg2); err == nil {
		t.Fatal("extended an unbound outcome")
	}
	if err := Execute(t.Context(), []string{"task", "extend", id, "--review-limit", "4"}, cfg2); err != nil {
		t.Fatal(err)
	}
	if err := Execute(t.Context(), []string{"task", "open", id, "--until", "review"}, cfg2); err != nil {
		t.Fatal(err)
	}
	if err := Execute(t.Context(), []string{"task", "start", "--resume", id, "--host", "codex"}, cfg); err != nil {
		t.Fatal(err)
	}
	files, err = filepath.Glob(repo.Path(".git/kitsu-stages/task-harness-*.json"))
	if err != nil || len(files) != 1 {
		t.Fatal("resume duplicated the task", files, err)
	}
}

func TestInteractiveHarnessPinsCommitBranchBeforeHostLaunch(t *testing.T) {
	repo := kitsutest.Demo(t)
	repo.Git("checkout", "-qb", "delivery")
	repo.Git("branch", "elsewhere")
	_, capture := onboardingReadyFixture(t)
	t.Chdir(repo.Dir)
	terminal := onboardingPTY(t)
	cfg := Config{In: terminal, Out: terminal, Err: terminal}
	t.Setenv("KITSU_TEST_ONBOARDING_TTY", "1")
	const request = "Check the change and commit it on this branch only if it passes."
	if err := Execute(t.Context(), []string{"task", "start", request, "--until", "deliver", "--host", "codex"}, cfg); err != nil {
		t.Fatal(err)
	}
	files, err := filepath.Glob(repo.Path(".git/kitsu-stages/task-harness-*.json"))
	if err != nil || len(files) != 1 {
		t.Fatal(files, err)
	}
	id := harnessRecordName(filepath.Base(files[0]))
	repo.Git("symbolic-ref", "HEAD", "refs/heads/elsewhere")
	var out bytes.Buffer
	jsonCfg := Config{Out: &out, Err: io.Discard}
	open := []string{"task", "open", id, "--entry", "verify", "--until", "deliver", "--commit", "--json"}
	if err := Execute(t.Context(), append(open, "--request", "Repair the change first"), jsonCfg); err == nil {
		t.Fatal("scope binding replaced the original request")
	}
	if err := Execute(t.Context(), open, jsonCfg); err != nil {
		t.Fatal(err)
	}
	var view harnessView
	if err := json.Unmarshal(out.Bytes(), &view); err != nil || view.Next != "blocked" || view.Request != request || view.Scope.Repair || view.Ticket != "" ||
		view.CommitTarget != "refs/heads/delivery" || view.CurrentCommitTarget != "refs/heads/elsewhere" {
		t.Fatalf("startup lost the request or checkout: %v\n%s", err, &out)
	}
	if err := os.Remove(capture); err != nil {
		t.Fatal(err)
	}
	resume := []string{"task", "start", "--resume", id, "--host", "codex"}
	if err := Execute(t.Context(), resume, cfg); err == nil || !strings.Contains(err.Error(), "refs/heads/delivery") {
		t.Fatalf("resume did not identify the original checkout: %v", err)
	}
	if _, err := os.Stat(capture); !os.IsNotExist(err) {
		t.Fatal("resume launched the host from the other checkout", err)
	}
	repo.Git("symbolic-ref", "HEAD", "refs/heads/delivery")
	if err := Execute(t.Context(), resume, cfg); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(capture); err != nil || !strings.Contains(string(data), id) {
		t.Fatalf("resume lost its task: %v, %s", err, data)
	}
}

func TestCompactDailyStartPinsTaskWithoutExplicitOutcome(t *testing.T) {
	repo := kitsutest.NewRepo(t)
	_, capture := onboardingReadyFixture(t)
	t.Chdir(repo.Dir)
	term := onboardingPTY(t)
	t.Setenv("KITSU_TEST_ONBOARDING_TTY", "1")
	if err := Execute(t.Context(), []string{"task", "start", "Plan a CSV export", "--host", "codex", "--compact"}, Config{In: term, Out: term, Err: term}); err != nil {
		t.Fatal(err)
	}
	files, err := filepath.Glob(repo.Path(".git/kitsu-stages/task-harness-*.json"))
	if err != nil || len(files) != 1 {
		t.Fatalf("expected one resumable compact task, got %v, %v", files, err)
	}
	data, err := os.ReadFile(capture)
	if err != nil || !strings.Contains(string(data), harnessRecordName(filepath.Base(files[0]))) {
		t.Fatal("missing handoff identity", err)
	}
}

func TestResumeDuringManagedIntervalDoesNotLaunchDailyHost(t *testing.T) {
	repo := compactChecksFixture(t, "pass")
	open := repo.Kitsu("task", "open", "daily", "--until", "review", "--request", "Review tomorrow's export")
	if open.Code != 0 {
		t.Fatal(open.Output())
	}
	id := stageID(t, stagePrepare(t, repo, "reviewer", "review"))
	for _, check := range [][]string{{"next", "daily", "--brief"}, {"show", "daily", "--json"}} {
		read := repo.Kitsu(append([]string{"task"}, check...)...)
		var view struct {
			Next, Ticket string
			Interval     string `json:"managed_interval"`
			Pending      string `json:"pending_daily_action"`
		}
		data := []byte(read.Stdout)
		if check[0] == "show" {
			var full struct{ Harness json.RawMessage }
			if err := json.Unmarshal(data, &full); err != nil {
				t.Fatal(err)
			}
			data = full.Harness
		}
		if err := json.Unmarshal(data, &view); read.Code != 0 || err != nil || view.Next != "blocked" || view.Interval != id || view.Pending != "intake" || view.Ticket != "" {
			t.Fatal("daily action was not blocked", read.Output())
		}
	}
	_, capture := onboardingReadyFixture(t)
	t.Chdir(repo.Dir)
	term := onboardingPTY(t)
	cfg := Config{In: term, Out: term, Err: term}
	t.Setenv("KITSU_TEST_ONBOARDING_TTY", "1")
	args := []string{"task", "start", "--resume", "daily", "--host", "codex"}
	if err := Execute(t.Context(), args, cfg); err == nil || !strings.Contains(err.Error(), id) {
		t.Fatalf("resume should identify the active interval: %v", err)
	}
	if data, _ := os.ReadFile(capture); len(data) != 0 {
		t.Fatal("launched host for blocked task")
	}
	if end := repo.Kitsu("task", "finish", id); end.Code != 0 {
		t.Fatal(end.Output())
	}
	if err := Execute(t.Context(), args, cfg); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(capture)
	if err != nil || !strings.Contains(string(data), "task next daily --json") {
		t.Fatal("host did not receive the saved task", err)
	}
}
