//go:build unix

package cli

import (
	"bytes"
	"context"
	json "encoding/json/v2"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"syscall"
	"testing"

	"golang.org/x/sys/unix"

	"github.com/belevtsev/agent-workflows/v3/internal/taskflow"
)

const (
	bundleLimitHelperArgs  = "CW_BUNDLE_LIMIT_HELPER_ARGS"
	bundleLimitHelperBytes = "CW_BUNDLE_LIMIT_HELPER_BYTES"
)

// TestTaskBundleSizeLimitHelperProcess is the child of
// TestTaskBundleWriteErrorLeavesNoFile; it does nothing in a normal run. It
// lowers its own file size limit (RLIMIT_FSIZE) and ignores SIGXFSZ, so a
// write past the limit fails with EFBIG, then runs cw task bundle in-process.
func TestTaskBundleSizeLimitHelperProcess(t *testing.T) {
	encoded := os.Getenv(bundleLimitHelperArgs)
	if encoded == "" {
		return
	}
	var args []string
	if err := json.Unmarshal([]byte(encoded), &args); err != nil {
		fmt.Fprintln(os.Stderr, "bad helper args:", err)
		os.Exit(3)
	}
	limit, err := strconv.ParseUint(os.Getenv(bundleLimitHelperBytes), 10, 64)
	if err != nil {
		fmt.Fprintln(os.Stderr, "bad helper limit:", err)
		os.Exit(3)
	}
	signal.Ignore(syscall.SIGXFSZ)
	var rlimit unix.Rlimit
	if err := unix.Getrlimit(unix.RLIMIT_FSIZE, &rlimit); err != nil {
		fmt.Fprintln(os.Stderr, "read the file size limit:", err)
		os.Exit(3)
	}
	rlimit.Cur = limit
	if err := unix.Setrlimit(unix.RLIMIT_FSIZE, &rlimit); err != nil {
		fmt.Fprintln(os.Stderr, "lower the file size limit:", err)
		os.Exit(3)
	}
	err = Execute(context.Background(), args, Config{Out: os.Stdout, Err: os.Stderr, Getenv: func(string) string { return "" }})
	if err == nil {
		fmt.Fprintln(os.Stderr, "cw task bundle succeeded under the file size limit")
		os.Exit(4)
	}
	fmt.Fprintln(os.Stderr, err)
	os.Exit(ExitCode(err))
}

// A bundle write that fails part way through leaves no file at --output, no
// temporary file beside it and the task record unchanged. The child's file
// size limit is half the bundle's size, so writing the bundle fails with EFBIG
// after the first half, as it would on a full disk or over a quota.
//
// Oracle: the child exits non-zero naming EFBIG and prints nothing on stdout;
// afterwards the output directory is empty and the task directory holds the
// same files as before.
func TestTaskBundleWriteErrorLeavesNoFile(t *testing.T) {
	h := newTaskBundleHarness(t)
	h.write(t, "source.txt", "changed source\n")
	bundle, err := taskflow.BuildBundle(t.Context(), h.record.Directory, "review1")
	if err != nil {
		t.Fatal(err)
	}
	data, err := taskflow.EncodeBundle(bundle)
	if err != nil {
		t.Fatal(err)
	}
	limit := len(data) / 2
	if limit == 0 {
		t.Fatalf("bundle of %d bytes is too small to fail half way", len(data))
	}
	before := taskBundleFiles(t, h.record.Directory)
	dir := taskBundleDir(t)
	path := filepath.Join(dir, "bundle.json")
	args, err := json.Marshal([]string{"task", "bundle", "--task", h.record.Directory, "--review-id", "review1", "--output", path})
	if err != nil {
		t.Fatal(err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	child := exec.Command(executable, "-test.run=^TestTaskBundleSizeLimitHelperProcess$", "-test.count=1")
	child.Env = append(os.Environ(), bundleLimitHelperArgs+"="+string(args), bundleLimitHelperBytes+"="+strconv.Itoa(limit))
	var stdout, stderr bytes.Buffer
	child.Stdout, child.Stderr = &stdout, &stderr
	err = child.Run()
	if _, failed := errors.AsType[*exec.ExitError](err); !failed {
		t.Fatalf("child: %v, stderr %q; want cw task bundle to fail", err, stderr.String())
	}
	if !strings.Contains(stderr.String(), syscall.EFBIG.Error()) {
		t.Fatalf("child failed without a write error (%v): stderr %q", err, stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("failed bundle printed %q", stdout.String())
	}
	if info, statErr := os.Lstat(path); !errors.Is(statErr, fs.ErrNotExist) {
		size := int64(-1)
		if info != nil {
			size = info.Size()
		}
		t.Fatalf("--output after a failed write: %v, %d of %d bytes; want no file", statErr, size, len(data))
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		names := []string{}
		for _, entry := range entries {
			names = append(names, entry.Name())
		}
		t.Fatalf("output directory holds %v after a failed write; want nothing", names)
	}
	if after := taskBundleFiles(t, h.record.Directory); !reflect.DeepEqual(after, before) {
		t.Fatal("failed bundle changed the task directory")
	}
}
