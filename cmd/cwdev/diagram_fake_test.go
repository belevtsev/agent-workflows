package main

// Black-box pipeline tests for `cwdev diagram-svg` (contract diagram-svg-v1)
// that replace node and Chrome with fakes. The fakes are this test binary:
// TestMain turns it into a fake `node` or a fake browser when it is started
// under one of those names with dsvgFakeDirEnv set. Each fake records how it
// was invoked into the directory named by dsvgFakeDirEnv and behaves as the
// mode environment variables say. Child processes inherit the caller's
// environment (R3), so tests configure the fakes with t.Setenv.
//
// Contract diagram-svg-v4 adds R7 (process cleanup and signals). Its tests
// are at the end of this file. They use helpers that leave the fake's
// process group, helpers that keep creating files until they are killed, and
// the real main run in a child process (dsvgMainEnv).
//
// Contract diagram-svg-v5 adds direct tests of the process-group helpers
// (R8) in process_group_test.go; they, not the end-to-end tests here, guard
// the wait for a killed group. v5 also requires that no deadline case fails
// only because a fake had not started or recorded its helper before a short
// --timeout expired on a loaded runner: see dsvgDeadlineAttempts.
//
// Every record a fake writes is renamed into place (dsvgWriteAtomic), so a
// fake killed at the deadline never leaves a partial record. That includes
// the fake browser's protocol log (proto-*.jsonl): dsvgProtocolLog rewrites
// the whole log under another name and renames it on every command, so its
// strict reader never meets a torn line (v6 test corrections).
//
// Contract diagram-svg-v6 also adds a test of R7's brief retries before
// the removal warning (TestDiagramSVGContractRetriesTemporaryDirectoryRemoval).

import (
	"bufio"
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

const (
	// dsvgFakeDirEnv guards fake mode and names the record directory.
	dsvgFakeDirEnv = "CWDEV_DSVG_FAKE_DIR"
	// dsvgNodeModeEnv selects the fake node behaviour: "" (write the
	// viewer HTML), "fail", "nohtml", "emptyhtml" or "sleep", the receipt
	// modes, "helpersleep", the R7 modes "escapesleep", "escape",
	// "litter" and "lockedtmp", or the v6 mode "retrylitter".
	dsvgNodeModeEnv = "CWDEV_DSVG_FAKE_NODE"
	// dsvgNodeFailSpecEnv makes the fake node fail for the spec with this
	// base name only.
	dsvgNodeFailSpecEnv = "CWDEV_DSVG_FAKE_NODE_FAIL_SPEC"
	// dsvgChromeModeEnv selects the fake browser behaviour: "" (answer with
	// an ok marker), "fail" (exit 5 before reading any command), "sleep"
	// (never read a command), "nomarker", "errormarker", "script",
	// "nobackground", or a protocol mode of dsvgServeCDP, optionally
	// prefixed with "helper" or (R7) "escape" or "litter".
	dsvgChromeModeEnv = "CWDEV_DSVG_FAKE_BROWSER"
	// dsvgChromeFailSpecEnv makes the fake browser fail for the spec with
	// this base name only.
	dsvgChromeFailSpecEnv = "CWDEV_DSVG_FAKE_BROWSER_FAIL_SPEC"
	// dsvgSVGNewlineEnv set to 1 makes the fake export end in "\n".
	dsvgSVGNewlineEnv = "CWDEV_DSVG_FAKE_SVG_NEWLINE"
	// dsvgSentinelEnv proves the children inherit the caller's environment.
	dsvgSentinelEnv = "CWDEV_DSVG_SENTINEL"
	dsvgSentinel    = "inherited-7f3c"

	// dsvgChromeName is the fake browser's file name. It deliberately does
	// not contain "chrome", so an error that mentions chrome does so on
	// its own account.
	dsvgChromeName = "fakebrowser"
	// dsvgNodeTail and dsvgBrowserTail end the fakes' failure output.
	dsvgNodeTail    = "node-tail-ABC-123"
	dsvgBrowserTail = "tail-QRS-911"
	// dsvgViewerError is the message of the fake error marker.
	dsvgViewerError = "viewer-said-NOPE-42"
	// dsvgPageTarget and dsvgSessionID are the fake browser's page target
	// and the session Target.attachToTarget returns.
	dsvgPageTarget = "PAGE-TARGET-7"
	dsvgSessionID  = "SESSION-42"

	// dsvgHelperEnv makes the test binary a long-running helper process:
	// "1" sleeps for a minute, dsvgHelperLitter keeps creating files in
	// dsvgLitterDirEnv for a minute.
	dsvgHelperEnv = "CWDEV_DSVG_HELPER"
	// dsvgHelperLitter and dsvgLitterDirEnv configure the file-creating
	// helper (R7: nothing left in the temporary directory).
	dsvgHelperLitter = "litter"
	dsvgLitterDirEnv = "CWDEV_DSVG_LITTER_DIR"
	// dsvgHelperRetryLitter makes the test binary the helper of the
	// removal-retry test (dsvgRetryLitter): dsvgRetrySlowFiles is how many
	// files its subdirectory holds, dsvgRetryLitterWindow how many files it
	// keeps beside that subdirectory, and dsvgRetryLitterPause how long it
	// pauses between files.
	dsvgHelperRetryLitter = "retrylitter"
	dsvgRetrySlowFiles    = 2000
	dsvgRetryLitterWindow = 200
	dsvgRetryLitterPause  = 100 * time.Microsecond
	// dsvgMainEnv set to 1 makes the test binary run the real main with its
	// own arguments (R7: signals). The fakes and helpers it starts inherit
	// it, so TestMain dispatches them first.
	dsvgMainEnv = "CWDEV_DSVG_MAIN"
	// dsvgEarlyEvaluateError and dsvgLateEvaluateError are the messages of
	// the first and of every later Runtime.evaluate error reply in mode
	// "errorsforever".
	dsvgEarlyEvaluateError = "early-evaluate-error-A"
	dsvgLateEvaluateError  = "late-evaluate-error-B"
	// dsvgFailedStage and the dsvgDiag* values fill the fake finalize
	// receipt.
	dsvgFailedStage    = "layout-check"
	dsvgDiagErrorCode  = "ARCH-E101"
	dsvgDiagErrorText  = "edge e3 overlaps node api"
	dsvgDiagErrorPath  = "$.edges[3]"
	dsvgDiagWarnCode   = "ARCH-W007"
	dsvgDiagWarnText   = "label exceeds 40 characters"
	dsvgDiagnosticJSON = `"diagnostics": [` +
		`{"code": "` + dsvgDiagErrorCode + `", "severity": "error", "message": "` + dsvgDiagErrorText + `", "subject": {"path": "` + dsvgDiagErrorPath + `"}},` +
		`{"code": "` + dsvgDiagWarnCode + `", "severity": "warning", "message": "` + dsvgDiagWarnText + `"}]`
)

func TestMain(m *testing.M) {
	switch os.Getenv(dsvgHelperEnv) {
	case "1":
		// A helper process a fake started: it keeps the inherited
		// stdout/stderr open and runs for a minute unless it is killed.
		time.Sleep(60 * time.Second)
		os.Exit(0)
	case dsvgHelperLitter:
		os.Exit(dsvgLitter())
	case dsvgHelperRetryLitter:
		os.Exit(dsvgRetryLitter())
	}
	if dir := os.Getenv(dsvgFakeDirEnv); dir != "" {
		switch filepath.Base(os.Args[0]) {
		case "node":
			os.Exit(dsvgFakeNode(dir))
		case dsvgChromeName:
			os.Exit(dsvgFakeChrome(dir))
		}
	}
	if os.Getenv(dsvgMainEnv) == "1" {
		// The real cwdev entry point with this process's arguments; main
		// exits on its own when run fails.
		main()
		os.Exit(0)
	}
	code := m.Run()
	if dsvgFakeRoot != "" {
		os.RemoveAll(dsvgFakeRoot)
	}
	os.Exit(code)
}

// dsvgRecord is what a fake records about one invocation.
type dsvgRecord struct {
	Args             []string
	Cwd              string
	ArchifyChrome    string
	ArchifyChromeSet bool
	Sentinel         string
	HTML             string
	InjectedPath     string
	TmpDir           string
	Started          int64
}

// dsvgSpawnHelper starts a helper process in the fake's own process group
// that inherits its stdout and stderr and runs for a minute, and records
// its pid as helper-<pid>.pid in dir.
func dsvgSpawnHelper(dir string) {
	dsvgStartHelper(dir, "helper", "1", nil, true, false)
}

// dsvgStartHelper starts the test binary as a helper process (mode is its
// dsvgHelperEnv value, extraEnv is added to the inherited environment) that
// inherits the fake's stderr, and its stdout too when withStdout is set.
// With escape it moves itself into a new session, and so into a new process
// group that a kill of the fake's group cannot reach. Its pid is recorded
// as <prefix>-<pid>.pid in dir.
func dsvgStartHelper(dir, prefix, mode string, extraEnv []string, withStdout, escape bool) int {
	executable, err := os.Executable()
	if err != nil {
		fmt.Fprintln(os.Stderr, "fake: helper:", err)
		os.Exit(99)
	}
	helper := exec.Command(executable)
	helper.Env = append(append(os.Environ(), dsvgHelperEnv+"="+mode), extraEnv...)
	helper.Stderr = os.Stderr
	if withStdout {
		helper.Stdout = os.Stdout
	}
	if escape {
		helper.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	}
	// Start returns once the helper has executed, so it already holds the
	// inherited descriptors.
	if err := helper.Start(); err != nil {
		fmt.Fprintln(os.Stderr, "fake: helper:", err)
		os.Exit(99)
	}
	pid := helper.Process.Pid
	if err := dsvgWriteAtomic(filepath.Join(dir, fmt.Sprintf("%s-%d.pid", prefix, pid)), []byte(strconv.Itoa(pid))); err != nil {
		fmt.Fprintln(os.Stderr, "fake: helper:", err)
		os.Exit(99)
	}
	return pid
}

// dsvgWriteAtomic writes data to a temporary name beside path and renames
// it to path, so a reader never sees a partial file, even when the writer
// is killed. The temporary name matches none of the records' patterns.
func dsvgWriteAtomic(path string, data []byte) error {
	partial := path + ".partial"
	if err := os.WriteFile(partial, data, 0o644); err != nil {
		return err
	}
	return os.Rename(partial, path)
}

// dsvgStartLitterHelper starts a helper in the fake's own process group
// that keeps creating files in litterDir until it is killed, and returns
// once it has created the first one, so the fake never proceeds before the
// helper is active.
func dsvgStartLitterHelper(dir, litterDir string, withStdout bool) {
	pid := dsvgStartHelper(dir, "helper", dsvgHelperLitter, []string{dsvgLitterDirEnv + "=" + litterDir}, withStdout, false)
	started := filepath.Join(dir, fmt.Sprintf("litter-%d.started", pid))
	// Generous for a loaded runner; the command's deadline kills the fake
	// sooner anyway.
	deadline := time.Now().Add(60 * time.Second)
	for {
		if _, err := os.Stat(started); err == nil {
			return
		}
		if time.Now().After(deadline) {
			fmt.Fprintln(os.Stderr, "fake: the litter helper created no file")
			os.Exit(99)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// dsvgLitter is the file-creating helper: for a minute, or until it is
// killed, it creates a new file in dsvgLitterDirEnv every few milliseconds,
// recreating that directory when it is gone. After its first file it
// records litter-<pid>.started in the record directory.
func dsvgLitter() int {
	dir, records := os.Getenv(dsvgLitterDirEnv), os.Getenv(dsvgFakeDirEnv)
	if dir == "" || records == "" {
		fmt.Fprintln(os.Stderr, "litter helper: no directory")
		return 99
	}
	reported := false
	for end := time.Now().Add(60 * time.Second); time.Now().Before(end); time.Sleep(5 * time.Millisecond) {
		if os.MkdirAll(dir, 0o755) != nil {
			continue
		}
		file, err := os.CreateTemp(dir, "litter-")
		if err != nil {
			continue
		}
		file.Close()
		if !reported {
			reported = true
			dsvgWriteAtomic(filepath.Join(records, fmt.Sprintf("litter-%d.started", os.Getpid())), nil)
		}
	}
	return 0
}

// dsvgStartRetryLitterHelper starts the removal-retry helper
// (dsvgRetryLitter) creating files in litterDir. It runs in a new session,
// so no kill of the fake's process group reaches it, and its standard
// streams are /dev/null, so it holds none of the fake's output. Its pid is
// recorded as escaped-<pid>.pid in dir. It returns once the helper has made
// its subdirectory and is about to create files beside it.
func dsvgStartRetryLitterHelper(dir, litterDir string) {
	executable, err := os.Executable()
	if err != nil {
		fmt.Fprintln(os.Stderr, "fake: helper:", err)
		os.Exit(99)
	}
	helper := exec.Command(executable)
	helper.Env = append(os.Environ(), dsvgHelperEnv+"="+dsvgHelperRetryLitter, dsvgLitterDirEnv+"="+litterDir)
	helper.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := helper.Start(); err != nil {
		fmt.Fprintln(os.Stderr, "fake: helper:", err)
		os.Exit(99)
	}
	pid := helper.Process.Pid
	if err := dsvgWriteAtomic(filepath.Join(dir, fmt.Sprintf("escaped-%d.pid", pid)), []byte(strconv.Itoa(pid))); err != nil {
		fmt.Fprintln(os.Stderr, "fake: helper:", err)
		os.Exit(99)
	}
	started := filepath.Join(dir, fmt.Sprintf("retrylitter-%d.started", pid))
	// Generous for a loaded runner; the command's deadline kills the fake
	// sooner anyway.
	for deadline := time.Now().Add(60 * time.Second); ; time.Sleep(5 * time.Millisecond) {
		if _, err := os.Stat(started); err == nil {
			return
		}
		if time.Now().After(deadline) {
			fmt.Fprintln(os.Stderr, "fake: the retry helper did not finish its subdirectory")
			os.Exit(99)
		}
	}
}

// dsvgRetryLitter is the removal-retry helper. In dsvgLitterDirEnv it
// first makes a subdirectory holding dsvgRetrySlowFiles files, which keeps
// a remover busy inside that subdirectory for a while, and records the time
// it finished that as retrylitter-<pid>.started in the record directory.
// Then every dsvgRetryLitterPause, while that subdirectory exists, it
// creates one more file beside it, keeping at most dsvgRetryLitterWindow of
// them (it removes its own oldest). A remover that listed the directory before descending
// into the subdirectory finds new files there when it tries to remove the
// directory itself. The helper never recreates anything it did not make.
// As soon as the subdirectory is gone (the command's removal got through
// it) or a file cannot be created (the directory is gone), it stops and
// records why in retrylitter-<pid>.stopped. So it creates at most one file
// after the command's first removal pass removed the subdirectory: an
// attempt that fails because of the helper is followed by at most one more
// that can, and a later one succeeds. It gives up after a minute, longer
// than the dsvgDeadlineRetry deadline.
func dsvgRetryLitter() int {
	dir, records := os.Getenv(dsvgLitterDirEnv), os.Getenv(dsvgFakeDirEnv)
	if dir == "" || records == "" {
		fmt.Fprintln(os.Stderr, "retry helper: no directory")
		return 99
	}
	stop := func(reason string) int {
		dsvgWriteAtomic(filepath.Join(records, fmt.Sprintf("retrylitter-%d.stopped", os.Getpid())), []byte(reason))
		return 0
	}
	slow := filepath.Join(dir, "retry-slow")
	if err := os.Mkdir(slow, 0o755); err != nil {
		fmt.Fprintln(os.Stderr, "retry helper:", err)
		return 99
	}
	for i := range dsvgRetrySlowFiles {
		if err := os.WriteFile(filepath.Join(slow, strconv.Itoa(i)), nil, 0o644); err != nil {
			fmt.Fprintln(os.Stderr, "retry helper:", err)
			return 99
		}
	}
	dsvgWriteTime(records, fmt.Sprintf("retrylitter-%d.started", os.Getpid()))
	var kept []string
	for end := time.Now().Add(60 * time.Second); time.Now().Before(end); time.Sleep(dsvgRetryLitterPause) {
		if _, err := os.Lstat(slow); err != nil {
			return stop(dsvgRetryStoppedRemoval)
		}
		if len(kept) >= dsvgRetryLitterWindow {
			os.Remove(kept[0])
			kept = kept[1:]
		}
		file, err := os.CreateTemp(dir, "litter-")
		if err != nil {
			return stop(dsvgRetryStoppedGone)
		}
		file.Close()
		kept = append(kept, file.Name())
	}
	return stop("time limit")
}

// The reasons dsvgRetryLitter records for stopping because the command
// removed (or was removing) its directory.
const (
	dsvgRetryStoppedRemoval = "subdirectory removed"
	dsvgRetryStoppedGone    = "directory gone"
)

// dsvgWriteTime records the current time as name in dir.
func dsvgWriteTime(dir, name string) {
	dsvgWriteAtomic(filepath.Join(dir, name), []byte(strconv.FormatInt(time.Now().UnixNano(), 10)))
}

// dsvgReadTime returns the time recorded in the one file of dir matching
// pattern.
func dsvgReadTime(t *testing.T, dir, pattern string) time.Time {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(dir, pattern))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 1 {
		t.Fatalf("%d files match %s, want 1", len(matches), pattern)
	}
	data, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatal(err)
	}
	ns, err := strconv.ParseInt(string(data), 10, 64)
	if err != nil {
		t.Fatalf("%s: %v", matches[0], err)
	}
	return time.Unix(0, ns)
}

// dsvgRecordedPids returns the pids recorded as <prefix>-<pid>.pid in dir.
func dsvgRecordedPids(dir, prefix string) []int {
	matches, _ := filepath.Glob(filepath.Join(dir, prefix+"-*.pid"))
	var pids []int
	for _, match := range matches {
		data, err := os.ReadFile(match)
		if err != nil {
			continue
		}
		if pid, err := strconv.Atoi(string(data)); err == nil && pid > 0 {
			pids = append(pids, pid)
		}
	}
	return pids
}

// dsvgFakePids returns the pids of the fake node and browser processes
// recorded in dir (records are named <kind>-<started>-<pid>.json).
func dsvgFakePids(dir string) []int {
	var pids []int
	for _, kind := range []string{"node", "chrome"} {
		matches, _ := filepath.Glob(filepath.Join(dir, kind+"-*.json"))
		for _, match := range matches {
			name := strings.TrimSuffix(filepath.Base(match), ".json")
			if pid, err := strconv.Atoi(name[strings.LastIndexByte(name, '-')+1:]); err == nil && pid > 0 {
				pids = append(pids, pid)
			}
		}
	}
	return pids
}

// dsvgKillLeftovers kills every fake (with its process group) and every
// helper recorded in dir that is still running, so no test leaves a process
// behind even when it fails.
func dsvgKillLeftovers(dir string) {
	for _, pid := range dsvgFakePids(dir) {
		if dsvgProcessAlive(pid) {
			// A live fake that leads a process group takes the group
			// with it; otherwise the first call fails harmlessly.
			syscall.Kill(-pid, syscall.SIGKILL)
			syscall.Kill(pid, syscall.SIGKILL)
		}
	}
	for _, prefix := range []string{"helper", "escaped"} {
		for _, pid := range dsvgRecordedPids(dir, prefix) {
			if dsvgProcessAlive(pid) {
				syscall.Kill(pid, syscall.SIGKILL)
			}
		}
	}
}

// dsvgProcessAlive reports whether pid names a live (not zombie) process.
func dsvgProcessAlive(pid int) bool {
	if stat, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid)); err == nil {
		if i := bytes.LastIndexByte(stat, ')'); i >= 0 && i+2 < len(stat) && stat[i+2] == 'Z' {
			return false
		}
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return process.Signal(syscall.Signal(0)) == nil
}

// dsvgAssertHelpersGone checks that the fakes started at least one helper
// and that every helper is gone within a few seconds of run returning
// (R3: the process group is killed).
func dsvgAssertHelpersGone(t *testing.T, dir string) {
	t.Helper()
	if matches, err := filepath.Glob(filepath.Join(dir, "helper-*.pid")); err != nil {
		t.Fatal(err)
	} else if len(matches) == 0 {
		t.Fatalf("the fake started no helper process")
	}
	dsvgAssertRecordedHelpersGone(t, dir)
}

// dsvgAssertRecordedHelpersGone checks that every helper recorded in dir,
// if any, is gone within a few seconds of run returning (R3: the process
// group is killed). It serves attempts whose deadline came before the fake
// recorded its helper.
func dsvgAssertRecordedHelpersGone(t *testing.T, dir string) {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(dir, "helper-*.pid"))
	if err != nil {
		t.Fatal(err)
	}
	for _, match := range matches {
		data, err := os.ReadFile(match)
		if err != nil {
			t.Fatal(err)
		}
		pid, err := strconv.Atoi(string(data))
		if err != nil {
			t.Fatal(err)
		}
		deadline := time.Now().Add(5 * time.Second)
		for dsvgProcessAlive(pid) && time.Now().Before(deadline) {
			time.Sleep(50 * time.Millisecond)
		}
		if dsvgProcessAlive(pid) {
			t.Errorf("helper process %d is still running; its process group was not killed", pid)
			if process, err := os.FindProcess(pid); err == nil {
				process.Kill()
			}
		}
	}
}

func dsvgWriteRecord(dir, kind string, record dsvgRecord) {
	data, err := json.Marshal(record)
	if err != nil {
		fmt.Fprintln(os.Stderr, "fake: encode record:", err)
		os.Exit(95)
	}
	name := filepath.Join(dir, fmt.Sprintf("%s-%d-%d.json", kind, record.Started, os.Getpid()))
	if err := dsvgWriteAtomic(name, data); err != nil {
		fmt.Fprintln(os.Stderr, "fake: write record:", err)
		os.Exit(96)
	}
}

// dsvgReadRecords returns the records of one kind ("node" or "chrome") in
// the order the fakes started.
func dsvgReadRecords(t *testing.T, dir, kind string) []dsvgRecord {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(dir, kind+"-*.json"))
	if err != nil {
		t.Fatal(err)
	}
	var records []dsvgRecord
	for _, match := range matches {
		data, err := os.ReadFile(match)
		if err != nil {
			t.Fatal(err)
		}
		var record dsvgRecord
		if err := json.Unmarshal(data, &record); err != nil {
			t.Fatalf("decode %s: %v", match, err)
		}
		records = append(records, record)
	}
	slices.SortFunc(records, func(a, b dsvgRecord) int { return cmp.Compare(a.Started, b.Started) })
	return records
}

// dsvgFakeViewer is the HTML the fake finalize writes. The earlier
// "</body>" inside a script string makes "the last </body>" matter.
func dsvgFakeViewer(name, diagramType string) []byte {
	return []byte("<!DOCTYPE html>\n<html><head><meta charset=\"utf-8\"><title>fake viewer</title></head>\n" +
		"<body><main data-spec=\"" + name + "\" data-type=\"" + diagramType + "\">fake viewer \u00e9</main>\n" +
		"<script>var closing = \"</body>\";</script>\n</body>\n</html>\n")
}

// dsvgFakeSVG is the export the fake browser reports for a spec. It passes
// validateExport and does not end in a newline.
func dsvgFakeSVG(name string) []byte {
	return []byte(`<?xml version="1.0" encoding="UTF-8"?>` + "\n" +
		`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 120 40">` +
		`<style>.c-bg-rect{fill:#fff}@media (prefers-color-scheme: dark){.c-bg-rect{fill:#111}}</style>` +
		`<rect class="c-bg-rect" width="120" height="40"/><text x="4" y="20">` + name + `</text></svg>`)
}

// dsvgSpecName returns the data-spec attribute of a fake viewer page.
func dsvgSpecName(page []byte) string {
	const attribute = `data-spec="`
	start := bytes.Index(page, []byte(attribute))
	if start < 0 {
		return ""
	}
	rest := page[start+len(attribute):]
	end := bytes.IndexByte(rest, '"')
	if end < 0 {
		return ""
	}
	return string(rest[:end])
}

// dsvgInsertBeforeLastBody inserts text before the last "</body>" (any
// case), as the capture script's appended element appears in a dumped DOM.
func dsvgInsertBeforeLastBody(page []byte, text string) []byte {
	i := dsvgLastBodyIndex(page)
	if i < 0 {
		i = len(page)
	}
	out := append([]byte{}, page[:i]...)
	out = append(out, text...)
	return append(out, page[i:]...)
}

func dsvgFakeNode(dir string) int {
	args := os.Args[1:]
	cwd, _ := os.Getwd()
	chrome, chromeSet := os.LookupEnv("ARCHIFY_CHROME")
	record := dsvgRecord{Args: args, Cwd: cwd, ArchifyChrome: chrome, ArchifyChromeSet: chromeSet, Sentinel: os.Getenv(dsvgSentinelEnv), TmpDir: os.Getenv("TMPDIR"), Started: time.Now().UnixNano()}
	if len(args) < 5 || args[1] != "finalize" {
		dsvgWriteRecord(dir, "node", record)
		fmt.Fprintf(os.Stderr, "fake node: unexpected arguments %q\n", args)
		return 90
	}
	spec, html := args[3], args[4]
	mode := os.Getenv(dsvgNodeModeEnv)
	if failing := os.Getenv(dsvgNodeFailSpecEnv); failing != "" && filepath.Base(spec) == failing {
		mode = "fail"
	}
	dsvgWriteRecord(dir, "node", record)
	receipt := strings.TrimSuffix(html, ".html") + ".finalize.json"
	switch mode {
	case "fail", "failreceipt", "failbadreceipt", "failnostage":
		switch mode {
		case "failreceipt":
			dsvgWriteFakeReceipt(receipt, `{"ok": false, "failedStage": "`+dsvgFailedStage+`", `+dsvgDiagnosticJSON+`}`)
		case "failbadreceipt":
			dsvgWriteFakeReceipt(receipt, `{"failedStage": "`+dsvgFailedStage+`", "diagnostics": [`)
		case "failnostage":
			dsvgWriteFakeReceipt(receipt, `{"ok": false, "failedStage": "", `+dsvgDiagnosticJSON+`}`)
		}
		for i := range 200 {
			fmt.Fprintf(os.Stdout, "progress line %d\n", i)
		}
		fmt.Fprintln(os.Stdout, "fatal: "+dsvgNodeTail)
		fmt.Fprintln(os.Stderr, "fatal: "+dsvgNodeTail)
		return 7
	case "helpersleep":
		dsvgSpawnHelper(dir)
		time.Sleep(60 * time.Second)
		return 0
	case "escapesleep":
		// R7: a helper outside node's group holds node's stdout and
		// stderr for a minute; node itself never finishes.
		dsvgStartHelper(dir, "escaped", "1", nil, true, true)
		time.Sleep(60 * time.Second)
		return 0
	case "escape":
		// R7: the same helper, but node succeeds (below) and records when
		// it finished.
		dsvgStartHelper(dir, "escaped", "1", nil, true, true)
	case "litter":
		// R7: a helper in node's group keeps creating files in TMPDIR
		// until it is killed; node never finishes.
		dsvgStartLitterHelper(dir, os.Getenv("TMPDIR"), true)
		time.Sleep(60 * time.Second)
		return 0
	case "retrylitter":
		// R7 removal retry (v6): a helper outside every process group,
		// holding none of node's descriptors, keeps creating files in
		// TMPDIR (the command's temporary directory) until the command's
		// removal gets through its subdirectory; node never finishes.
		dsvgStartRetryLitterHelper(dir, os.Getenv("TMPDIR"))
		time.Sleep(60 * time.Second)
		return 0
	case "lockedtmp":
		// R7: a directory in TMPDIR whose entry cannot be removed (no
		// write permission on it); node then succeeds as usual.
		locked := filepath.Join(os.Getenv("TMPDIR"), "locked")
		if err := os.MkdirAll(locked, 0o755); err != nil {
			fmt.Fprintln(os.Stderr, "fake node:", err)
			return 92
		}
		if err := os.WriteFile(filepath.Join(locked, "pinned"), []byte("pinned\n"), 0o644); err != nil {
			fmt.Fprintln(os.Stderr, "fake node:", err)
			return 92
		}
		if err := os.Chmod(locked, 0o500); err != nil {
			fmt.Fprintln(os.Stderr, "fake node:", err)
			return 92
		}
	case "nohtml":
		return 0
	case "emptyhtml":
		if err := os.WriteFile(html, nil, 0o644); err != nil {
			fmt.Fprintln(os.Stderr, "fake node:", err)
			return 92
		}
		return 0
	case "sleep":
		time.Sleep(60 * time.Second)
		return 0
	}
	// Like archify, leave a scratch file in TMPDIR; R3 sets TMPDIR to the
	// command's temporary directory so it is removed with it.
	if scratch, err := os.CreateTemp("", "archify-scratch-"); err == nil {
		scratch.Close()
	}
	name := strings.TrimSuffix(filepath.Base(spec), ".json")
	if err := os.WriteFile(html, dsvgFakeViewer(name, args[2]), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "fake node:", err)
		return 92
	}
	if mode == "escape" {
		dsvgWriteTime(dir, fmt.Sprintf("nodedone-%d.time", os.Getpid()))
	}
	return 0
}

func dsvgWriteFakeReceipt(path, content string) {
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "fake node: receipt:", err)
		os.Exit(92)
	}
}

func dsvgFakeLoadPage(args []string) ([]byte, string, error) {
	if len(args) == 0 {
		return nil, "", errors.New("no arguments")
	}
	last := args[len(args)-1]
	if !strings.HasPrefix(last, "file://") {
		return nil, "", fmt.Errorf("last argument %q is not a file:// URL", last)
	}
	parsed, err := url.Parse(last)
	if err != nil {
		return nil, "", err
	}
	page, err := os.ReadFile(parsed.Path)
	return page, parsed.Path, err
}

func dsvgFakeChrome(dir string) int {
	args := os.Args[1:]
	record := dsvgRecord{Args: args, Sentinel: os.Getenv(dsvgSentinelEnv), Started: time.Now().UnixNano()}
	page, path, err := dsvgFakeLoadPage(args)
	if err != nil {
		dsvgWriteRecord(dir, "chrome", record)
		fmt.Fprintln(os.Stderr, "fake browser:", err)
		return 93
	}
	record.InjectedPath, record.HTML = path, string(page)
	name := dsvgSpecName(page)
	mode := os.Getenv(dsvgChromeModeEnv)
	if failing := os.Getenv(dsvgChromeFailSpecEnv); failing != "" && name+".json" == failing {
		mode = "fail"
	}
	dsvgWriteRecord(dir, "chrome", record)
	fmt.Fprintln(os.Stderr, "[fake] devtools noise on stderr")
	if rest, ok := strings.CutPrefix(mode, "escape"); ok {
		// R7 "escape" and "escapeblank": a helper in a new session that
		// holds the inherited stderr (only stderr) for a minute, then
		// behave as "" or "blank". The pipe descriptors stay with the
		// browser.
		syscall.CloseOnExec(3)
		syscall.CloseOnExec(4)
		dsvgStartHelper(dir, "escaped", "1", nil, false, true)
		mode = rest
	}
	if rest, ok := strings.CutPrefix(mode, "litter"); ok {
		// R7 "litter" and "litterblank": a helper in the browser's group
		// that keeps creating files in the user-data-dir (inside the
		// command's temporary directory) until it is killed.
		userDataDir, found := dsvgFlagValue(args, "--user-data-dir=")
		if !found || userDataDir == "" {
			fmt.Fprintln(os.Stderr, "fake browser: no --user-data-dir")
			return 93
		}
		syscall.CloseOnExec(3)
		syscall.CloseOnExec(4)
		dsvgStartLitterHelper(dir, userDataDir, false)
		mode = rest
	}
	if rest, ok := strings.CutPrefix(mode, "helper"); ok {
		// "helper" and "helperblank": start a long-running helper that
		// inherits stderr, then behave as "" or "blank".
		dsvgSpawnHelper(dir)
		mode = rest
	}
	if mode == "errorsforever" {
		dsvgBrowserTailText()
	}
	// Decoy: a dumped DOM carrying a different export on stdout. The marker
	// must come from the Runtime.evaluate reply, never from stdout (R3).
	os.Stdout.Write(dsvgInsertBeforeLastBody(page, dsvgMarker("ok", dsvgFakeSVG("decoy-from-stdout"))))
	svg := dsvgFakeSVG(name)
	if os.Getenv(dsvgSVGNewlineEnv) == "1" {
		svg = append(svg, '\n')
	}
	value := dsvgMarker("ok", svg)
	switch mode {
	case "fail":
		dsvgBrowserTailText()
		return 5
	case "sleep":
		time.Sleep(60 * time.Second)
		return 0
	case "nomarker":
		value = "<p>the page returned text without a capture marker</p>"
	case "errormarker":
		value = dsvgMarker("error", []byte(dsvgViewerError))
	case "script":
		value = dsvgMarker("ok", bytes.Replace(svg, []byte("</svg>"), []byte("<script>alert(1)</script></svg>"), 1))
	case "nobackground":
		value = dsvgMarker("ok", bytes.ReplaceAll(svg, []byte("c-bg-rect"), []byte("plain-rect")))
	}
	commands, replies := os.NewFile(3, "cdp-commands"), os.NewFile(4, "cdp-replies")
	if commands == nil || replies == nil {
		fmt.Fprintln(os.Stderr, "fake browser: descriptors 3 and 4 are missing")
		return 94
	}
	log := &dsvgProtocolLog{path: filepath.Join(dir, fmt.Sprintf("proto-%d-%d.jsonl", record.Started, os.Getpid()))}
	// An empty log first, so a browser that receives no command still
	// counts as one protocol session.
	if _, err := log.Write(nil); err != nil {
		fmt.Fprintln(os.Stderr, "fake browser:", err)
		return 96
	}
	dsvgMarkerHook = func() { dsvgWriteTime(dir, fmt.Sprintf("marker-%d.time", os.Getpid())) }
	return dsvgServeCDP(commands, replies, log, mode, value)
}

// dsvgProtocolLog is the fake browser's protocol log at path: one JSON line
// per command received. Each Write appends to the lines kept in memory and
// writes them all with dsvgWriteAtomic (another name, then a rename), so
// the file on disk always holds whole lines, even when the browser is
// killed during a write.
type dsvgProtocolLog struct {
	path  string
	lines []byte
}

func (l *dsvgProtocolLog) Write(data []byte) (int, error) {
	l.lines = append(l.lines, data...)
	if err := dsvgWriteAtomic(l.path, l.lines); err != nil {
		return 0, err
	}
	return len(data), nil
}

// dsvgMarkerHook, when set in a fake browser, runs just before
// dsvgServeCDP sends a Runtime.evaluate reply carrying a non-empty string
// (the marker), so R7 tests can measure from the marker.
var dsvgMarkerHook func()

// dsvgBrowserTailText writes the fake browser's failure output to stderr.
func dsvgBrowserTailText() {
	for i := range 200 {
		fmt.Fprintf(os.Stderr, "browser log line %d\n", i)
	}
	fmt.Fprintln(os.Stderr, "fatal: "+dsvgBrowserTail)
}

// dsvgCommand is one DevTools command read from descriptor 3.
type dsvgCommand struct {
	ID        int64          `json:"id"`
	Method    string         `json:"method"`
	Params    map[string]any `json:"params,omitempty"`
	SessionID string         `json:"sessionId,omitempty"`
}

// dsvgEOFMethod is logged when descriptor 3 reaches end of file.
const dsvgEOFMethod = "<eof>"

// dsvgServeCDP answers DevTools pipe commands (NUL-terminated JSON) as a
// headless browser with one page target would. Modes:
//   - "" and the export-content modes: answer every command once and exit
//     after answering Browser.close;
//   - "retry": no page target in the first two Target.getTargets replies,
//     then Runtime.evaluate answers with an error, "", 42 and no value
//     before the marker, with events in between;
//   - "blank": Runtime.evaluate always returns "";
//   - "errorsforever": Runtime.evaluate always gets an error reply, the
//     first with dsvgEarlyEvaluateError, later ones with
//     dsvgLateEvaluateError (stderr already holds the failure tail);
//   - "exitbeforemarker": exit 0 on the first Runtime.evaluate, unanswered;
//   - "closepipe": close descriptor 4 on the first Runtime.evaluate, then
//     exit shortly after;
//   - "linger": do not answer Browser.close; exit when descriptor 3 closes;
//   - "stubborn": ignore Browser.close and descriptor 3; sleep a minute.
func dsvgServeCDP(commands io.Reader, replies *os.File, log io.Writer, mode, value string) int {
	reader := bufio.NewReader(commands)
	send := func(message map[string]any) {
		data, err := json.Marshal(message)
		if err != nil {
			fmt.Fprintln(os.Stderr, "fake browser:", err)
			os.Exit(97)
		}
		replies.Write(append(data, 0))
	}
	logCommand := func(command dsvgCommand) {
		data, _ := json.Marshal(command)
		log.Write(append(data, '\n'))
	}
	event := func(method string) {
		send(map[string]any{"method": method, "params": map[string]any{"note": "unsolicited event"}})
	}
	event("Target.targetCreated")
	targets, evaluations := 0, 0
	for {
		raw, err := reader.ReadBytes(0)
		if err != nil {
			logCommand(dsvgCommand{Method: dsvgEOFMethod})
			return 0
		}
		var command dsvgCommand
		if err := json.Unmarshal(bytes.TrimSuffix(raw, []byte{0}), &command); err != nil {
			fmt.Fprintf(os.Stderr, "fake browser: command %q is not one JSON object: %v\n", raw, err)
			return 98
		}
		logCommand(command)
		reply := map[string]any{"id": command.ID}
		switch command.Method {
		case "Target.getTargets":
			targets++
			infos := []any{map[string]any{"targetId": "BROWSER-TARGET-1", "type": "browser", "url": ""}}
			if mode != "retry" || targets > 2 {
				infos = append(infos, map[string]any{"targetId": dsvgPageTarget, "type": "page", "url": "file:///fake/page.html"})
			}
			reply["result"] = map[string]any{"targetInfos": infos}
		case "Target.attachToTarget":
			reply["result"] = map[string]any{"sessionId": dsvgSessionID}
		case "Runtime.evaluate":
			evaluations++
			switch mode {
			case "exitbeforemarker":
				dsvgBrowserTailText()
				return 0
			case "closepipe":
				dsvgBrowserTailText()
				replies.Close()
				time.Sleep(300 * time.Millisecond)
				return 6
			}
			result := map[string]any{"type": "string", "value": value}
			switch {
			case mode == "blank":
				result["value"] = ""
			case mode == "errorsforever" && evaluations == 1:
				reply["error"] = map[string]any{"code": -32000, "message": dsvgEarlyEvaluateError}
			case mode == "errorsforever":
				reply["error"] = map[string]any{"code": -32000, "message": dsvgLateEvaluateError}
			case mode == "retry" && evaluations == 1:
				reply["error"] = map[string]any{"code": -32000, "message": "Execution context was destroyed."}
			case mode == "retry" && evaluations == 2:
				result["value"] = ""
			case mode == "retry" && evaluations == 3:
				result = map[string]any{"type": "number", "value": 42}
			case mode == "retry" && evaluations == 4:
				result = map[string]any{"type": "undefined"}
			}
			if _, failed := reply["error"]; !failed {
				reply["result"] = map[string]any{"result": result}
				if text, ok := result["value"].(string); ok && text != "" && dsvgMarkerHook != nil {
					dsvgMarkerHook()
				}
			}
		case "Browser.close":
			switch mode {
			case "stubborn":
				time.Sleep(60 * time.Second)
				return 0
			case "linger":
				continue
			}
			reply["result"] = map[string]any{}
			send(reply)
			return 0
		default:
			reply["result"] = map[string]any{}
		}
		if mode == "retry" {
			event("Runtime.consoleAPICalled")
		}
		send(reply)
	}
}

// dsvgReadProtocol returns the commands each fake browser process received,
// one slice per process in start order.
func dsvgReadProtocol(t *testing.T, dir string) [][]dsvgCommand {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(dir, "proto-*.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(matches)
	var sessions [][]dsvgCommand
	for _, match := range matches {
		data, err := os.ReadFile(match)
		if err != nil {
			t.Fatal(err)
		}
		var session []dsvgCommand
		for line := range strings.SplitSeq(string(data), "\n") {
			if line == "" {
				continue
			}
			var command dsvgCommand
			if err := json.Unmarshal([]byte(line), &command); err != nil {
				t.Fatalf("decode %s: %v", match, err)
			}
			session = append(session, command)
		}
		sessions = append(sessions, session)
	}
	return sessions
}

var (
	dsvgFakeOnce sync.Once
	dsvgFakeRoot string
	dsvgFakeErr  error
)

// dsvgFakeBinaries returns the directory holding the fake `node` and the
// path of the fake browser, creating them once per test process.
func dsvgFakeBinaries(t *testing.T) (nodeDir, chrome string) {
	t.Helper()
	dsvgFakeOnce.Do(func() { dsvgFakeRoot, dsvgFakeErr = dsvgBuildFakeBinaries() })
	if dsvgFakeErr != nil {
		t.Fatalf("prepare fake executables: %v", dsvgFakeErr)
	}
	return filepath.Join(dsvgFakeRoot, "runtime"), filepath.Join(dsvgFakeRoot, "browser", dsvgChromeName)
}

func dsvgBuildFakeBinaries() (string, error) {
	executable, err := os.Executable()
	if err != nil {
		return "", err
	}
	root, err := os.MkdirTemp("", "cwdev-dsvg-fakes-")
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	}
	for _, target := range []string{filepath.Join(root, "runtime", "node"), filepath.Join(root, "browser", dsvgChromeName)} {
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return root, err
		}
		if err := dsvgCopyExecutable(executable, target); err != nil {
			return root, err
		}
	}
	return root, nil
}

func dsvgCopyExecutable(source, target string) error {
	if os.Link(source, target) == nil {
		return nil
	}
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// dsvgEnv is one isolated fake environment: a repository root with the
// archify CLI script and docs/diagrams, a record directory, and a private
// TMPDIR.
type dsvgEnv struct {
	root, diagrams, script, records, tmp, nodeDir, chrome string
}

// dsvgTempDir returns a fresh directory with symbolic links resolved, so
// paths derived by the command and by the test compare as strings.
func dsvgTempDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

func dsvgUnsetenv(t *testing.T, key string) {
	t.Helper()
	t.Setenv(key, "")
	os.Unsetenv(key)
}

// dsvgSetup prepares a fake environment: node on PATH is the fake node and
// nothing else is on PATH, ARCHIFY_CHROME is the fake browser,
// ARCHIFY_CHROME_NO_SANDBOX is unset and TMPDIR is a fresh empty directory.
func dsvgSetup(t *testing.T) *dsvgEnv {
	t.Helper()
	nodeDir, chrome := dsvgFakeBinaries(t)
	// Every t.TempDir call happens before TMPDIR changes; later calls reuse
	// the test's own temporary parent directory.
	e := &dsvgEnv{root: dsvgTempDir(t), records: dsvgTempDir(t), tmp: dsvgTempDir(t), nodeDir: nodeDir, chrome: chrome}
	e.diagrams = filepath.Join(e.root, "docs", "diagrams")
	e.script = filepath.Join(e.root, "third_party", "archify", "bin", "archify.mjs")
	for _, dir := range []string{filepath.Dir(e.script), e.diagrams} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(e.script, []byte("// fake archify CLI; the fake node never reads it\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Registered after the record directory, so it runs before that
	// directory is removed.
	t.Cleanup(func() { dsvgKillLeftovers(e.records) })
	t.Setenv(dsvgFakeDirEnv, e.records)
	for _, name := range []string{dsvgNodeModeEnv, dsvgNodeFailSpecEnv, dsvgChromeModeEnv, dsvgChromeFailSpecEnv, dsvgSVGNewlineEnv} {
		t.Setenv(name, "")
	}
	t.Setenv(dsvgSentinelEnv, dsvgSentinel)
	t.Setenv("PATH", nodeDir)
	t.Setenv("ARCHIFY_CHROME", chrome)
	dsvgUnsetenv(t, "ARCHIFY_CHROME_NO_SANDBOX")
	t.Setenv("TMPDIR", e.tmp)
	return e
}

func dsvgWriteSpec(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func dsvgWriteFile(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}

// dsvgRun runs `cwdev diagram-svg args...` and returns what it printed.
func dsvgRun(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var err error
	output := captureOutput(t, func() {
		err = run(t.Context(), append([]string{"diagram-svg"}, args...))
	})
	return output, err
}

// dsvgSnapshot maps every file below dir to its content and every directory
// to "" (with a trailing slash in its key). A missing dir is empty.
func dsvgSnapshot(t *testing.T, dir string) map[string]string {
	t.Helper()
	snapshot := map[string]string{}
	err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			if path == dir && errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			return err
		}
		if path == dir {
			return nil
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		if entry.IsDir() {
			snapshot[rel+"/"] = ""
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		snapshot[rel] = string(data)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func dsvgAssertSnapshot(t *testing.T, what string, want, got map[string]string) {
	t.Helper()
	if maps.Equal(want, got) {
		return
	}
	for _, key := range slices.Sorted(maps.Keys(got)) {
		if before, ok := want[key]; !ok {
			t.Errorf("%s: unexpected file %s", what, key)
		} else if before != got[key] {
			t.Errorf("%s: %s changed from %q to %q", what, key, before, got[key])
		}
	}
	for _, key := range slices.Sorted(maps.Keys(want)) {
		if _, ok := got[key]; !ok {
			t.Errorf("%s: %s disappeared", what, key)
		}
	}
}

func dsvgAssertFile(t *testing.T, path, want string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if string(data) != want {
		t.Fatalf("%s:\n got %q\nwant %q", path, data, want)
	}
}

func dsvgAssertMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Errorf("%s: mode %v, want %v", path, got, want)
	}
}

func dsvgAssertMissing(t *testing.T, what, path string) {
	t.Helper()
	if _, err := os.Lstat(path); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("%s %s still exists after the run (err %v)", what, path, err)
	}
}

// dsvgAssertTmpEmpty checks that the run left nothing in TMPDIR (R3: the
// temporary directory is removed afterwards).
func dsvgAssertTmpEmpty(t *testing.T, e *dsvgEnv) {
	t.Helper()
	entries, err := os.ReadDir(e.tmp)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		t.Errorf("left behind in TMPDIR: %s", entry.Name())
	}
}

func dsvgSameFile(a, b string) bool {
	first, err := os.Stat(a)
	if err != nil {
		return false
	}
	second, err := os.Stat(b)
	if err != nil {
		return false
	}
	return os.SameFile(first, second)
}

// dsvgReportedPaths returns the rest of every output line that starts with
// prefix.
func dsvgReportedPaths(output, prefix string) []string {
	var paths []string
	for line := range strings.SplitSeq(output, "\n") {
		if rest, ok := strings.CutPrefix(strings.TrimRight(line, "\r"), prefix); ok {
			paths = append(paths, rest)
		}
	}
	return paths
}

// dsvgAssertReported checks that output has a "<prefix><target>" line for
// each target, spelled exactly as R1 derives it (the spec path as given with
// .json replaced by .svg, the --out value, or Join(root, "docs",
// "diagrams", name.svg) for --all).
func dsvgAssertReported(t *testing.T, output, prefix string, targets ...string) {
	t.Helper()
	reported := dsvgReportedPaths(output, prefix)
	for _, target := range targets {
		if !slices.Contains(reported, target) {
			t.Errorf("output has no %q line for %s:\n%s", prefix+"<target>", target, output)
		}
	}
}

func dsvgPtr(s string) *string { return &s }

func dsvgFlagValue(args []string, prefix string) (string, bool) {
	for _, arg := range args {
		if value, ok := strings.CutPrefix(arg, prefix); ok {
			return value, true
		}
	}
	return "", false
}

// dsvgWithin reports whether path is dir or below it.
func dsvgWithin(dir, path string) bool {
	rel, err := filepath.Rel(dir, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// dsvgCommonDir returns the deepest directory that holds every path.
func dsvgCommonDir(first string, rest ...string) string {
	common := filepath.Dir(filepath.Clean(first))
	for _, path := range rest {
		for !dsvgWithin(common, path) {
			parent := filepath.Dir(common)
			if parent == common {
				break
			}
			common = parent
		}
	}
	return common
}

// interface + determinism (R1, R5, R6): with the default root "." and a
// relative spec path, the normalised export (the browser's bytes plus one
// newline) is written next to the spec with mode 0644, `wrote <target>` is
// printed, and nothing else appears in the target directory or TMPDIR.
func TestDiagramSVGContractWritesNormalisedExportAtDefaultTarget(t *testing.T) {
	e := dsvgSetup(t)
	dsvgWriteSpec(t, e.diagrams, "alpha.json", `{"diagram_type": "flowchart-fake", "title": "Alpha"}`)
	t.Chdir(e.root)
	output, err := dsvgRun(t, filepath.Join("docs", "diagrams", "alpha.json"))
	if err != nil {
		t.Fatalf("diagram-svg failed: %v\noutput: %s", err, output)
	}
	target := filepath.Join(e.diagrams, "alpha.svg")
	dsvgAssertFile(t, target, string(dsvgFakeSVG("alpha"))+"\n")
	dsvgAssertMode(t, target, 0o644)
	dsvgAssertReported(t, output, "wrote ", filepath.Join("docs", "diagrams", "alpha.svg"))
	dsvgAssertSnapshot(t, "docs/diagrams", map[string]string{
		"alpha.json": `{"diagram_type": "flowchart-fake", "title": "Alpha"}`,
		"alpha.svg":  string(dsvgFakeSVG("alpha")) + "\n",
	}, dsvgSnapshot(t, e.diagrams))
	dsvgAssertTmpEmpty(t, e)
}

// interface (R6): an existing target is replaced by the new export with
// mode 0644 and no other file is left beside it.
func TestDiagramSVGContractReplacesExistingTarget(t *testing.T) {
	e := dsvgSetup(t)
	spec := dsvgWriteSpec(t, e.diagrams, "alpha.json", `{"diagram_type": "flowchart-fake"}`)
	target := filepath.Join(e.diagrams, "alpha.svg")
	dsvgWriteFile(t, target, "<?xml stale export?>\n", 0o600)
	output, err := dsvgRun(t, "--root", e.root, spec)
	if err != nil {
		t.Fatalf("diagram-svg failed: %v\noutput: %s", err, output)
	}
	dsvgAssertFile(t, target, string(dsvgFakeSVG("alpha"))+"\n")
	dsvgAssertMode(t, target, 0o644)
	dsvgAssertSnapshot(t, "docs/diagrams", map[string]string{
		"alpha.json": `{"diagram_type": "flowchart-fake"}`,
		"alpha.svg":  string(dsvgFakeSVG("alpha")) + "\n",
	}, dsvgSnapshot(t, e.diagrams))
}

// pipeline (R3, R4): finalize gets exactly `<script> finalize <type>
// <abs spec> <abs .html>` with cwd <root>, the resolved (absolute)
// ARCHIFY_CHROME and the inherited environment; the browser gets the
// required flags and, last, a file:// URL of a different file holding the
// viewer HTML with one script injected before the last </body>; the HTML,
// the injected copy and the user-data-dir share one temporary directory
// that is gone afterwards.
func TestDiagramSVGContractInvokesFinalizeAndBrowser(t *testing.T) {
	e := dsvgSetup(t)
	spec := dsvgWriteSpec(t, e.diagrams, "beta.json", `{"diagram_type": "sequence-fake"}`)
	t.Chdir(e.root)
	relative, err := filepath.Rel(e.root, e.chrome)
	if err != nil {
		t.Fatal(err)
	}
	// A relative ARCHIFY_CHROME must reach node as the resolved absolute
	// path, so plain inheritance cannot satisfy the check below.
	t.Setenv("ARCHIFY_CHROME", relative)
	output, err := dsvgRun(t, filepath.Join("docs", "diagrams", "beta.json"))
	if err != nil {
		t.Fatalf("diagram-svg failed: %v\noutput: %s", err, output)
	}

	nodes := dsvgReadRecords(t, e.records, "node")
	if len(nodes) != 1 {
		t.Fatalf("node ran %d times, want 1", len(nodes))
	}
	node := nodes[0]
	if len(node.Args) != 5 {
		t.Fatalf("node arguments %q, want script finalize type spec html (no --quality without meta.quality_profile)", node.Args)
	}
	if !dsvgSameFile(node.Args[0], e.script) {
		t.Errorf("node script %q, want %s", node.Args[0], e.script)
	}
	if node.Args[1] != "finalize" || node.Args[2] != "sequence-fake" {
		t.Errorf("node arguments %q, want finalize sequence-fake ...", node.Args)
	}
	if !filepath.IsAbs(node.Args[3]) || !dsvgSameFile(node.Args[3], spec) {
		t.Errorf("finalize spec %q, want the absolute path of %s", node.Args[3], spec)
	}
	html := node.Args[4]
	if !filepath.IsAbs(html) || !strings.HasSuffix(html, ".html") {
		t.Errorf("finalize output %q is not an absolute .html path", html)
	}
	if filepath.Clean(node.TmpDir) != filepath.Dir(filepath.Clean(html)) {
		t.Errorf("node TMPDIR %q, want the temporary directory holding %s", node.TmpDir, html)
	}
	if !dsvgSameFile(node.Cwd, e.root) {
		t.Errorf("node working directory %q, want %s", node.Cwd, e.root)
	}
	if !node.ArchifyChromeSet || !filepath.IsAbs(node.ArchifyChrome) || !dsvgSameFile(node.ArchifyChrome, e.chrome) {
		t.Errorf("node ARCHIFY_CHROME %q (set %t), want the absolute resolved browser %s", node.ArchifyChrome, node.ArchifyChromeSet, e.chrome)
	}
	if node.Sentinel != dsvgSentinel {
		t.Errorf("node did not inherit the caller's environment (sentinel %q)", node.Sentinel)
	}

	browsers := dsvgReadRecords(t, e.records, "chrome")
	if len(browsers) != 1 {
		t.Fatalf("browser ran %d times, want 1", len(browsers))
	}
	browser := browsers[0]
	for _, want := range []string{"--headless=new", "--remote-debugging-pipe"} {
		if !slices.Contains(browser.Args, want) {
			t.Errorf("browser arguments %q lack %s", browser.Args, want)
		}
	}
	for _, unwanted := range []string{"--dump-dom", "--virtual-time-budget"} {
		if slices.ContainsFunc(browser.Args, func(arg string) bool { return strings.HasPrefix(arg, unwanted) }) {
			t.Errorf("browser arguments %q contain %s", browser.Args, unwanted)
		}
	}
	userDataDir, ok := dsvgFlagValue(browser.Args, "--user-data-dir=")
	if !ok || userDataDir == "" {
		t.Errorf("browser arguments %q lack --user-data-dir=<dir>", browser.Args)
	}
	if last := browser.Args[len(browser.Args)-1]; !strings.HasPrefix(last, "file://") {
		t.Errorf("last browser argument %q is not a file:// URL", last)
	}
	if browser.Sentinel != dsvgSentinel {
		t.Errorf("browser did not inherit the caller's environment (sentinel %q)", browser.Sentinel)
	}
	if filepath.Clean(browser.InjectedPath) == filepath.Clean(html) {
		t.Errorf("browser opened the finalize output %s itself, want a separate injected copy", html)
	}
	dsvgCheckInjected(t, dsvgFakeViewer("beta", "sequence-fake"), []byte(browser.HTML))

	dsvgAssertMissing(t, "finalize output", html)
	dsvgAssertMissing(t, "injected copy", browser.InjectedPath)
	if userDataDir != "" {
		dsvgAssertMissing(t, "user-data-dir", userDataDir)
		if filepath.IsAbs(userDataDir) {
			common := dsvgCommonDir(html, browser.InjectedPath, userDataDir)
			if common == e.tmp || common == filepath.Dir(common) {
				t.Errorf("finalize output %s, injected copy %s and user-data-dir %s do not share one temporary directory", html, browser.InjectedPath, userDataDir)
			}
			dsvgAssertMissing(t, "temporary directory", common)
		}
	}
	dsvgAssertTmpEmpty(t, e)
}

// interface (R1): --out redirects the target, flags may follow the spec, and
// both `--flag value` and `--flag=value` work.
func TestDiagramSVGContractOutFlagAndFlagPositions(t *testing.T) {
	for _, form := range []string{"flags-after-spec", "equals-syntax"} {
		t.Run(form, func(t *testing.T) {
			e := dsvgSetup(t)
			spec := dsvgWriteSpec(t, e.diagrams, "gamma.json", `{"diagram_type": "flowchart-fake"}`)
			outDir := filepath.Join(e.root, "rendered")
			if err := os.Mkdir(outDir, 0o755); err != nil {
				t.Fatal(err)
			}
			out := filepath.Join(outDir, "custom-name.svg")
			var args []string
			if form == "flags-after-spec" {
				args = []string{spec, "--out", out, "--timeout", "30s", "--root", e.root}
			} else {
				args = []string{"--root=" + e.root, "--out=" + out, "--timeout=45s", spec}
			}
			output, err := dsvgRun(t, args...)
			if err != nil {
				t.Fatalf("diagram-svg %q failed: %v\noutput: %s", args, err, output)
			}
			dsvgAssertFile(t, out, string(dsvgFakeSVG("gamma"))+"\n")
			dsvgAssertMode(t, out, 0o644)
			dsvgAssertMissing(t, "default target", filepath.Join(e.diagrams, "gamma.svg"))
			dsvgAssertReported(t, output, "wrote ", out)
			dsvgAssertSnapshot(t, "--out directory", map[string]string{"custom-name.svg": string(dsvgFakeSVG("gamma")) + "\n"}, dsvgSnapshot(t, outDir))
		})
	}
}

// determinism (R5): an export that already ends in "\n" is written unchanged
// (no second newline), and --check accepts it.
func TestDiagramSVGContractKeepsExistingFinalNewline(t *testing.T) {
	e := dsvgSetup(t)
	t.Setenv(dsvgSVGNewlineEnv, "1")
	spec := dsvgWriteSpec(t, e.diagrams, "delta.json", `{"diagram_type": "flowchart-fake"}`)
	output, err := dsvgRun(t, "--root", e.root, spec)
	if err != nil {
		t.Fatalf("diagram-svg failed: %v\noutput: %s", err, output)
	}
	target := filepath.Join(e.diagrams, "delta.svg")
	dsvgAssertFile(t, target, string(dsvgFakeSVG("delta"))+"\n")
	if output, err := dsvgRun(t, "--check", "--root", e.root, spec); err != nil {
		t.Fatalf("--check after writing failed: %v\noutput: %s", err, output)
	}
}

// pipeline (R3): finalize receives `--quality <q>` after the HTML path only
// when the spec has meta.quality_profile.
func TestDiagramSVGContractQualityProfile(t *testing.T) {
	for _, c := range []struct {
		name, spec string
		want       []string
	}{
		{"absent", `{"diagram_type": "q-fake"}`, nil},
		{"meta-without-profile", `{"diagram_type": "q-fake", "meta": {"owner": "docs"}}`, nil},
		{"standard", `{"diagram_type": "q-fake", "meta": {"quality_profile": "standard"}}`, []string{"--quality", "standard"}},
		{"showcase", `{"diagram_type": "q-fake", "meta": {"quality_profile": "showcase"}}`, []string{"--quality", "showcase"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			e := dsvgSetup(t)
			spec := dsvgWriteSpec(t, e.diagrams, "q.json", c.spec)
			if output, err := dsvgRun(t, "--root", e.root, spec); err != nil {
				t.Fatalf("diagram-svg failed: %v\noutput: %s", err, output)
			}
			nodes := dsvgReadRecords(t, e.records, "node")
			if len(nodes) != 1 || len(nodes[0].Args) < 5 {
				t.Fatalf("node records %+v", nodes)
			}
			if extra := nodes[0].Args[5:]; !slices.Equal(extra, c.want) {
				t.Fatalf("finalize arguments after the HTML path %q, want %q", extra, c.want)
			}
		})
	}
}

// pipeline (R3): --no-sandbox is passed exactly when
// ARCHIFY_CHROME_NO_SANDBOX=1 (or the process runs as root).
func TestDiagramSVGContractNoSandbox(t *testing.T) {
	for _, c := range []struct {
		name  string
		value string
		set   bool
		want  bool
	}{
		{"unset", "", false, false},
		{"zero", "0", true, false},
		{"one", "1", true, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			if !c.want && os.Geteuid() == 0 {
				t.Skip("running as root always passes --no-sandbox")
			}
			e := dsvgSetup(t)
			if c.set {
				t.Setenv("ARCHIFY_CHROME_NO_SANDBOX", c.value)
			}
			spec := dsvgWriteSpec(t, e.diagrams, "sandbox.json", `{"diagram_type": "flowchart-fake"}`)
			if output, err := dsvgRun(t, "--root", e.root, spec); err != nil {
				t.Fatalf("diagram-svg failed: %v\noutput: %s", err, output)
			}
			browsers := dsvgReadRecords(t, e.records, "chrome")
			if len(browsers) != 1 {
				t.Fatalf("browser ran %d times, want 1", len(browsers))
			}
			if got := slices.Contains(browsers[0].Args, "--no-sandbox"); got != c.want {
				t.Fatalf("--no-sandbox present %t, want %t (arguments %q)", got, c.want, browsers[0].Args)
			}
		})
	}
}

// dsvgDeadlineRetry is the --timeout of the second attempt of a deadline
// case (v5, deadlines under load): far above the time any fake needs to
// start and record its helper, even on a loaded -race runner.
const dsvgDeadlineRetry = 30 * time.Second

// dsvgDeadlineAttempts runs a deadline case whose oracle needs the fakes to
// have reached some state (started, recorded a helper) before the deadline.
// attempt runs the command once in a fresh environment with the given
// --timeout, asserts every outcome that holds however far the fakes got,
// and reports whether they had reached that state; only then does it assert
// the rest. The case's own short timeout comes first. When the fakes had
// not reached the state and nothing failed, the case runs once more with
// dsvgDeadlineRetry, so start-up time on a loaded runner cannot fail it,
// while a defect that shows at either deadline still does. Each attempt
// keeps the case's bound on how long the command may take after its own
// deadline. A case not ready even at dsvgDeadlineRetry fails.
func dsvgDeadlineAttempts(t *testing.T, short time.Duration, attempt func(timeout time.Duration) (ready bool)) {
	t.Helper()
	for _, timeout := range []time.Duration{short, dsvgDeadlineRetry} {
		if attempt(timeout) || t.Failed() {
			return
		}
		t.Logf("the fakes had not reached the state this case needs when the %v deadline expired", timeout)
	}
	t.Fatalf("the fakes had not reached the state this case needs even with a %v deadline, so the case was not exercised", dsvgDeadlineRetry)
}

// dsvgFakesReady reports whether the fakes recorded in dir reached the
// state a deadline case needs: "helper" (a helper in the fake's group is
// recorded), "escaped" (a helper outside it is recorded), "litter" (the
// file-creating helper made its first file), "browser" (the fake browser
// started) or "evaluations" (the fake browser received at least three
// Runtime.evaluate commands, so the command had read a reply after the
// first).
func dsvgFakesReady(dir, state string) bool {
	switch state {
	case "helper", "escaped":
		return len(dsvgRecordedPids(dir, state)) > 0
	case "litter":
		matches, _ := filepath.Glob(filepath.Join(dir, "litter-*.started"))
		return len(matches) > 0
	case "browser":
		matches, _ := filepath.Glob(filepath.Join(dir, "chrome-*.json"))
		return len(matches) > 0
	case "evaluations":
		for _, session := range dsvgReadProtocolQuiet(dir) {
			evaluations := 0
			for _, command := range session {
				if command.Method == "Runtime.evaluate" {
					evaluations++
				}
			}
			if evaluations >= 3 {
				return true
			}
		}
		return false
	}
	panic("unknown fake state " + state)
}

// failures (R3, R4, R6): every failing step returns an error that says what
// failed, never writes a partial or empty SVG, leaves an existing target
// byte-for-byte unchanged and no other file in the target directory, and
// removes its temporary directory. A timeout kills the running child.
// Subtest names avoid the words the errors must contain, because temporary
// paths include them. Deadline cases that need a fake to have started or
// recorded a helper run through dsvgDeadlineAttempts.
func TestDiagramSVGContractFailuresWriteNothing(t *testing.T) {
	// failureBound is how long a run may take in all; for a deadline case,
	// the part of it after the case's own timeout is kept on a retry.
	const failureBound = 20 * time.Second
	for _, c := range []struct {
		name           string
		env            map[string]string
		timeout        time.Duration
		ready          string // the dsvgFakesReady state the case needs; "" none
		want           []string
		wantIfReady    []string // checked only when the fakes reached ready
		wantFold       []string
		wantNot        []string
		wantLines      [][]string // each group: some line of the error contains all of it
		browserStarted bool
		noClose        bool // the browser must not receive Browser.close
		helpers        bool // a helper process must be gone afterwards
	}{
		{name: "node-exit", env: map[string]string{dsvgNodeModeEnv: "fail"}, want: []string{"finalize", dsvgNodeTail}, wantNot: []string{"failed stage"}},
		{
			name: "node-exit-with-receipt", env: map[string]string{dsvgNodeModeEnv: "failreceipt"},
			want: []string{"finalize", dsvgNodeTail, "failed stage: " + dsvgFailedStage},
			wantLines: [][]string{
				{"error", dsvgDiagErrorCode, dsvgDiagErrorText, dsvgDiagErrorPath},
				{"warning", dsvgDiagWarnCode, dsvgDiagWarnText},
				{"failed stage: " + dsvgFailedStage},
			},
		},
		{
			name: "node-exit-receipt-without-stage", env: map[string]string{dsvgNodeModeEnv: "failnostage"},
			want: []string{"finalize", dsvgNodeTail}, wantNot: []string{"failed stage"},
			wantLines: [][]string{
				{"error", dsvgDiagErrorCode, dsvgDiagErrorText, dsvgDiagErrorPath},
				{"warning", dsvgDiagWarnCode, dsvgDiagWarnText},
			},
		},
		{name: "node-exit-unparsable-receipt", env: map[string]string{dsvgNodeModeEnv: "failbadreceipt"}, want: []string{"finalize", dsvgNodeTail}, wantNot: []string{"failed stage"}},
		{name: "deadline-in-node-with-helper", env: map[string]string{dsvgNodeModeEnv: "helpersleep"}, timeout: time.Second, ready: "helper", want: []string{"timed out"}, helpers: true},
		{name: "deadline-evaluate-errors", env: map[string]string{dsvgChromeModeEnv: "errorsforever"}, timeout: 1500 * time.Millisecond, ready: "evaluations", want: []string{"timed out"}, wantIfReady: []string{dsvgLateEvaluateError, dsvgBrowserTail}, browserStarted: true, noClose: true},
		{name: "deadline-blank-page-with-helper", env: map[string]string{dsvgChromeModeEnv: "helperblank"}, timeout: 1500 * time.Millisecond, ready: "helper", want: []string{"timed out"}, browserStarted: true, noClose: true, helpers: true},
		{name: "node-no-html", env: map[string]string{dsvgNodeModeEnv: "nohtml"}, want: []string{"finalize"}},
		{name: "node-empty-html", env: map[string]string{dsvgNodeModeEnv: "emptyhtml"}, want: []string{"finalize"}},
		{name: "browser-exit", env: map[string]string{dsvgChromeModeEnv: "fail"}, wantFold: []string{"chrome"}, want: []string{dsvgBrowserTail}, browserStarted: true},
		{name: "browser-exit-zero-before-marker", env: map[string]string{dsvgChromeModeEnv: "exitbeforemarker"}, wantFold: []string{"chrome"}, want: []string{dsvgBrowserTail}, browserStarted: true},
		{name: "browser-closes-reply-pipe", env: map[string]string{dsvgChromeModeEnv: "closepipe"}, wantFold: []string{"chrome"}, want: []string{dsvgBrowserTail}, browserStarted: true},
		{name: "page-without-marker", env: map[string]string{dsvgChromeModeEnv: "nomarker"}, want: []string{"no export"}, browserStarted: true},
		{name: "viewer-error-marker", env: map[string]string{dsvgChromeModeEnv: "errormarker"}, want: []string{dsvgViewerError}, browserStarted: true},
		{name: "export-with-script", env: map[string]string{dsvgChromeModeEnv: "script"}, browserStarted: true},
		{name: "export-without-background", env: map[string]string{dsvgChromeModeEnv: "nobackground"}, browserStarted: true},
		// The fake node sleeps from its start, so a deadline before it has
		// started still falls in finalize: no state is needed.
		{name: "deadline-in-node", env: map[string]string{dsvgNodeModeEnv: "sleep"}, timeout: 400 * time.Millisecond, want: []string{"timed out"}},
		{name: "deadline-in-browser", env: map[string]string{dsvgChromeModeEnv: "sleep"}, timeout: 1500 * time.Millisecond, ready: "browser", want: []string{"timed out"}, browserStarted: true},
		{name: "deadline-blank-page", env: map[string]string{dsvgChromeModeEnv: "blank"}, timeout: 1500 * time.Millisecond, ready: "browser", want: []string{"timed out"}, browserStarted: true, noClose: true},
	} {
		for _, existing := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/existing=%t", c.name, existing), func(t *testing.T) {
				// One run in a fresh environment with the given --timeout
				// (0: none); it reports whether the fakes reached c.ready.
				attempt := func(timeout time.Duration) bool {
					e := dsvgSetup(t)
					spec := dsvgWriteSpec(t, e.diagrams, "gamma.json", `{"diagram_type": "flowchart-fake"}`)
					dsvgWriteFile(t, filepath.Join(e.diagrams, "notes.txt"), "unrelated\n", 0o644)
					if existing {
						dsvgWriteFile(t, filepath.Join(e.diagrams, "gamma.svg"), "<?xml previous export?>\n", 0o644)
					}
					for key, value := range c.env {
						t.Setenv(key, value)
					}
					before := dsvgSnapshot(t, e.diagrams)
					args := []string{"--root", e.root}
					if timeout > 0 {
						args = append(args, "--timeout", timeout.String())
					}
					started := time.Now()
					output, err := dsvgRun(t, append(args, spec)...)
					elapsed := time.Since(started)
					if err == nil {
						t.Fatalf("diagram-svg succeeded; output: %s", output)
					}
					ready := c.ready == "" || dsvgFakesReady(e.records, c.ready)
					message := err.Error()
					wants := c.want
					if ready {
						wants = append(slices.Clone(wants), c.wantIfReady...)
					}
					for _, want := range wants {
						if !strings.Contains(message, want) {
							t.Errorf("error %q does not contain %q", message, want)
						}
					}
					for _, want := range c.wantFold {
						if !strings.Contains(strings.ToLower(message), want) {
							t.Errorf("error %q does not mention %q", message, want)
						}
					}
					for _, unwanted := range c.wantNot {
						if strings.Contains(message, unwanted) {
							t.Errorf("error %q contains %q", message, unwanted)
						}
					}
					for _, group := range c.wantLines {
						if !slices.ContainsFunc(strings.Split(message, "\n"), func(line string) bool {
							return !slices.ContainsFunc(group, func(part string) bool { return !strings.Contains(line, part) })
						}) {
							t.Errorf("error has no line containing all of %q:\n%s", group, message)
						}
					}
					if limit := failureBound - c.timeout + timeout; elapsed > limit {
						t.Errorf("run took %v (bound %v); the stuck child was not killed at the deadline", elapsed.Round(time.Millisecond), limit)
					}
					dsvgAssertSnapshot(t, "docs/diagrams", before, dsvgSnapshot(t, e.diagrams))
					// A deadline that came before the browser started is no
					// defect, so a started browser is required only once
					// the fakes were ready; a browser that should not start
					// never may.
					if ran := len(dsvgReadRecords(t, e.records, "chrome")) > 0; ran != c.browserStarted && (ready || ran) {
						t.Errorf("browser started %t, want %t", ran, c.browserStarted)
					}
					if c.noClose {
						for _, session := range dsvgReadProtocol(t, e.records) {
							if slices.ContainsFunc(session, func(command dsvgCommand) bool { return command.Method == "Browser.close" }) {
								t.Errorf("Browser.close was sent after the deadline")
							}
						}
					}
					if c.helpers && ready {
						dsvgAssertHelpersGone(t, e.records)
					} else {
						dsvgAssertRecordedHelpersGone(t, e.records)
					}
					dsvgAssertTmpEmpty(t, e)
					return ready
				}
				if c.ready == "" {
					attempt(c.timeout)
					return
				}
				dsvgDeadlineAttempts(t, c.timeout, attempt)
			})
		}
	}
}

// interface + failures (R1, R6): --all exports every docs/diagrams/*.json in
// lexical order and writes each target; when any export fails, no target is
// written.
func TestDiagramSVGContractAllExportsEverySpecBeforeWriting(t *testing.T) {
	names := []string{"charlie", "alpha", "bravo"}
	prepare := func(t *testing.T) *dsvgEnv {
		e := dsvgSetup(t)
		for _, name := range names {
			dsvgWriteSpec(t, e.diagrams, name+".json", fmt.Sprintf(`{"diagram_type": "type-%s"}`, name))
		}
		dsvgWriteFile(t, filepath.Join(e.diagrams, "README.md"), "not a spec\n", 0o644)
		dsvgWriteFile(t, filepath.Join(e.diagrams, "alpha.svg"), "<?xml stale alpha?>\n", 0o644)
		return e
	}

	t.Run("success", func(t *testing.T) {
		e := prepare(t)
		output, err := dsvgRun(t, "--all", "--root", e.root)
		if err != nil {
			t.Fatalf("--all failed: %v\noutput: %s", err, output)
		}
		want := map[string]string{"README.md": "not a spec\n"}
		var targets []string
		for _, name := range names {
			want[name+".json"] = fmt.Sprintf(`{"diagram_type": "type-%s"}`, name)
			want[name+".svg"] = string(dsvgFakeSVG(name)) + "\n"
			targets = append(targets, filepath.Join(e.diagrams, name+".svg"))
		}
		dsvgAssertSnapshot(t, "docs/diagrams", want, dsvgSnapshot(t, e.diagrams))
		dsvgAssertReported(t, output, "wrote ", targets...)
		var order []string
		for _, node := range dsvgReadRecords(t, e.records, "node") {
			if len(node.Args) < 5 {
				t.Fatalf("node arguments %q", node.Args)
			}
			name := strings.TrimSuffix(filepath.Base(node.Args[3]), ".json")
			if node.Args[2] != "type-"+name {
				t.Errorf("spec %s finalized with diagram type %q", name, node.Args[2])
			}
			order = append(order, name)
		}
		if want := []string{"alpha", "bravo", "charlie"}; !slices.Equal(order, want) {
			t.Errorf("finalize order %q, want lexical %q", order, want)
		}
		dsvgAssertTmpEmpty(t, e)
	})

	for _, c := range []struct {
		name string
		env  map[string]string
	}{
		{"last-spec-browser-fails", map[string]string{dsvgChromeFailSpecEnv: "charlie.json"}},
		{"middle-spec-node-fails", map[string]string{dsvgNodeFailSpecEnv: "bravo.json"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			e := prepare(t)
			for key, value := range c.env {
				t.Setenv(key, value)
			}
			before := dsvgSnapshot(t, e.diagrams)
			output, err := dsvgRun(t, "--all", "--root", e.root)
			if err == nil {
				t.Fatalf("--all succeeded although one export failed; output: %s", output)
			}
			dsvgAssertSnapshot(t, "docs/diagrams", before, dsvgSnapshot(t, e.diagrams))
			dsvgAssertTmpEmpty(t, e)
		})
	}
}

// interface + determinism (R1, R5): --check exports fresh, compares the
// normalised export with the target bytes, writes nothing, prints
// `up to date: <target>` when all match and otherwise fails with
// `out of date` naming every differing target.
func TestDiagramSVGContractCheck(t *testing.T) {
	const spec = `{"diagram_type": "flowchart-fake"}`
	current := func(name string) string { return string(dsvgFakeSVG(name)) + "\n" }

	single := []struct {
		name     string
		target   *string // nil: no target file
		useOut   bool
		nodeMode string
		wantErr  bool
	}{
		{name: "up-to-date", target: dsvgPtr(current("delta"))},
		{name: "up-to-date-with-out", target: dsvgPtr(current("delta")), useOut: true},
		{name: "differs", target: dsvgPtr("<?xml stale export?>\n"), wantErr: true},
		{name: "differs-by-final-newline", target: dsvgPtr(string(dsvgFakeSVG("delta"))), wantErr: true},
		{name: "missing", wantErr: true},
		{name: "export-fails", target: dsvgPtr(current("delta")), nodeMode: "fail", wantErr: true},
	}
	for _, c := range single {
		t.Run(c.name, func(t *testing.T) {
			e := dsvgSetup(t)
			t.Setenv(dsvgNodeModeEnv, c.nodeMode)
			specPath := dsvgWriteSpec(t, e.diagrams, "delta.json", spec)
			target := filepath.Join(e.diagrams, "delta.svg")
			args := []string{"--check", "--root", e.root}
			if c.useOut {
				target = filepath.Join(e.root, "elsewhere.svg")
				args = append(args, "--out", target)
			}
			if c.target != nil {
				dsvgWriteFile(t, target, *c.target, 0o644)
			}
			before := dsvgSnapshot(t, e.root)
			output, err := dsvgRun(t, append(args, specPath)...)
			dsvgAssertSnapshot(t, "root", before, dsvgSnapshot(t, e.root))
			dsvgAssertTmpEmpty(t, e)
			if !c.wantErr {
				if err != nil {
					t.Fatalf("--check failed: %v\noutput: %s", err, output)
				}
				dsvgAssertReported(t, output, "up to date: ", target)
				if n, b := len(dsvgReadRecords(t, e.records, "node")), len(dsvgReadRecords(t, e.records, "chrome")); n != 1 || b != 1 {
					t.Errorf("--check ran node %d and the browser %d times, want a fresh export (1 each)", n, b)
				}
				return
			}
			if err == nil {
				t.Fatalf("--check passed; output: %s", output)
			}
			if c.nodeMode != "" {
				return
			}
			if !strings.Contains(err.Error(), "out of date") || !strings.Contains(err.Error(), target) {
				t.Errorf("error %q does not say `out of date` and name %s", err, target)
			}
		})
	}

	t.Run("target-is-a-directory", func(t *testing.T) {
		e := dsvgSetup(t)
		specPath := dsvgWriteSpec(t, e.diagrams, "delta.json", spec)
		target := filepath.Join(e.diagrams, "delta.svg")
		if err := os.Mkdir(target, 0o755); err != nil {
			t.Fatal(err)
		}
		before := dsvgSnapshot(t, e.root)
		output, err := dsvgRun(t, "--check", "--root", e.root, specPath)
		dsvgAssertSnapshot(t, "root", before, dsvgSnapshot(t, e.root))
		dsvgAssertTmpEmpty(t, e)
		if err == nil {
			t.Fatalf("--check passed with a directory as its target; output: %s", output)
		}
		if !strings.Contains(err.Error(), target) || strings.Contains(err.Error(), "out of date") {
			t.Errorf("error %q must name %s and must not say `out of date`", err, target)
		}
	})

	t.Run("all-mixed", func(t *testing.T) {
		e := dsvgSetup(t)
		for _, name := range []string{"alpha", "bravo", "charlie"} {
			dsvgWriteSpec(t, e.diagrams, name+".json", spec)
		}
		dsvgWriteFile(t, filepath.Join(e.diagrams, "alpha.svg"), current("alpha"), 0o644)
		dsvgWriteFile(t, filepath.Join(e.diagrams, "bravo.svg"), current("alpha"), 0o644)
		before := dsvgSnapshot(t, e.root)
		output, err := dsvgRun(t, "--check", "--all", "--root", e.root)
		dsvgAssertSnapshot(t, "root", before, dsvgSnapshot(t, e.root))
		if err == nil {
			t.Fatalf("--check --all passed with a differing and a missing target; output: %s", output)
		}
		for _, want := range []string{"out of date", filepath.Join(e.diagrams, "bravo.svg"), filepath.Join(e.diagrams, "charlie.svg")} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("error %q does not contain %q", err, want)
			}
		}
	})

	t.Run("all-up-to-date", func(t *testing.T) {
		e := dsvgSetup(t)
		var targets []string
		for _, name := range []string{"alpha", "bravo", "charlie"} {
			dsvgWriteSpec(t, e.diagrams, name+".json", spec)
			target := filepath.Join(e.diagrams, name+".svg")
			dsvgWriteFile(t, target, current(name), 0o644)
			targets = append(targets, target)
		}
		before := dsvgSnapshot(t, e.root)
		output, err := dsvgRun(t, "--check", "--all", "--root", e.root)
		dsvgAssertSnapshot(t, "root", before, dsvgSnapshot(t, e.root))
		if err != nil {
			t.Fatalf("--check --all failed: %v\noutput: %s", err, output)
		}
		dsvgAssertReported(t, output, "up to date: ", targets...)
	})
}

// failures (R2): a missing node or an unusable ARCHIFY_CHROME is reported
// before anything starts and nothing is written; ARCHIFY_CHROME may be a
// symbolic link to an executable. Subtest names avoid "node" because
// temporary paths include them.
func TestDiagramSVGContractToolResolution(t *testing.T) {
	t.Run("path-without-runtime", func(t *testing.T) {
		e := dsvgSetup(t)
		spec := dsvgWriteSpec(t, e.diagrams, "eps.json", `{"diagram_type": "flowchart-fake"}`)
		t.Setenv("PATH", dsvgTempDir(t))
		before := dsvgSnapshot(t, e.root)
		output, err := dsvgRun(t, "--root", e.root, spec)
		if err == nil || !strings.Contains(err.Error(), "node") {
			t.Fatalf("error %v does not mention node; output: %s", err, output)
		}
		if records := dsvgReadRecords(t, e.records, "chrome"); len(records) != 0 {
			t.Errorf("browser started without node")
		}
		dsvgAssertSnapshot(t, "root", before, dsvgSnapshot(t, e.root))
		dsvgAssertTmpEmpty(t, e)
	})

	for _, c := range []struct {
		name    string
		browser func(t *testing.T, e *dsvgEnv) string
	}{
		{"not-executable", func(t *testing.T, e *dsvgEnv) string {
			path := filepath.Join(dsvgTempDir(t), "browser")
			dsvgWriteFile(t, path, "#!/bin/sh\nexit 0\n", 0o644)
			return path
		}},
		{"directory", func(t *testing.T, e *dsvgEnv) string { return dsvgTempDir(t) }},
		{"missing", func(t *testing.T, e *dsvgEnv) string { return filepath.Join(dsvgTempDir(t), "absent") }},
		{"empty", func(t *testing.T, e *dsvgEnv) string { return "" }},
	} {
		t.Run("browser-"+c.name, func(t *testing.T) {
			e := dsvgSetup(t)
			spec := dsvgWriteSpec(t, e.diagrams, "eps.json", `{"diagram_type": "flowchart-fake"}`)
			t.Setenv("ARCHIFY_CHROME", c.browser(t, e))
			before := dsvgSnapshot(t, e.root)
			output, err := dsvgRun(t, "--root", e.root, spec)
			if err == nil || !strings.Contains(err.Error(), "ARCHIFY_CHROME") {
				t.Fatalf("error %v does not mention ARCHIFY_CHROME; output: %s", err, output)
			}
			if n, b := len(dsvgReadRecords(t, e.records, "node")), len(dsvgReadRecords(t, e.records, "chrome")); n+b != 0 {
				t.Errorf("node ran %d and the browser %d times although the browser was not resolved", n, b)
			}
			dsvgAssertSnapshot(t, "root", before, dsvgSnapshot(t, e.root))
			dsvgAssertTmpEmpty(t, e)
		})
	}

	t.Run("browser-symlink", func(t *testing.T) {
		e := dsvgSetup(t)
		spec := dsvgWriteSpec(t, e.diagrams, "eps.json", `{"diagram_type": "flowchart-fake"}`)
		link := filepath.Join(dsvgTempDir(t), dsvgChromeName)
		if err := os.Symlink(e.chrome, link); err != nil {
			t.Fatal(err)
		}
		t.Setenv("ARCHIFY_CHROME", link)
		if output, err := dsvgRun(t, "--root", e.root, spec); err != nil {
			t.Fatalf("diagram-svg with a symlinked ARCHIFY_CHROME failed: %v\noutput: %s", err, output)
		}
		dsvgAssertFile(t, filepath.Join(e.diagrams, "eps.svg"), string(dsvgFakeSVG("eps"))+"\n")
	})
}

// The fake export itself satisfies the viewer contract, so a fake-pipeline
// failure is never caused by the fixture.
func TestDiagramSVGContractFakeExportIsValid(t *testing.T) {
	if err := validateExport(dsvgFakeSVG("fixture")); err != nil {
		t.Fatalf("fake export rejected: %v", err)
	}
	if got := dsvgSpecName(dsvgFakeViewer("fixture", "x")); got != "fixture" {
		t.Fatalf("fake viewer spec name %q", got)
	}
}

// dsvgCheckProtocol asserts the R3 pipe command sequence one browser
// received: Target.getTargets (at least minTargets times),
// Target.attachToTarget once with the page target and flatten, then
// Runtime.evaluate on the attached session with returnByValue (at least
// minEvaluations times), then Browser.close, with positive unique ids.
func dsvgCheckProtocol(t *testing.T, session []dsvgCommand, minTargets, minEvaluations int, wantClose bool) {
	t.Helper()
	var methods []string
	ids := map[int64]bool{}
	for _, command := range session {
		if command.Method == dsvgEOFMethod {
			continue
		}
		methods = append(methods, command.Method)
		if command.ID <= 0 || ids[command.ID] {
			t.Errorf("command %s has id %d; ids must be positive and unique", command.Method, command.ID)
		}
		ids[command.ID] = true
	}
	count := func(from int, method string) int {
		n := 0
		for from+n < len(methods) && methods[from+n] == method {
			n++
		}
		return n
	}
	targets := count(0, "Target.getTargets")
	attaches := count(targets, "Target.attachToTarget")
	evaluations := count(targets+attaches, "Runtime.evaluate")
	closes := count(targets+attaches+evaluations, "Browser.close")
	if targets < minTargets || attaches != 1 || evaluations < minEvaluations || targets+attaches+evaluations+closes != len(methods) || (wantClose && closes != 1) {
		t.Fatalf("command sequence %q; want Target.getTargets x>=%d, Target.attachToTarget x1, Runtime.evaluate x>=%d, Browser.close", methods, minTargets, minEvaluations)
	}
	for _, command := range session {
		switch command.Method {
		case "Target.attachToTarget":
			if command.Params["targetId"] != dsvgPageTarget || command.Params["flatten"] != true {
				t.Errorf("Target.attachToTarget params %v, want targetId %q and flatten true", command.Params, dsvgPageTarget)
			}
		case "Runtime.evaluate":
			if command.SessionID != dsvgSessionID || command.Params["returnByValue"] != true {
				t.Errorf("Runtime.evaluate with sessionId %q and params %v, want sessionId %q and returnByValue true", command.SessionID, command.Params, dsvgSessionID)
			}
		}
	}
}

// pipeline (R3): the browser is driven over the DevTools pipe on
// descriptors 3 and 4 with NUL-terminated JSON. The command polls
// Target.getTargets until a page target appears, attaches with flatten,
// polls Runtime.evaluate through error replies, "" and non-string values
// until a marker string arrives, ignores events, takes the export from that
// reply (not from stdout, which carries a decoy export), and sends
// Browser.close.
func TestDiagramSVGContractDrivesBrowserOverPipe(t *testing.T) {
	for _, c := range []struct {
		mode                       string
		minTargets, minEvaluations int
	}{
		{"", 1, 1},
		{"retry", 3, 5},
	} {
		t.Run("mode="+c.mode, func(t *testing.T) {
			e := dsvgSetup(t)
			t.Setenv(dsvgChromeModeEnv, c.mode)
			spec := dsvgWriteSpec(t, e.diagrams, "pipe.json", `{"diagram_type": "flowchart-fake"}`)
			output, err := dsvgRun(t, "--root", e.root, "--timeout", "30s", spec)
			if err != nil {
				t.Fatalf("diagram-svg failed: %v\noutput: %s", err, output)
			}
			dsvgAssertFile(t, filepath.Join(e.diagrams, "pipe.svg"), string(dsvgFakeSVG("pipe"))+"\n")
			sessions := dsvgReadProtocol(t, e.records)
			if len(sessions) != 1 {
				t.Fatalf("browser protocol sessions %d, want 1", len(sessions))
			}
			dsvgCheckProtocol(t, sessions[0], c.minTargets, c.minEvaluations, true)
			dsvgAssertTmpEmpty(t, e)
		})
	}
}

// pipeline (R3): after Browser.close the command closes descriptor 3 and
// waits for the browser, killing it if it has not exited within about five
// seconds; the export already received is written either way.
func TestDiagramSVGContractShutsDownBrowserAfterMarker(t *testing.T) {
	t.Run("exits-when-commands-close", func(t *testing.T) {
		e := dsvgSetup(t)
		t.Setenv(dsvgChromeModeEnv, "linger")
		spec := dsvgWriteSpec(t, e.diagrams, "close.json", `{"diagram_type": "flowchart-fake"}`)
		if output, err := dsvgRun(t, "--root", e.root, "--timeout", "30s", spec); err != nil {
			t.Fatalf("diagram-svg failed: %v\noutput: %s", err, output)
		}
		dsvgAssertFile(t, filepath.Join(e.diagrams, "close.svg"), string(dsvgFakeSVG("close"))+"\n")
		sessions := dsvgReadProtocol(t, e.records)
		if len(sessions) != 1 {
			t.Fatalf("browser protocol sessions %d, want 1", len(sessions))
		}
		session := sessions[0]
		dsvgCheckProtocol(t, session, 1, 1, true)
		if len(session) == 0 || session[len(session)-1].Method != dsvgEOFMethod {
			t.Errorf("the browser never saw descriptor 3 close after Browser.close")
		}
		dsvgAssertTmpEmpty(t, e)
	})

	t.Run("killed-when-it-ignores-close", func(t *testing.T) {
		e := dsvgSetup(t)
		t.Setenv(dsvgChromeModeEnv, "stubborn")
		spec := dsvgWriteSpec(t, e.diagrams, "stubborn.json", `{"diagram_type": "flowchart-fake"}`)
		started := time.Now()
		output, err := dsvgRun(t, "--root", e.root, "--timeout", "45s", spec)
		if elapsed := time.Since(started); elapsed > 20*time.Second {
			t.Errorf("run took %v; a browser that ignores Browser.close was not killed after about 5 s", elapsed)
		}
		if err != nil {
			t.Fatalf("diagram-svg failed: %v\noutput: %s", err, output)
		}
		dsvgAssertFile(t, filepath.Join(e.diagrams, "stubborn.svg"), string(dsvgFakeSVG("stubborn"))+"\n")
		dsvgAssertTmpEmpty(t, e)
	})
}

// pipeline (R3): the browser runs in its own process group, which is killed
// after the browser exits, so a helper it started that inherited stderr and
// would run for a minute neither survives nor holds the command.
func TestDiagramSVGContractKillsBrowserProcessGroup(t *testing.T) {
	e := dsvgSetup(t)
	t.Setenv(dsvgChromeModeEnv, "helper")
	spec := dsvgWriteSpec(t, e.diagrams, "group.json", `{"diagram_type": "flowchart-fake"}`)
	started := time.Now()
	output, err := dsvgRun(t, "--root", e.root, "--timeout", "45s", spec)
	if elapsed := time.Since(started); elapsed > 20*time.Second {
		t.Errorf("run took %v; the browser's helper process held the command", elapsed)
	}
	if err != nil {
		t.Fatalf("diagram-svg failed: %v\noutput: %s", err, output)
	}
	dsvgAssertFile(t, filepath.Join(e.diagrams, "group.svg"), string(dsvgFakeSVG("group"))+"\n")
	dsvgAssertHelpersGone(t, e.records)
	dsvgAssertTmpEmpty(t, e)
}

// pipeline + failures (R3, R6): when run's context is cancelled while the
// command polls Runtime.evaluate, the browser is killed at once without
// Browser.close, run returns an error and nothing is written.
func TestDiagramSVGContractInterruptSkipsBrowserClose(t *testing.T) {
	e := dsvgSetup(t)
	t.Setenv(dsvgChromeModeEnv, "blank")
	spec := dsvgWriteSpec(t, e.diagrams, "interrupt.json", `{"diagram_type": "flowchart-fake"}`)
	dsvgWriteFile(t, filepath.Join(e.diagrams, "interrupt.svg"), "<?xml previous export?>\n", 0o644)
	before := dsvgSnapshot(t, e.diagrams)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go func() {
		// Cancel once the command is polling the page.
		deadline := time.Now().Add(15 * time.Second)
		for time.Now().Before(deadline) && ctx.Err() == nil {
			for _, session := range dsvgReadProtocolQuiet(e.records) {
				if slices.ContainsFunc(session, func(command dsvgCommand) bool { return command.Method == "Runtime.evaluate" }) {
					cancel()
					return
				}
			}
			time.Sleep(50 * time.Millisecond)
		}
		cancel()
	}()
	var err error
	started := time.Now()
	output := captureOutput(t, func() {
		err = run(ctx, []string{"diagram-svg", "--root", e.root, spec})
	})
	if elapsed := time.Since(started); elapsed > 25*time.Second {
		t.Errorf("run took %v after the interrupt", elapsed)
	}
	if err == nil {
		t.Fatalf("interrupted run succeeded; output: %s", output)
	}
	dsvgAssertSnapshot(t, "docs/diagrams", before, dsvgSnapshot(t, e.diagrams))
	for _, session := range dsvgReadProtocol(t, e.records) {
		if !slices.ContainsFunc(session, func(command dsvgCommand) bool { return command.Method == "Runtime.evaluate" }) {
			t.Errorf("the interrupt came before Runtime.evaluate; the test did not exercise polling")
		}
		if slices.ContainsFunc(session, func(command dsvgCommand) bool { return command.Method == "Browser.close" }) {
			t.Errorf("Browser.close was sent after the interrupt")
		}
	}
	dsvgAssertTmpEmpty(t, e)
}

// dsvgReadProtocolQuiet is dsvgReadProtocol for a polling loop: instead of
// failing the test it skips a file it cannot read and a line it cannot
// decode. The logs are written atomically (dsvgProtocolLog), so this is
// only a guard.
func dsvgReadProtocolQuiet(dir string) [][]dsvgCommand {
	matches, _ := filepath.Glob(filepath.Join(dir, "proto-*.jsonl"))
	var sessions [][]dsvgCommand
	for _, match := range matches {
		data, err := os.ReadFile(match)
		if err != nil {
			continue
		}
		var session []dsvgCommand
		for line := range strings.SplitSeq(string(data), "\n") {
			var command dsvgCommand
			if json.Unmarshal([]byte(line), &command) == nil {
				session = append(session, command)
			}
		}
		sessions = append(sessions, session)
	}
	return sessions
}

// R7 (contract diagram-svg-v4): process cleanup and signals.

const (
	// dsvgHoldBound is how long after the deadline or the marker the
	// command may take when a helper outside the process group holds its
	// output: R7 says about 10 seconds; the rest is slack for a loaded
	// machine. A command that waits for the helper takes a minute.
	dsvgHoldBound = 15 * time.Second
	// dsvgHoldRelease is when the test kills the escaped helpers itself,
	// so a command that waits for their output (the R7 violation) is
	// released and the test stays bounded.
	dsvgHoldRelease = 18 * time.Second
)

// dsvgReleaseEscapedAfter kills the helpers recorded as escaped-<pid>.pid
// in dir once after has passed. The returned stop function cancels that
// and reports whether it had already happened.
func dsvgReleaseEscapedAfter(dir string, after time.Duration) (stop func() bool) {
	var fired atomic.Bool
	timer := time.AfterFunc(after, func() {
		fired.Store(true)
		for _, pid := range dsvgRecordedPids(dir, "escaped") {
			syscall.Kill(pid, syscall.SIGKILL)
		}
	})
	return func() bool {
		timer.Stop()
		return fired.Load()
	}
}

// dsvgHoldCase is one R7 case of a helper that leaves its process group
// and holds the inherited output.
type dsvgHoldCase struct {
	nodeMode, browserMode string
	timeout               time.Duration
	succeeds              bool
	// from is the pattern of the record holding the time the bound is
	// measured from; "" measures from the --timeout deadline.
	from string
}

// dsvgCheckHelperOutsideGroupDoesNotHold runs one dsvgHoldCase: the run
// succeeds (writing the export) or fails with `timed out` (writing
// nothing), returns within dsvgHoldBound of its reference time without the
// test having to kill the helper, and leaves TMPDIR empty. A deadline case
// runs through dsvgDeadlineAttempts, since it needs the escaped helper to
// be recorded before the deadline.
func dsvgCheckHelperOutsideGroupDoesNotHold(t *testing.T, c dsvgHoldCase) {
	t.Helper()
	if c.succeeds {
		dsvgHoldAttempt(t, c, c.timeout)
		return
	}
	dsvgDeadlineAttempts(t, c.timeout, func(timeout time.Duration) bool { return dsvgHoldAttempt(t, c, timeout) })
}

// dsvgHoldAttempt is one run of a dsvgHoldCase with the given --timeout. It
// reports whether the fake had recorded its escaped helper; a successful
// case without one fails, since nothing then held the output.
func dsvgHoldAttempt(t *testing.T, c dsvgHoldCase, timeout time.Duration) bool {
	t.Helper()
	e := dsvgSetup(t)
	t.Setenv(dsvgNodeModeEnv, c.nodeMode)
	t.Setenv(dsvgChromeModeEnv, c.browserMode)
	spec := dsvgWriteSpec(t, e.diagrams, "hold.json", `{"diagram_type": "flowchart-fake"}`)
	target := filepath.Join(e.diagrams, "hold.svg")
	dsvgWriteFile(t, target, "<?xml previous export?>\n", 0o644)
	before := dsvgSnapshot(t, e.diagrams)
	release := dsvgHoldRelease
	if !c.succeeds {
		release += timeout
	}
	stop := dsvgReleaseEscapedAfter(e.records, release)
	started := time.Now()
	output, err := dsvgRun(t, "--root", e.root, "--timeout", timeout.String(), spec)
	returned := time.Now()
	released := stop()
	ready := dsvgFakesReady(e.records, "escaped")
	if c.succeeds && !ready {
		t.Fatalf("the fake started no helper outside its process group, so the case was not exercised (error %v, output %s)", err, output)
	}
	reference, what := started.Add(timeout), "the --timeout deadline"
	if c.succeeds {
		if err != nil {
			t.Fatalf("diagram-svg failed: %v\noutput: %s", err, output)
		}
		dsvgAssertFile(t, target, string(dsvgFakeSVG("hold"))+"\n")
		reference, what = dsvgReadTime(t, e.records, c.from), c.from
	} else {
		if err == nil || !strings.Contains(err.Error(), "timed out") {
			t.Errorf("error %v, want one containing `timed out`; output: %s", err, output)
		}
		dsvgAssertSnapshot(t, "docs/diagrams", before, dsvgSnapshot(t, e.diagrams))
	}
	if held := returned.Sub(reference); held > dsvgHoldBound || released {
		t.Errorf("run returned %v after %s (the test had to kill the helper: %t); a helper outside the process group that holds the inherited output held the command", held.Round(time.Millisecond), what, released)
	}
	dsvgAssertTmpEmpty(t, e)
	return ready
}

// failures (R7, helpers that leave the process group): a helper the browser
// started in a new session holds the inherited stderr for a minute; the
// command still writes the export and returns within about 10 s of the
// marker. Fails if the command waits for the helper's end of stderr.
func TestDiagramSVGContractBrowserHelperOutsideGroupAfterMarker(t *testing.T) {
	dsvgCheckHelperOutsideGroupDoesNotHold(t, dsvgHoldCase{browserMode: "escape", timeout: 45 * time.Second, succeeds: true, from: "marker-*.time"})
}

// failures (R7, helpers that leave the process group): the same helper
// while the page never yields a marker; the command fails with `timed out`
// within about 10 s of the deadline and writes nothing. Fails if the
// command waits for the helper's end of stderr after killing the browser.
func TestDiagramSVGContractBrowserHelperOutsideGroupAfterDeadline(t *testing.T) {
	dsvgCheckHelperOutsideGroupDoesNotHold(t, dsvgHoldCase{browserMode: "escapeblank", timeout: 3 * time.Second})
}

// failures (R7, helpers that leave the process group): a helper node
// started in a new session holds node's stdout and stderr for a minute
// while node itself succeeds; finalize is not held, so the command writes
// the export and returns within about 10 s of node finishing (the marker
// comes later still). Fails if the command waits for node's output to
// close.
func TestDiagramSVGContractFinalizeHelperOutsideGroupOnSuccess(t *testing.T) {
	dsvgCheckHelperOutsideGroupDoesNotHold(t, dsvgHoldCase{nodeMode: "escape", timeout: 45 * time.Second, succeeds: true, from: "nodedone-*.time"})
}

// failures (R7, helpers that leave the process group): the same helper
// while node never finishes; at the deadline the command fails with `timed
// out` within about 10 s and writes nothing. Fails if the command waits for
// node's output to close after killing node's group.
func TestDiagramSVGContractFinalizeHelperOutsideGroupAfterDeadline(t *testing.T) {
	dsvgCheckHelperOutsideGroupDoesNotHold(t, dsvgHoldCase{nodeMode: "escapesleep", timeout: 2 * time.Second})
}

// failures (R7, nothing left in the temporary directory; R3 group kill): a
// helper in node's (or the browser's) process group keeps creating files in
// the command's temporary directory, recreating it if needed, until it is
// killed. After a finalize or browser deadline, and after a successful
// browser run, the helper is gone and the TMPDIR the command was started
// with is empty. Fails if the command does not kill the group (the helper
// survives and keeps writing into TMPDIR), does not remove its temporary
// directory, or takes far longer than the deadline (or the marker) plus
// the about 5 s R7 allows for the group to go. It cannot show that the
// command waits for the group to be gone before removing the directory:
// SIGKILL usually ends the helper before the removal starts, so a command
// without that wait usually passes too. The direct R8 tests in
// process_group_test.go guard that wait. The deadline cases run through
// dsvgDeadlineAttempts, since they need the helper's first file before the
// deadline.
func TestDiagramSVGContractGroupHelperLeavesNothingInTMPDIR(t *testing.T) {
	for _, c := range []struct {
		name, nodeMode, browserMode string
		timeout                     time.Duration
		succeeds                    bool
	}{
		{name: "finalize-deadline", nodeMode: "litter", timeout: 2 * time.Second},
		{name: "browser-deadline", browserMode: "litterblank", timeout: 3 * time.Second},
		{name: "browser-after-marker", browserMode: "litter", timeout: 45 * time.Second, succeeds: true},
	} {
		t.Run(c.name, func(t *testing.T) {
			// One run with the given --timeout; it reports whether the
			// helper had made its first file.
			attempt := func(timeout time.Duration) bool {
				e := dsvgSetup(t)
				t.Setenv(dsvgNodeModeEnv, c.nodeMode)
				t.Setenv(dsvgChromeModeEnv, c.browserMode)
				spec := dsvgWriteSpec(t, e.diagrams, "litter.json", `{"diagram_type": "flowchart-fake"}`)
				before := dsvgSnapshot(t, e.diagrams)
				started := time.Now()
				output, err := dsvgRun(t, "--root", e.root, "--timeout", timeout.String(), spec)
				elapsed := time.Since(started)
				ready := dsvgFakesReady(e.records, "litter")
				if c.succeeds && !ready {
					t.Fatalf("the helper created no file, so the case was not exercised (error %v, output %s)", err, output)
				}
				limit := 15 * time.Second
				if c.succeeds {
					if err != nil {
						t.Fatalf("diagram-svg failed: %v\noutput: %s", err, output)
					}
					dsvgAssertFile(t, filepath.Join(e.diagrams, "litter.svg"), string(dsvgFakeSVG("litter"))+"\n")
				} else {
					limit += timeout
					if err == nil || !strings.Contains(err.Error(), "timed out") {
						t.Errorf("error %v, want one containing `timed out`; output: %s", err, output)
					}
					dsvgAssertSnapshot(t, "docs/diagrams", before, dsvgSnapshot(t, e.diagrams))
				}
				if elapsed > limit {
					t.Errorf("run took %v (bound %v); the command must not wait more than about 5 s for the killed process group", elapsed.Round(time.Millisecond), limit)
				}
				// Waits until each recorded helper is gone, so a late file
				// shows below.
				if ready {
					dsvgAssertHelpersGone(t, e.records)
				} else {
					dsvgAssertRecordedHelpersGone(t, e.records)
				}
				dsvgAssertTmpEmpty(t, e)
				return ready
			}
			if c.succeeds {
				attempt(c.timeout)
				return
			}
			dsvgDeadlineAttempts(t, c.timeout, attempt)
		})
	}
}

// dsvgCaptureStreams returns what the action wrote to standard output and
// to standard error, separately.
func dsvgCaptureStreams(t *testing.T, action func()) (stdout, stderr string) {
	t.Helper()
	dir := dsvgTempDir(t)
	out, err := os.Create(filepath.Join(dir, "stdout"))
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()
	errs, err := os.Create(filepath.Join(dir, "stderr"))
	if err != nil {
		t.Fatal(err)
	}
	defer errs.Close()
	savedOut, savedErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = out, errs
	func() {
		defer func() { os.Stdout, os.Stderr = savedOut, savedErr }()
		action()
	}()
	outData, err := os.ReadFile(out.Name())
	if err != nil {
		t.Fatal(err)
	}
	errData, err := os.ReadFile(errs.Name())
	if err != nil {
		t.Fatal(err)
	}
	return string(outData), string(errData)
}

// failures (R7, removal failure is reported): when the temporary directory
// cannot be removed (finalize left a directory in it without write
// permission), stderr has a line containing `could not remove temporary
// directory` and the directory's path. Fails if the failure is silent.
func TestDiagramSVGContractReportsUnremovableTemporaryDirectory(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root removes entries of a directory without write permission")
	}
	e := dsvgSetup(t)
	t.Setenv(dsvgNodeModeEnv, "lockedtmp")
	t.Cleanup(func() {
		// Restore write permission so the test's own cleanup can remove
		// what the command could not.
		filepath.WalkDir(e.tmp, func(path string, entry fs.DirEntry, err error) error {
			if err == nil && entry.IsDir() {
				os.Chmod(path, 0o755)
			}
			return nil
		})
	})
	spec := dsvgWriteSpec(t, e.diagrams, "locked.json", `{"diagram_type": "flowchart-fake"}`)
	var err error
	stdout, stderr := dsvgCaptureStreams(t, func() {
		err = run(t.Context(), []string{"diagram-svg", "--root", e.root, "--timeout", "30s", spec})
	})
	nodes := dsvgReadRecords(t, e.records, "node")
	if len(nodes) != 1 || nodes[0].TmpDir == "" {
		t.Fatalf("node records %+v; error %v\nstdout: %s\nstderr: %s", nodes, err, stdout, stderr)
	}
	tmp := nodes[0].TmpDir
	if _, statErr := os.Lstat(filepath.Join(tmp, "locked")); statErr != nil {
		t.Skipf("the command removed %s anyway (%v), so no removal failure was provoked", tmp, statErr)
	}
	if !slices.ContainsFunc(strings.Split(stderr, "\n"), func(line string) bool {
		return strings.Contains(line, "could not remove temporary directory") && strings.Contains(line, tmp)
	}) {
		t.Errorf("stderr has no line with `could not remove temporary directory` and %s (error %v)\nstdout: %s\nstderr: %s", tmp, err, stdout, stderr)
	}
}

// failures (R7, removal failure is reported: brief retries, v6): a helper
// outside every process group (so the deadline's group kill cannot stop
// it) keeps creating files in the command's temporary directory after a
// finalize deadline, beside a subdirectory of many files, until the
// command's removal has got through that subdirectory; it then creates at
// most one more file and stops (dsvgRetryLitter). So the first removal
// attempt usually fails, because files appeared after it listed the
// directory, while at most one later attempt can be disturbed again. With
// brief retries (at least two) the command still removes the directory: it
// prints no `could not remove temporary directory` line, the TMPDIR it was
// started with is empty, and the helper stopped because of the removal.
// Fails if the command gives up after the first failed attempt (the
// warning appears and the directory stays). The test does not prove that an
// attempt was disturbed in a given run, since that depends on scheduling,
// so a command without retries fails it in most runs, not in all. Runs
// through dsvgDeadlineAttempts, since it needs the helper to be creating
// files before the deadline.
func TestDiagramSVGContractRetriesTemporaryDirectoryRemoval(t *testing.T) {
	dsvgDeadlineAttempts(t, 1500*time.Millisecond, func(timeout time.Duration) bool {
		e := dsvgSetup(t)
		t.Setenv(dsvgNodeModeEnv, "retrylitter")
		spec := dsvgWriteSpec(t, e.diagrams, "retry.json", `{"diagram_type": "flowchart-fake"}`)
		dsvgWriteFile(t, filepath.Join(e.diagrams, "retry.svg"), "<?xml previous export?>\n", 0o644)
		before := dsvgSnapshot(t, e.diagrams)
		var err error
		started := time.Now()
		stdout, stderr := dsvgCaptureStreams(t, func() {
			err = run(t.Context(), []string{"diagram-svg", "--root", e.root, "--timeout", timeout.String(), spec})
		})
		elapsed := time.Since(started)
		if err == nil || !strings.Contains(err.Error(), "timed out") {
			t.Errorf("error %v, want one containing `timed out`\nstdout: %s\nstderr: %s", err, stdout, stderr)
		}
		if limit := timeout + dsvgHoldBound; elapsed > limit {
			t.Errorf("run took %v (bound %v) with a helper outside node's process group", elapsed.Round(time.Millisecond), limit)
		}
		dsvgAssertSnapshot(t, "docs/diagrams", before, dsvgSnapshot(t, e.diagrams))
		// Ready only when the helper finished its subdirectory before the
		// deadline (the command's own deadline is no earlier than
		// started+timeout), so the removal began after that. Otherwise the
		// helper may still have been filling the subdirectory while the
		// command removed it, which the retry rule does not cover.
		ready := false
		if matches, _ := filepath.Glob(filepath.Join(e.records, "retrylitter-*.started")); len(matches) == 1 {
			if data, readErr := os.ReadFile(matches[0]); readErr == nil {
				if ns, parseErr := strconv.ParseInt(string(data), 10, 64); parseErr == nil {
					ready = time.Unix(0, ns).Before(started.Add(timeout))
				}
			}
		}
		pids := dsvgRecordedPids(e.records, "escaped")
		if !ready {
			for _, pid := range pids {
				syscall.Kill(pid, syscall.SIGKILL)
				for deadline := time.Now().Add(5 * time.Second); dsvgProcessAlive(pid) && time.Now().Before(deadline); {
					time.Sleep(20 * time.Millisecond)
				}
			}
			return false
		}
		// The helper stops on its own once the command's removal got
		// through its subdirectory; one still running means the command
		// never removed it.
		for _, pid := range pids {
			for deadline := time.Now().Add(10 * time.Second); dsvgProcessAlive(pid) && time.Now().Before(deadline); {
				time.Sleep(20 * time.Millisecond)
			}
			if dsvgProcessAlive(pid) {
				t.Errorf("the helper %d still creates files 10 s after run returned; the command did not remove its temporary directory", pid)
				syscall.Kill(pid, syscall.SIGKILL)
			}
		}
		matches, _ := filepath.Glob(filepath.Join(e.records, "retrylitter-*.stopped"))
		if len(matches) != 1 {
			t.Errorf("%d helper stop records, want 1", len(matches))
		} else if reason, readErr := os.ReadFile(matches[0]); readErr != nil || (string(reason) != dsvgRetryStoppedRemoval && string(reason) != dsvgRetryStoppedGone) {
			t.Errorf("the helper stopped with %q (%v), want it stopped by the command's removal", reason, readErr)
		}
		if slices.ContainsFunc(strings.Split(stderr, "\n"), func(line string) bool {
			return strings.Contains(line, "could not remove temporary directory")
		}) {
			t.Errorf("the command gave up removing its temporary directory although the helper made at most one file after the removal began\nstderr: %s", stderr)
		}
		dsvgAssertTmpEmpty(t, e)
		return ready
	})
}

// failures (R7, signals): the real main, run in a child process, cancels
// the run on SIGHUP, SIGINT and SIGTERM while finalize runs (and on SIGHUP
// while the browser is polled), each with a helper in the running child's
// process group: the process exits non-zero, writes nothing (the existing
// target is unchanged and nothing is reported as written), the helper is
// gone, TMPDIR is empty (R3) and an interrupted browser gets no
// Browser.close (R3). Fails if a signal ends cwdev without the group kill
// and cleanup.
func TestDiagramSVGContractMainCancelsRunOnSignal(t *testing.T) {
	for _, c := range []struct {
		name                  string
		nodeMode, browserMode string
		signal                syscall.Signal
	}{
		{"finalize-hangup", "helpersleep", "", syscall.SIGHUP},
		{"finalize-interrupt", "helpersleep", "", syscall.SIGINT},
		{"finalize-terminate", "helpersleep", "", syscall.SIGTERM},
		{"browser-hangup", "", "helperblank", syscall.SIGHUP},
	} {
		t.Run(c.name, func(t *testing.T) {
			e := dsvgSetup(t)
			t.Setenv(dsvgNodeModeEnv, c.nodeMode)
			t.Setenv(dsvgChromeModeEnv, c.browserMode)
			spec := dsvgWriteSpec(t, e.diagrams, "signal.json", `{"diagram_type": "flowchart-fake"}`)
			dsvgWriteFile(t, filepath.Join(e.diagrams, "signal.svg"), "<?xml previous export?>\n", 0o644)
			before := dsvgSnapshot(t, e.diagrams)
			executable, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			streams := dsvgTempDir(t)
			stdout, err := os.Create(filepath.Join(streams, "stdout"))
			if err != nil {
				t.Fatal(err)
			}
			defer stdout.Close()
			stderr, err := os.Create(filepath.Join(streams, "stderr"))
			if err != nil {
				t.Fatal(err)
			}
			defer stderr.Close()
			read := func(file *os.File) string {
				data, _ := os.ReadFile(file.Name())
				return string(data)
			}
			child := exec.Command(executable, "diagram-svg", "--root", e.root, "--timeout", "45s", spec)
			child.Env = append(os.Environ(), dsvgMainEnv+"=1")
			child.Dir = e.root
			child.Stdout, child.Stderr = stdout, stderr
			if err := child.Start(); err != nil {
				t.Fatal(err)
			}
			waited := make(chan struct{})
			go func() {
				child.Wait()
				close(waited)
			}()
			t.Cleanup(func() {
				select {
				case <-waited:
				default:
					child.Process.Kill()
					<-waited
				}
			})

			// Signal only once the helper runs (and, for the browser, once
			// the page is being polled).
			ready := func() bool {
				if len(dsvgRecordedPids(e.records, "helper")) == 0 {
					return false
				}
				if c.browserMode == "" {
					return true
				}
				for _, session := range dsvgReadProtocolQuiet(e.records) {
					if slices.ContainsFunc(session, func(command dsvgCommand) bool { return command.Method == "Runtime.evaluate" }) {
						return true
					}
				}
				return false
			}
			for limit := time.Now().Add(15 * time.Second); !ready(); {
				if time.Now().After(limit) {
					t.Fatalf("the child never reached the helper within 15 s\nstderr: %s", read(stderr))
				}
				select {
				case <-waited:
					t.Fatalf("cwdev ended before the signal (%v)\nstderr: %s", child.ProcessState, read(stderr))
				case <-time.After(20 * time.Millisecond):
				}
			}
			if err := child.Process.Signal(c.signal); err != nil {
				t.Fatal(err)
			}
			select {
			case <-waited:
			case <-time.After(15 * time.Second):
				t.Fatalf("cwdev still runs 15 s after %v; the run was not cancelled", c.signal)
			}
			if child.ProcessState.Success() {
				t.Errorf("cwdev exited 0 after %v, want a non-zero exit", c.signal)
			}
			if wrote := dsvgReportedPaths(read(stdout), "wrote "); len(wrote) != 0 {
				t.Errorf("cwdev reported writing %q after %v", wrote, c.signal)
			}
			dsvgAssertSnapshot(t, "docs/diagrams", before, dsvgSnapshot(t, e.diagrams))
			dsvgAssertHelpersGone(t, e.records)
			if c.browserMode != "" {
				for _, session := range dsvgReadProtocol(t, e.records) {
					if slices.ContainsFunc(session, func(command dsvgCommand) bool { return command.Method == "Browser.close" }) {
						t.Errorf("Browser.close was sent after %v", c.signal)
					}
				}
			}
			dsvgAssertTmpEmpty(t, e)
			if t.Failed() {
				t.Logf("exit: %v\nstdout: %s\nstderr: %s", child.ProcessState, read(stdout), read(stderr))
			}
		})
	}
}
