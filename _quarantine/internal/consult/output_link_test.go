package consult

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

// fxNoLink makes link fail with err for the rest of the test. The test must
// not be parallel: link is a package variable.
func fxNoLink(t *testing.T, err error) {
	t.Helper()
	saved := link
	link = func(string, string) error { return &os.LinkError{Op: "link", Err: err} }
	t.Cleanup(func() { link = saved })
}

// Oracle: on a file system without hard links (link fails with an error other
// than "exists"), the run still writes the record at --output, private
// (0600), and leaves no temporary file beside it.
func TestConsultWritesTheOutputWhereHardLinksAreUnsupported(t *testing.T) {
	fxNoLink(t, errors.ErrUnsupported)
	request, output := fxFiles(t, fxSample)
	result := fxRun(t, Options{Request: request, Output: output, Policy: fxPolicy("all"), Getenv: fxEnv(fxProviders...), DryRun: true})
	if result.code != Complete {
		t.Fatalf("run = %+v, want exit 0", result)
	}
	if record := fxRecordAt(t, output); record.get("status") != "dry_run" {
		t.Fatalf("output holds status %v, want dry_run", record.get("status"))
	}
	info, err := os.Stat(output)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("output mode %v (%v), want 0600", info, err)
	}
	if entries, _ := os.ReadDir(filepath.Dir(output)); len(entries) != 2 {
		t.Fatalf("directory holds %v, want the request and the record", entries)
	}
}

// Oracle: a link that fails because the output appeared meanwhile is the
// refusal "cannot create a new output file"; the file there is not replaced.
func TestConsultRefusesAnOutputThatAppearsDuringTheRun(t *testing.T) {
	fxNoLink(t, fs.ErrExist)
	request, output := fxFiles(t, fxSample)
	var stdout, stderr bytes.Buffer
	code, err := Run(t.Context(), Options{Request: request, Output: output, Policy: fxPolicy("all"), Getenv: fxEnv(fxProviders...), DryRun: true,
		Stdout: &stdout, Stderr: &stderr})
	if code != Invalid || !errors.Is(err, errOutputExists) {
		t.Fatalf("run = %d, %v; want %d and %v", code, err, Invalid, errOutputExists)
	}
	if _, err := os.Lstat(output); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("refused run left %s: %v", output, err)
	}
	if entries, _ := os.ReadDir(filepath.Dir(output)); len(entries) != 1 {
		t.Fatalf("refused run left %v beside the request", entries)
	}
}
