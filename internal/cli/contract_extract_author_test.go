package cli

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/belevtsev/agent-workflows/v3/internal/taskflow"
)

// Independent CLI contract tests for WP9 (packet revisions 1 and 2). Every test names
// the requirement IDs it covers. They do not call t.Parallel because the
// scaffold's test configuration is not known to be parallel-safe.

// contractAuthorCLISource declares Kind and Run.
const contractAuthorCLISource = "package pkg\n\n// Kind is a kind.\ntype Kind int\n\n// Run runs.\nfunc Run(x int) int {\n\treturn x + 1\n}\n"

// contractAuthorCLIDir returns a new empty directory with symlinks resolved.
func contractAuthorCLIDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

// contractAuthorCLIWrite writes data to the slash-separated path under dir.
func contractAuthorCLIWrite(t *testing.T, dir, path, data string) {
	t.Helper()
	full := filepath.Join(dir, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
}

// contractAuthorCLIModule returns a module root holding pkg/a.go.
func contractAuthorCLIModule(t *testing.T) string {
	t.Helper()
	root := contractAuthorCLIDir(t)
	contractAuthorCLIWrite(t, root, "go.mod", "module example.com/m\n\ngo 1.27\n")
	contractAuthorCLIWrite(t, root, "pkg/a.go", contractAuthorCLISource)
	return root
}

// contractAuthorCLIWant returns EncodeContract(ExtractContract(dir, symbols))
// for the parsed symbols value.
func contractAuthorCLIWant(t *testing.T, dir, value string) string {
	t.Helper()
	symbols, err := taskflow.ParseContractSymbols(value)
	if err != nil {
		t.Fatalf("ParseContractSymbols(%q): %v", value, err)
	}
	contract, err := taskflow.ExtractContract(dir, symbols)
	if err != nil {
		t.Fatalf("ExtractContract(%q, %q): %v", dir, symbols, err)
	}
	data, err := taskflow.EncodeContract(contract)
	if err != nil {
		t.Fatalf("EncodeContract: %v", err)
	}
	return string(data)
}

// TestContractAuthorCLIExtract covers C1: stdout is exactly the encoded
// extraction, an absolute --path is used as given, and no task is needed.
func TestContractAuthorCLIExtract(t *testing.T) {
	cases := []struct {
		name  string
		dir   func(t *testing.T) string
		value string
	}{
		{"plain symbols", func(t *testing.T) string { return filepath.Join(contractAuthorCLIModule(t), "pkg") }, "Kind,Run"},
		{"spaced symbols in another order", func(t *testing.T) string { return filepath.Join(contractAuthorCLIModule(t), "pkg") }, " Run , Kind"},
		{"absolute path through a symlink", func(t *testing.T) string {
			link := filepath.Join(contractAuthorCLIDir(t), "link")
			if err := os.Symlink(contractAuthorCLIModule(t), link); err != nil {
				t.Fatal(err)
			}
			return filepath.Join(link, "pkg")
		}, "Run"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := tc.dir(t)
			out, err := scaffoldContractExtract(t, "contract-extract", "--path", dir, "--symbols", tc.value)
			if err != nil {
				t.Fatalf("contract-extract: %v", err)
			}
			if want := contractAuthorCLIWant(t, dir, tc.value); out != want {
				t.Errorf("stdout differs from EncodeContract(ExtractContract)\n got  %q\n want %q", out, want)
			}
		})
	}
}

// TestContractAuthorCLIExtractErrors covers C2: each failure returns an error
// and writes nothing to stdout.
func TestContractAuthorCLIExtractErrors(t *testing.T) {
	root := contractAuthorCLIModule(t)
	pkg := filepath.Join(root, "pkg")
	empty := filepath.Join(root, "empty")
	contractAuthorCLIWrite(t, root, "empty/README.md", "no Go here\n")
	cases := []struct {
		name   string
		args   []string
		naming string
	}{
		{name: "missing path", args: []string{"--symbols", "Kind"}},
		{name: "missing symbols", args: []string{"--path", pkg}},
		{name: "empty symbol entry", args: []string{"--path", pkg, "--symbols", "Kind,,Run"}},
		{name: "invalid symbol", args: []string{"--path", pkg, "--symbols", "1A"}},
		{name: "repeated symbol", args: []string{"--path", pkg, "--symbols", "Kind,Kind"}},
		{name: "unknown symbol", args: []string{"--path", pkg, "--symbols", "Kind,Nope"}, naming: "Nope"},
		{name: "dir without Go source", args: []string{"--path", empty, "--symbols", "Kind"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := scaffoldContractExtract(t, append([]string{"contract-extract"}, tc.args...)...)
			if err == nil {
				t.Fatalf("contract-extract %q succeeded, want an error", tc.args)
			}
			if out != "" {
				t.Errorf("stdout = %q, want empty", out)
			}
			if tc.naming != "" && !strings.Contains(err.Error(), tc.naming) {
				t.Errorf("error %q does not name %s", err, tc.naming)
			}
		})
	}
}

// TestContractAuthorCLIRelativePath covers C3 (with C1): a relative --path is
// resolved against the command's configured working directory, not the test
// process's, whose working directory stays unchanged and has no such path.
func TestContractAuthorCLIRelativePath(t *testing.T) {
	if _, err := os.Stat("pkg"); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("precondition: the process working directory must have no pkg entry (stat error %v)", err)
	}
	before, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	root := contractAuthorCLIModule(t)
	pkg := filepath.Join(root, "pkg")
	cases := []struct{ name, workingDir, path string }{
		{"pkg under the module root", root, "pkg"},
		{"dot-slash pkg under the module root", root, "./pkg"},
		{"dot inside pkg", pkg, "."},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			const symbols = "Kind,Run"
			out, err := scaffoldContractExtractIn(t, tc.workingDir, "contract-extract", "--path", tc.path, "--symbols", symbols)
			if err != nil {
				t.Fatalf("contract-extract --path %s in %s: %v", tc.path, tc.workingDir, err)
			}
			if want := contractAuthorCLIWant(t, pkg, symbols); out != want {
				t.Errorf("stdout differs from EncodeContract(ExtractContract(%s))\n got  %q\n want %q", pkg, out, want)
			}
			if after, err := os.Getwd(); err != nil || after != before {
				t.Errorf("process working directory = %q (%v), want unchanged %q", after, err, before)
			}
		})
	}
}
