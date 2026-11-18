package installers

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

const preparedVersion = "v0.0.1"

// The archived test executable acts as a native release binary on every OS.
func TestMain(m *testing.M) {
	if len(os.Args) == 2 && os.Args[1] == "version" {
		version := os.Getenv("KITSU_INSTALL_TEST_VERSION")
		if version == "" {
			version = preparedVersion
		}
		fmt.Printf("kitsu %s\nrevision fixture\n", version)
		os.Exit(0)
	}
	if len(os.Args) > 1 && os.Args[1] == "agent" {
		fmt.Printf("fixture agent arguments %q\n", os.Args[1:])
		os.Exit(23)
	}
	os.Exit(m.Run())
}

type installer struct{ name, program, script string }

func installerChoices() []installer {
	if runtime.GOOS == "windows" {
		return []installer{{name: "powershell", script: "install.ps1"}, {name: "pwsh", script: "install.ps1"}}
	}
	return []installer{{name: "sh", script: "install.sh"}}
}

func nativeInstallers(t *testing.T) []installer {
	t.Helper()
	var all []installer
	for _, shell := range installerChoices() {
		if program, err := exec.LookPath(shell.name); err == nil {
			shell.program = program
			all = append(all, shell)
		}
	}
	if len(all) == 0 {
		t.Fatalf("no supported installer shell on %s/%s", runtime.GOOS, runtime.GOARCH)
	}
	return all
}

func binaryName() string {
	if runtime.GOOS == "windows" {
		return "kitsu.exe"
	}
	return "kitsu"
}

func defaultInstallTarget(home string) string {
	if runtime.GOOS == "windows" {
		return filepath.Join(home, "AppData", "Local", "kitsu", binaryName())
	}
	return filepath.Join(home, ".local", "bin", binaryName())
}

func archiveName(version string) string {
	extension := ".tar.gz"
	if runtime.GOOS == "windows" {
		extension = ".zip"
	}
	return fmt.Sprintf("kitsu_%s_%s_%s%s", version, runtime.GOOS, runtime.GOARCH, extension)
}

func archiveBytes(t *testing.T, files map[string][]byte) []byte {
	t.Helper()
	var output bytes.Buffer
	if runtime.GOOS == "windows" {
		writer := zip.NewWriter(&output)
		for name, data := range files {
			file, err := writer.Create(name)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := file.Write(data); err != nil {
				t.Fatal(err)
			}
		}
		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}
		return output.Bytes()
	}
	compressed := gzip.NewWriter(&output)
	writer := tar.NewWriter(compressed)
	for name, data := range files {
		if err := writer.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(data))}); err != nil {
			t.Fatal(err)
		}
		if _, err := writer.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := compressed.Close(); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

// The damaged-checksum fixture must still unpack, so a missing checksum
// verification cannot be hidden by an unrelated archive-format failure.
func assertReadableArchive(t *testing.T, data []byte) {
	t.Helper()
	if runtime.GOOS == "windows" {
		archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range archive.File {
			file, err := entry.Open()
			if err != nil {
				t.Fatal(err)
			}
			_, err = io.CopyN(io.Discard, file, 128<<20)
			_ = file.Close()
			if !errors.Is(err, io.EOF) {
				t.Fatalf("fixture member is invalid or exceeds 128 MiB: %v", err)
			}
		}
		return
	}
	compressed, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = compressed.Close() }()
	archive := tar.NewReader(compressed)
	for {
		if _, err := archive.Next(); errors.Is(err, io.EOF) {
			return
		} else if err != nil {
			t.Fatal(err)
		}
		if _, err := io.CopyN(io.Discard, archive, 128<<20); !errors.Is(err, io.EOF) {
			t.Fatalf("fixture member is invalid or exceeds 128 MiB: %v", err)
		}
	}
}

func fixtureAssets(t *testing.T, version string) map[string][]byte {
	t.Helper()
	program, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	data := readFile(t, program)
	archive := archiveBytes(t, map[string][]byte{binaryName(): data, "LICENSE": []byte("fixture")})
	name := archiveName(version)
	return map[string][]byte{name: archive, "SHA256SUMS": checksumLine(name, archive)}
}

func checksumLine(name string, data []byte) []byte {
	return fmt.Appendf(nil, "%x  %s\n", sha256.Sum256(data), name)
}

func readFile(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func writeFile(t *testing.T, name string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(name), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(name, data, 0o755); err != nil {
		t.Fatal(err)
	}
}

func serveAssets(t *testing.T, assets map[string][]byte) string {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if data, ok := assets[strings.TrimPrefix(r.URL.Path, "/")]; ok {
			_, _ = w.Write(data)
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(server.Close)
	return server.URL
}

// The shell sees only ordinary installer dependencies, never Go or Git.
func installerPath(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		return filepath.Join(os.Getenv("SystemRoot"), "System32")
	}
	dir := t.TempDir()
	for _, name := range strings.Fields("sh curl tar gzip uname sysctl mktemp mkdir chmod mv rm sed grep tr cat cp dirname awk head wc sort cut touch basename readlink") {
		if program, err := exec.LookPath(name); err == nil {
			if err := os.Symlink(program, filepath.Join(dir, name)); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, name := range []string{"sha256sum", "shasum"} {
		if program, err := exec.LookPath(name); err == nil {
			if err := os.Symlink(program, filepath.Join(dir, name)); err != nil {
				t.Fatal(err)
			}
			break
		}
	}
	return dir
}

type installRun struct {
	t                 *testing.T
	host              installer
	dir, home, target string
	env               map[string]string
}

func newInstallRun(t *testing.T, host installer, base string) *installRun {
	t.Helper()
	dir := t.TempDir()
	if canonical, err := filepath.EvalSymlinks(dir); err == nil {
		dir = canonical
	}
	home := filepath.Join(dir, "user home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	return &installRun{t: t, host: host, dir: dir, home: home, target: defaultInstallTarget(home), env: map[string]string{
		"HOME": home, "USERPROFILE": home, "LOCALAPPDATA": filepath.Join(home, "AppData", "Local"),
		"PATH": installerPath(t), "SHELL": "/bin/zsh", "KITSU_DOWNLOAD_BASE": base,
		"KITSU_NO_MODIFY_PATH": "1", "KITSU_INSTALL_TEST_VERSION": preparedVersion,
	}}
}

func (r *installRun) environment() []string {
	env := []string{}
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		upper := strings.ToUpper(key)
		if _, replaced := r.env[upper]; replaced || strings.HasPrefix(upper, "KITSU_") ||
			strings.HasPrefix(upper, "GIT_") || strings.HasPrefix(upper, "XDG_") || upper == "ZDOTDIR" {
			continue
		}
		env = append(env, entry)
	}
	for key, value := range r.env {
		env = append(env, key+"="+value)
	}
	return env
}

func (r *installRun) run(piped bool, args ...string) (string, error) {
	r.t.Helper()
	source, err := filepath.Abs(filepath.Join("..", "..", r.host.script))
	if err != nil {
		r.t.Fatal(err)
	}
	// Copy to a clean directory; installation has no checkout to depend on.
	script := filepath.Join(r.dir, r.host.script)
	writeFile(r.t, script, readFile(r.t, source))
	return r.runScript(script, piped, args...)
}

func (r *installRun) runScript(script string, piped bool, args ...string) (string, error) {
	r.t.Helper()
	return invokeInstaller(r.t, r.host, r.dir, r.environment(), script, piped, args...)
}

func invokeInstaller(t *testing.T, host installer, dir string, env []string, script string, piped bool, args ...string) (string, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	var command *exec.Cmd
	if runtime.GOOS == "windows" {
		prefix := []string{"-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass"}
		if piped {
			env = append(env, "KITSU_TEST_INSTALLER_SCRIPT="+script)
			prefix = append(prefix, "-Command", "Get-Content -LiteralPath $env:KITSU_TEST_INSTALLER_SCRIPT -Raw | Invoke-Expression")
		} else {
			prefix = append(prefix, "-File", script)
			prefix = append(prefix, args...)
		}
		command = exec.CommandContext(ctx, host.program, prefix...)
	} else if piped {
		command = exec.CommandContext(ctx, host.program, append([]string{"-s", "--"}, args...)...)
		command.Stdin = bytes.NewReader(readFile(t, script))
	} else {
		command = exec.CommandContext(ctx, host.program, append([]string{script}, args...)...)
	}
	command.Dir, command.Env = dir, env
	command.WaitDelay = 3 * time.Second
	output, err := command.CombinedOutput()
	return string(output), err
}

func (r *installRun) installedVersion() string {
	r.t.Helper()
	command := exec.CommandContext(r.t.Context(), r.target, "version")
	command.Env = r.environment()
	output, err := command.CombinedOutput()
	if err != nil {
		r.t.Fatalf("installed binary could not run: %v\n%s", err, output)
	}
	return string(output)
}

func fetchReleaseAsset(t *testing.T, base, name string, limit int64) []byte {
	t.Helper()
	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, base+"/"+name, nil)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Timeout: 30 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("download %s returned %s", name, response.Status)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		t.Fatal(err)
	}
	if int64(len(data)) > limit {
		t.Fatalf("download %s exceeds %d bytes", name, limit)
	}
	return data
}
