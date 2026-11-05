package manageruntime

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/v2"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func writeFixture(t *testing.T, path, contents string, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), mode); err != nil {
		t.Fatal(err)
	}
}
func managerScript(protocol bool) string {
	if protocol {
		return currentManagerScriptIdentity(fixtureSHA)
	}
	return managerScriptIdentity("", fixtureSHA)
}
func currentManagerScriptIdentity(revision string) string {
	return managerScriptIdentity("cw-manager-v10", revision)
}
func managerScriptIdentity(protocol, revision string) string {
	identity := fmt.Sprintf(`{"arch":"%s","built_at":"unknown","os":"%s","revision":"%s","version":"v1.2.3"}`, runtime.GOARCH, runtime.GOOS, revision)
	bundle := fmt.Sprintf(`{"version":"v1.2.3","revision":"%s","os":"%s","arch":"%s","base_digest":"%s"}`, revision, runtime.GOOS, runtime.GOARCH, strings.Repeat("b", 64))
	return fmt.Sprintf(`#!/bin/sh
set -eu
record() { if [ -n "${CW_MOCK_CHILD_CALLS-}" ]; then printf '%%s\n' "$1" >> "$CW_MOCK_CHILD_CALLS"; fi; }
case ${1-} in
 version) record version; printf '%%s\n' '%s'; exit 0 ;;
 --manager-protocol) record protocol; [ -n '%s' ] || exit 2; printf '%%s\n' '%s'; exit 0 ;;
 --bundle-identity) record bundle; printf '%%s\n' '%s'; exit 0 ;;
 --verify-release) [ "$#" -eq 2 ] && [ "$2" = v1.2.3 ] || exit 24; record "verified $2"; exit "${CW_MOCK_VERIFY_EXIT-0}" ;;
esac
record executed
if [ -n "${CW_MOCK_RUNNING-}" ]; then printf '%%s\n' "$0" > "$CW_MOCK_RUNNING"; fi
if [ "${CW_MOCK_HOLD-0}" = 1 ]; then while [ ! -e "$CW_MOCK_GATE" ]; do sleep 0.02; done; fi
if [ "${CW_MOCK_STREAMS-0}" = 1 ]; then
 printf '%%s\000' "$@" > "$CW_MOCK_ARGS"
 IFS= read -r input
 printf '%%s\n' "$input"
 printf '%%s\n' fixture-error >&2
else
 printf '%%s\n' '{"forwarded":true}'
fi
exit "${CW_MOCK_CHILD_EXIT-0}"
`, identity, protocol, protocol, bundle)
}

// R-BS: every payload, archive and call record is under this test's private
// TMPDIR. The launcher receives only synthetic HOME and an explicit clean env.
// The directory holding install.sh contains nothing else: install.sh has no
// checkout cache (.bin/cw) or checkout lock (.bin.lock) any more.
func bootstrapFixture(t *testing.T) (string, string) {
	t.Helper()
	root := realTemp(t)
	checkout := filepath.Join(root, "source with spaces")
	if err := os.MkdirAll(checkout, 0755); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile("../../install.sh")
	if err != nil {
		t.Fatal(err)
	}
	launcher := filepath.Join(checkout, "install.sh")
	writeFixture(t, launcher, string(data), 0755)
	mockBin := filepath.Join(root, "mock-bin")
	if err := os.Mkdir(mockBin, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "synthetic home"), 0700); err != nil {
		t.Fatal(err)
	}
	writeFixture(t, filepath.Join(mockBin, "curl"), `#!/bin/sh
set -eu
output= address= writeout= next= head=false
for argument in "$@"; do
 if [ -n "$next" ]; then case $next in output) output=$argument ;; writeout) writeout=$argument ;; esac; next=; continue; fi
 case $argument in
  -o|--output) next=output ;; -w|--write-out) next=writeout ;;
  --output=*) output=${argument#*=} ;; --write-out=*) writeout=${argument#*=} ;;
  -o?*) output=${argument#-o} ;; -w?*) writeout=${argument#-w} ;;
  -I|--head|-*I*) head=true ;; https:*) address=$argument ;;
 esac
done
method=GET; if "$head"; then method=HEAD; fi
printf '%s %s\n' "$method" "$address" >> "$CW_MOCK_CALLS"
case $address in
 https://github.com/belevtsev/agent-workflows/releases/latest)
  [ "${CW_MOCK_FAIL-}" != latest ] || exit 22
  "$head" || exit 90; case $writeout in *'%{url_effective}'*) ;; *) exit 90 ;; esac
  printf '%s' "$CW_MOCK_LATEST" ;;
 https://github.com/belevtsev/agent-workflows/releases/download/v1.2.3/SHA256SUMS)
  [ "${CW_MOCK_FAIL-}" != checksum ] || exit 22; cp "$CW_MOCK_SUMS" "$output" ;;
 https://github.com/belevtsev/agent-workflows/releases/download/v1.2.3/cw_*.tar.gz)
  [ "${CW_MOCK_FAIL-}" != archive ] || exit 22; cp "$CW_MOCK_ARCHIVE" "$output" ;;
 *) exit 91 ;;
esac
`, 0755)
	machine, system := "x86_64", "Linux"
	if runtime.GOOS == "darwin" {
		system = "Darwin"
	}
	if runtime.GOARCH == "arm64" {
		machine = "aarch64"
		if runtime.GOOS == "darwin" {
			machine = "arm64"
		}
	}
	writeFixture(t, filepath.Join(mockBin, "uname"), fmt.Sprintf("#!/bin/sh\nprintf '%%s\\n' \"$*\" >> \"$CW_MOCK_UNAME_CALLS\"\ncase ${1-} in -s) printf '%%s\\n' '%s' ;; -m) printf '%%s\\n' '%s' ;; *) exit 92 ;; esac\n", system, machine), 0755)
	for _, tool := range []string{"git", "go"} {
		writeFixture(t, filepath.Join(mockBin, tool), "#!/bin/sh\nprintf '%s\\n' forbidden >> \"$CW_MOCK_FORBIDDEN\"\nexit 97\n", 0755)
	}
	for name, value := range map[string]string{
		"CW_BOOTSTRAP_ROOT": root, "CW_MOCK_CALLS": filepath.Join(root, "calls"), "CW_MOCK_UNAME_CALLS": filepath.Join(root, "uname-calls"),
		"CW_MOCK_CHILD_CALLS": filepath.Join(root, "child-calls"), "CW_MOCK_FORBIDDEN": filepath.Join(root, "forbidden"),
		"CW_MOCK_KILL_PID_FILE": filepath.Join(root, "kill-parent-pid"), "CW_MOCK_RUNNING": filepath.Join(root, "running"), "CW_MOCK_GATE": filepath.Join(root, "gate"), "CW_MOCK_ARGS": filepath.Join(root, "args"),
		"CW_MOCK_LATEST": "https://github.com/belevtsev/agent-workflows/releases/tag/v1.2.3", "CW_MOCK_FAIL": "", "CW_MOCK_VERIFY_EXIT": "0",
		"CW_MOCK_HOLD": "0", "CW_MOCK_STREAMS": "0", "CW_MOCK_CHILD_EXIT": "0",
	} {
		t.Setenv(name, value)
	}
	bootstrapRelease(t, root, managerScript(true))
	return launcher, filepath.Join(root, "calls")
}
func bootstrapRelease(t *testing.T, root, script string) {
	t.Helper()
	bootstrapArchive(t, root, archiveFixture(t, []tar.Header{{Name: "cw", Typeflag: tar.TypeReg, Mode: 0755}}, []string{script}))
}
func bootstrapArchive(t *testing.T, root string, archive []byte) {
	t.Helper()
	archivePath := filepath.Join(root, "release.tar.gz")
	if err := os.WriteFile(archivePath, archive, 0644); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(archive)
	sums := filepath.Join(root, "SHA256SUMS")
	writeFixture(t, sums, fmt.Sprintf("%s  cw_%s_%s.tar.gz\n", hex.EncodeToString(sum[:]), runtime.GOOS, runtime.GOARCH), 0644)
	t.Setenv("CW_MOCK_SUMS", sums)
	t.Setenv("CW_MOCK_ARCHIVE", archivePath)
}
func bootstrapCommand(ctx context.Context, launcher string, args ...string) *exec.Cmd {
	command := exec.CommandContext(ctx, launcher, args...)
	root := filepath.Dir(filepath.Dir(launcher))
	command.Dir = filepath.Dir(launcher)
	command.Env = []string{"PATH=" + filepath.Join(root, "mock-bin") + ":/usr/bin:/bin:/usr/sbin:/sbin", "HOME=" + filepath.Join(root, "synthetic home"), "TMPDIR=" + root, "LC_ALL=C"}
	for _, key := range []string{"CW_MOCK_CALLS", "CW_MOCK_UNAME_CALLS", "CW_MOCK_CHILD_CALLS", "CW_MOCK_FORBIDDEN", "CW_MOCK_KILL_PID_FILE", "CW_MOCK_RUNNING", "CW_MOCK_GATE", "CW_MOCK_ARGS", "CW_MOCK_LATEST", "CW_MOCK_FAIL", "CW_MOCK_VERIFY_EXIT", "CW_MOCK_HOLD", "CW_MOCK_STREAMS", "CW_MOCK_CHILD_EXIT", "CW_MOCK_SUMS", "CW_MOCK_ARCHIVE"} {
		command.Env = append(command.Env, key+"="+os.Getenv(key))
	}
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	command.Cancel = func() error {
		err := syscall.Kill(-command.Process.Pid, syscall.SIGTERM)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
	command.WaitDelay = 2 * time.Second
	return command
}
func runBootstrap(ctx context.Context, launcher string, args ...string) (string, error) {
	stdout, stderr, err := bootstrapStreams(ctx, launcher, args...)
	return stdout + stderr, err
}
func bootstrapStreams(ctx context.Context, launcher string, args ...string) (string, string, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	command := bootstrapCommand(ctx, launcher, args...)
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	err := command.Run()
	return stdout.String(), stderr.String(), err
}
func bootstrapTree(t *testing.T, root string) map[string]string {
	t.Helper()
	result := map[string]string{}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		state := info.Mode().String()
		if info.Mode().IsRegular() {
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			state += ":" + string(data)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}
			state += ":" + target
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		result[relative] = state
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}
func assertBootstrapPinnedRequests(t *testing.T, calls string) {
	t.Helper()
	data, err := os.ReadFile(calls)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	prefix := "https://github.com/belevtsev/agent-workflows/releases/"
	want := map[string]int{"HEAD " + prefix + "latest": 1, "GET " + prefix + "download/v1.2.3/SHA256SUMS": 1, "GET " + prefix + "download/v1.2.3/cw_" + runtime.GOOS + "_" + runtime.GOARCH + ".tar.gz": 1}
	for _, line := range lines {
		want[line]--
	}
	if len(lines) != 3 {
		t.Fatalf("release pinning made %d requests: %s", len(lines), data)
	}
	for request, remaining := range want {
		if remaining != 0 {
			t.Fatalf("latest must resolve once and both assets must stay pinned: %q delta=%d; calls=%s", request, remaining, data)
		}
	}
}

// assertBootstrapCleaned requires that no private stage survives, that Git
// and Go were never invoked, and that the launcher directory still holds only
// install.sh: no .bin cache, .bin.lock or .bootstrap.* stage is created there.
func assertBootstrapCleaned(t *testing.T, launcher string) {
	t.Helper()
	root := filepath.Dir(filepath.Dir(launcher))
	stages, err := filepath.Glob(filepath.Join(root, "cw-bootstrap.*"))
	if err != nil || len(stages) != 0 {
		t.Fatalf("private bootstrap stages remain: %v %v", stages, err)
	}
	entries, err := os.ReadDir(filepath.Dir(launcher))
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	if !slices.Equal(names, []string{"install.sh"}) {
		t.Fatalf("bootstrap wrote next to install.sh: %v", names)
	}
	if _, err := os.Lstat(filepath.Join(root, "forbidden")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("bootstrap invoked Git or Go: %v", err)
	}
}
func bootstrapOwnedInstallation(t *testing.T, root string) (string, map[string]string, map[string]string) {
	t.Helper()
	home := filepath.Join(root, "installed home")
	state := filepath.Join(home, ".local", "state", "agent-workflows")
	owned := map[string]string{
		filepath.Join(home, ".codex", "config.toml"): "model = \"fixture\"\n", filepath.Join(home, ".codex", "AGENTS.md"): "fixture instructions\n",
		filepath.Join(state, "state.json"): "{\"fixture\":\"owned state\"}\n", filepath.Join(state, "journal.json"): "{\"fixture\":\"pending journal\"}\n", filepath.Join(home, ".bashrc"): "export PATH=\"fixture\"\n",
	}
	for path, contents := range owned {
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		writeFixture(t, path, contents, 0600)
	}
	links := map[string]string{
		filepath.Join(home, ".agents", "skills", "task-orchestration"): filepath.Join(root, "active skills", "task-orchestration"),
		filepath.Join(home, ".local", "bin", "cw"):                     filepath.Join(state, "runtime", "current", "cw"),
		filepath.Join(state, "current"):                                filepath.Join(root, "active release"), filepath.Join(state, "runtime", "current"): filepath.Join(root, "active manager"),
	}
	for path, target := range links {
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, path); err != nil {
			t.Fatal(err)
		}
	}
	return home, owned, links
}
func assertBootstrapOwnedInstallation(t *testing.T, owned, links map[string]string) {
	t.Helper()
	for path, contents := range owned {
		data, err := os.ReadFile(path)
		if err != nil || string(data) != contents {
			t.Fatalf("owned installation changed at %s: %v", path, err)
		}
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm() != 0600 {
			t.Fatalf("owned permissions changed at %s: %v", path, err)
		}
	}
	for path, target := range links {
		actual, err := os.Readlink(path)
		if err != nil || actual != target {
			t.Fatalf("owned link changed at %s: %v: %q", path, err, actual)
		}
	}
}
func bootstrapSuccess(t *testing.T, launcher string, args ...string) {
	t.Helper()
	before := bootstrapTree(t, filepath.Dir(launcher))
	stdout, stderr, err := bootstrapStreams(t.Context(), launcher, args...)
	if after := bootstrapTree(t, filepath.Dir(launcher)); !reflect.DeepEqual(after, before) {
		t.Fatal("bootstrap changed the launcher directory")
	}
	if err != nil || strings.TrimSpace(stdout) != `{"forwarded":true}` || stderr != "" {
		t.Fatalf("verified child did not run: %v stdout=%q stderr=%q", err, stdout, stderr)
	}
	assertBootstrapPinnedRequests(t, filepath.Join(filepath.Dir(filepath.Dir(launcher)), "calls"))
	assertBootstrapCleaned(t, launcher)
	steps, err := os.ReadFile(filepath.Join(filepath.Dir(filepath.Dir(launcher)), "child-calls"))
	verified, executed := strings.Index(string(steps), "verified v1.2.3\n"), strings.Index(string(steps), "executed\n")
	if err != nil || verified < 0 || executed <= verified {
		t.Fatalf("child ran without pinned release verification: %s %v", steps, err)
	}
	for _, metadata := range []string{"version\n", "protocol\n", "bundle\n"} {
		index := strings.Index(string(steps), metadata)
		if index < 0 || index >= verified {
			t.Fatalf("missing native metadata before verification: %s", steps)
		}
	}
}

// R-BS: every run acquires a fresh pinned cw-manager-v10 release; nothing is
// cached or locked next to install.sh.
func TestProductionBootstrapColdV10Download(t *testing.T) {
	launcher, _ := bootstrapFixture(t)
	bootstrapSuccess(t, launcher, "install")
}

// R4: install.sh help describes only the standalone manager.
var bootstrapRemovedHelp = []*regexp.Regexp{
	regexp.MustCompile(`\bvalidate\b`),
	regexp.MustCompile(`--migrate-from`),
	regexp.MustCompile(`--source\b`),
	regexp.MustCompile(`--no-checkout\b`),
	regexp.MustCompile(`developer source compatibility`),
}

// R-BS / R4: dry-run boolean forms and help are offline: no downloads, child
// runs or persistent writes, and help omits the removed source-mode surface.
func TestBootstrapColdDryRunAndHelpAreOffline(t *testing.T) {
	for _, argument := range []string{"--dry-run", "--dry-run=1", "--dry-run=t", "--dry-run=T", "--dry-run=true", "--dry-run=TRUE", "--dry-run=True", "--help", "help"} {
		t.Run(argument, func(t *testing.T) {
			launcher, calls := bootstrapFixture(t)
			root := filepath.Dir(filepath.Dir(launcher))
			home, owned, links := bootstrapOwnedInstallation(t, root)
			before := bootstrapTree(t, root)
			args := []string{"install", "--home", home, argument}
			if argument == "help" {
				args = []string{argument}
			}
			stdout, stderr, err := bootstrapStreams(t.Context(), launcher, args...)
			if err != nil || stderr != "" {
				t.Fatalf("offline bootstrap: %v stdout=%q stderr=%q", err, stdout, stderr)
			}
			if argument == "help" || argument == "--help" {
				if !strings.Contains(stdout, "Usage: install.sh") {
					t.Fatalf("standalone bootstrap help missing: %q", stdout)
				}
				for _, removed := range bootstrapRemovedHelp {
					if removed.MatchString(stdout) {
						t.Fatalf("help still lists removed surface %s: %q", removed, stdout)
					}
				}
			} else {
				// R-BS / bootstrap-native-v4: compare the standalone wire object,
				// including exact fields and values, without requiring JSON key order.
				want := map[string]any{"dry_run": true, "environment": map[string]any{"kind": "standalone", "status": "deferred"}, "next_step": "Run the bootstrap without --dry-run to prepare a verified standalone manager", "validation": "deferred"}
				var got map[string]any
				if err := json.Unmarshal([]byte(stdout), &got); err != nil || !reflect.DeepEqual(got, want) {
					t.Fatalf("standalone dry-run wire differs: %q: %v", stdout, err)
				}
			}
			if after := bootstrapTree(t, root); !reflect.DeepEqual(after, before) {
				t.Fatal("offline bootstrap made persistent writes")
			}
			assertBootstrapOwnedInstallation(t, owned, links)
			for _, path := range []string{calls, filepath.Join(root, "uname-calls"), filepath.Join(root, "child-calls")} {
				if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("offline bootstrap invoked or created %s: %v", path, err)
				}
			}
			assertBootstrapCleaned(t, launcher)
		})
	}
}

// R-BS: install.sh accepts only a published release whose manager reports
// exactly cw-manager-v10; every other protocol is refused before the child runs.
func TestBootstrapRejectsUnsupportedReleasesWithoutChangingOwnedInstallation(t *testing.T) {
	for _, protocol := range []string{"cw-manager-v1", "cw-manager-v2", "cw-manager-v3", "cw-manager-v4", "cw-manager-v5", "cw-manager-v6", "cw-manager-v7", "cw-manager-v8", "cw-manager-v9", "cw-manager-v11", "cw-manager-v100", "unknown"} {
		t.Run(protocol, func(t *testing.T) {
			launcher, calls := bootstrapFixture(t)
			root := filepath.Dir(filepath.Dir(launcher))
			home, owned, links := bootstrapOwnedInstallation(t, root)
			bootstrapRelease(t, root, managerScriptIdentity(protocol, fixtureSHA))
			before := bootstrapTree(t, filepath.Dir(launcher))
			stdout, stderr, err := bootstrapStreams(t.Context(), launcher, "install", "--home", home)
			if after := bootstrapTree(t, filepath.Dir(launcher)); !reflect.DeepEqual(after, before) {
				t.Fatal("refused release changed the launcher directory")
			}
			if err == nil || stdout != "" || !strings.Contains(stderr, "cw-manager-v10") {
				t.Fatalf("unsupported release accepted: %v stdout=%q stderr=%q", err, stdout, stderr)
			}
			assertBootstrapOwnedInstallation(t, owned, links)
			assertBootstrapPinnedRequests(t, calls)
			assertBootstrapCleaned(t, launcher)
			steps, err := os.ReadFile(filepath.Join(root, "child-calls"))
			if err != nil && !errors.Is(err, os.ErrNotExist) {
				t.Fatal(err)
			}
			if strings.Contains(string(steps), "executed\n") {
				t.Fatalf("refused release ran: %s", steps)
			}
		})
	}
}
func TestBootstrapAcquisitionFailuresPreserveOwnedInstallation(t *testing.T) {
	failures := []struct {
		name    string
		prepare func(*testing.T, string)
	}{
		{"latest-download", func(t *testing.T, _ string) { t.Setenv("CW_MOCK_FAIL", "latest") }},
		{"archive-download", func(t *testing.T, _ string) { t.Setenv("CW_MOCK_FAIL", "archive") }},
		{"checksum-download", func(t *testing.T, _ string) { t.Setenv("CW_MOCK_FAIL", "checksum") }},
		{"missing-checksum", func(t *testing.T, root string) { writeFixture(t, filepath.Join(root, "SHA256SUMS"), "\n", 0644) }},
		{"duplicate-checksum", func(t *testing.T, root string) {
			path := filepath.Join(root, "SHA256SUMS")
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			writeFixture(t, path, string(data)+string(data), 0644)
		}},
		{"checksum-mismatch", func(t *testing.T, root string) {
			writeFixture(t, filepath.Join(root, "SHA256SUMS"), fmt.Sprintf("%s  cw_%s_%s.tar.gz\n", strings.Repeat("0", 64), runtime.GOOS, runtime.GOARCH), 0644)
		}},
		{"unidentified-binary", func(t *testing.T, root string) { bootstrapRelease(t, root, "#!/bin/sh\nexit 2\n") }},
		{"unbounded-version-output", func(t *testing.T, root string) {
			bootstrapRelease(t, root, "#!/bin/sh\nprintf '%s\\n' '"+strings.Repeat("x", 8192)+"'\n")
		}},
		{"hidden-verification-rejected", func(t *testing.T, _ string) { t.Setenv("CW_MOCK_VERIFY_EXIT", "23") }},
	}
	for _, member := range []struct {
		name    string
		headers []tar.Header
		bodies  []string
	}{
		{"missing-binary", []tar.Header{{Name: "unrelated", Typeflag: tar.TypeReg, Mode: 0755}}, []string{managerScript(true)}},
		{"duplicate-binary", []tar.Header{{Name: "cw", Typeflag: tar.TypeReg, Mode: 0755}, {Name: "cw", Typeflag: tar.TypeReg, Mode: 0755}}, []string{managerScript(true), managerScript(true)}},
		{"symlink-binary", []tar.Header{{Name: "cw", Typeflag: tar.TypeSymlink, Linkname: "../outside"}}, []string{""}},
		{"hardlink-binary", []tar.Header{{Name: "cw", Typeflag: tar.TypeLink, Linkname: "other"}}, []string{""}},
		{"traversal-binary", []tar.Header{{Name: "../cw", Typeflag: tar.TypeReg, Mode: 0755}}, []string{managerScript(true)}},
	} {
		failures = append(failures, struct {
			name    string
			prepare func(*testing.T, string)
		}{member.name, func(t *testing.T, root string) {
			bootstrapArchive(t, root, archiveFixture(t, member.headers, member.bodies))
		}})
	}
	valid := managerScript(true)
	for _, changed := range []struct{ name, old, replacement string }{
		{"wrong-version", `"version":"v1.2.3"`, `"version":"v1.2.4"`},
		{"invalid-source", fixtureSHA, strings.Repeat("G", 40)},
		{"wrong-platform", `"arch":"` + runtime.GOARCH + `"`, `"arch":"wrong"`},
		{"bundle-source-mismatch", `"revision":"` + fixtureSHA + `","os"`, `"revision":"` + strings.Repeat("1", 40) + `","os"`},
		{"bundle-version-mismatch", `{"version":"v1.2.3","revision"`, `{"version":"v1.2.4","revision"`},
		{"bundle-invalid-digest", strings.Repeat("b", 64), strings.Repeat("b", 63)},
	} {
		failures = append(failures, struct {
			name    string
			prepare func(*testing.T, string)
		}{changed.name, func(t *testing.T, root string) {
			bootstrapRelease(t, root, strings.ReplaceAll(valid, changed.old, changed.replacement))
		}})
	}
	for _, failure := range failures {
		t.Run(failure.name, func(t *testing.T) {
			launcher, _ := bootstrapFixture(t)
			root := filepath.Dir(filepath.Dir(launcher))
			home, owned, links := bootstrapOwnedInstallation(t, root)
			failure.prepare(t, root)
			before := bootstrapTree(t, filepath.Dir(launcher))
			stdout, stderr, err := bootstrapStreams(t.Context(), launcher, "install", "--home", home)
			if after := bootstrapTree(t, filepath.Dir(launcher)); !reflect.DeepEqual(after, before) {
				t.Fatal("failed acquisition changed the launcher directory")
			}
			if err == nil || stdout != "" || stderr == "" {
				t.Fatalf("invalid acquisition accepted: %v stdout=%q stderr=%q", err, stdout, stderr)
			}
			assertBootstrapOwnedInstallation(t, owned, links)
			assertBootstrapCleaned(t, launcher)
			steps, err := os.ReadFile(filepath.Join(root, "child-calls"))
			if err != nil && !errors.Is(err, os.ErrNotExist) {
				t.Fatal(err)
			}
			if strings.Contains(string(steps), "executed\n") {
				t.Fatalf("rejected candidate ran: %s", steps)
			}
		})
	}
}
func TestBootstrapRejectsNonstableOrUnpinnedLatest(t *testing.T) {
	for _, latest := range []string{"", "https://github.com/belevtsev/agent-workflows/releases/tag/v1.2.3-rc1", "https://github.com/belevtsev/agent-workflows/releases/tag/v01.2.3", "https://github.com/belevtsev/agent-workflows/releases/download/v1.2.3", "https://elsewhere.test/releases/tag/v1.2.3"} {
		t.Run(latest, func(t *testing.T) {
			launcher, calls := bootstrapFixture(t)
			t.Setenv("CW_MOCK_LATEST", latest)
			_, err := runBootstrap(t.Context(), launcher, "install")
			if err == nil {
				t.Fatal("untrusted latest URL accepted")
			}
			assertBootstrapCleaned(t, launcher)
			data, err := os.ReadFile(calls)
			if err != nil || strings.Count(string(data), "\n") != 1 {
				t.Fatalf("invalid pin fetched assets or retried: %q %v", data, err)
			}
		})
	}
}

// R-BS: verify stage lifetime, exact argv/stdio/exit forwarding, and cancellation
// with an owned process group. Every wait has a deadline and the child is joined.
func TestBootstrapStagesWhileChildRunsAndPreservesArgumentsStreamsAndExitStatus(t *testing.T) {
	bootstrapLiveChild(t, false)
}
func TestBootstrapCancellationCleansPrivateStage(t *testing.T) { bootstrapLiveChild(t, true) }
func bootstrapLiveChild(t *testing.T, cancellation bool) {
	t.Helper()
	launcher, calls := bootstrapFixture(t)
	root := filepath.Dir(filepath.Dir(launcher))
	home, owned, links := bootstrapOwnedInstallation(t, root)
	checkoutBefore := bootstrapTree(t, filepath.Dir(launcher))
	t.Setenv("CW_MOCK_HOLD", "1")
	t.Setenv("CW_MOCK_STREAMS", "1")
	t.Setenv("CW_MOCK_CHILD_EXIT", "37")
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	args := []string{"install", "--home", home, "argument with spaces", "", "literal-$value", "--dry-run=false"}
	command := bootstrapCommand(ctx, launcher, args...)
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	command.Stdin = strings.NewReader("forwarded input\n")
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	joined := false
	defer func() {
		cancel()
		if !joined {
			_ = command.Wait()
		}
	}()
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	var native string
waiting:
	for {
		select {
		case <-deadline.C:
			t.Fatal("native child did not reach its bounded hold")
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-ticker.C:
			data, err := os.ReadFile(filepath.Join(root, "running"))
			if err == nil && strings.HasSuffix(string(data), "\n") && strings.TrimSpace(string(data)) != "" {
				native = strings.TrimSpace(string(data))
				break waiting
			}
			if err != nil && !errors.Is(err, os.ErrNotExist) {
				t.Fatal(err)
			}
		}
	}
	stage := filepath.Dir(native)
	name := filepath.Base(stage)
	if filepath.Dir(stage) != root || !strings.HasPrefix(name, "cw-bootstrap.") || len(name) != len("cw-bootstrap.")+8 || filepath.Base(native) != "cw" {
		t.Fatalf("child is outside private stage: %q", native)
	}
	info, err := os.Stat(stage)
	if err != nil || info.Mode().Perm() != 0700 {
		t.Fatalf("stage permissions: %v %v", info, err)
	}
	entries, err := os.ReadDir(stage)
	if err != nil || len(entries) != 1 || entries[0].Name() != "cw" {
		t.Fatalf("live stage contains more than native child: %v %v", entries, err)
	}
	if cancellation {
		cancel()
	} else {
		writeFixture(t, filepath.Join(root, "gate"), "continue\n", 0600)
	}
	err = command.Wait()
	joined = true
	if cancellation {
		if err == nil {
			t.Fatal("cancellation reported success")
		}
	} else {
		var exit *exec.ExitError
		if !errors.As(err, &exit) || exit.ExitCode() != 37 {
			t.Fatalf("child exit not preserved: %v", err)
		}
		if stdout.String() != "forwarded input\n" || stderr.String() != "fixture-error\n" {
			t.Fatalf("stdio changed: stdout=%q stderr=%q", stdout.String(), stderr.String())
		}
		data, readErr := os.ReadFile(filepath.Join(root, "args"))
		if readErr != nil || string(data) != strings.Join(args, "\x00")+"\x00" {
			t.Fatalf("argv changed: %q %v", data, readErr)
		}
	}
	if after := bootstrapTree(t, filepath.Dir(launcher)); !reflect.DeepEqual(after, checkoutBefore) {
		t.Fatal("child lifecycle changed the launcher directory")
	}
	assertBootstrapOwnedInstallation(t, owned, links)
	assertBootstrapPinnedRequests(t, calls)
	assertBootstrapCleaned(t, launcher)
}

// R-BS / bootstrap-native-v3: SIGKILL cannot run shell cleanup traps. Preserve
// the killed-parent scenario, assert an untouched installation and launcher
// directory, and remove only fixture-owned residual stages before a verified retry.
func TestBootstrapKilledParentPreservesOwnedInstallation(t *testing.T) {
	launcher, _ := bootstrapFixture(t)
	root := filepath.Dir(filepath.Dir(launcher))
	home, owned, links := bootstrapOwnedInstallation(t, root)
	before := bootstrapTree(t, filepath.Dir(launcher))
	curl := filepath.Join(root, "mock-bin", "curl")
	original, err := os.ReadFile(curl)
	if err != nil {
		t.Fatal(err)
	}
	writeFixture(t, curl, `#!/bin/sh
set -eu
attempt=0
while [ ! -s "$CW_MOCK_KILL_PID_FILE" ]; do
 [ "$attempt" -lt 500 ] || exit 96
 attempt=$((attempt + 1)); sleep 0.01
done
kill -KILL "$(cat "$CW_MOCK_KILL_PID_FILE")"
exit 0
`, 0755)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	command := bootstrapCommand(ctx, launcher, "setup", "--home", home)
	var output bytes.Buffer
	command.Stdout, command.Stderr = &output, &output
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	joined := false
	defer func() {
		cancel()
		_ = syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
		if !joined {
			_ = command.Wait()
		}
	}()
	writeFixture(t, filepath.Join(root, "kill-parent-pid"), strconv.Itoa(command.Process.Pid)+"\n", 0600)
	err = command.Wait()
	joined = true
	var exit *exec.ExitError
	if !errors.As(err, &exit) {
		t.Fatalf("parent did not receive deliberate KILL: %v %s", err, output.String())
	}
	status, ok := exit.Sys().(syscall.WaitStatus)
	if !ok || !status.Signaled() || status.Signal() != syscall.SIGKILL {
		t.Fatalf("wrong parent termination: %v", err)
	}
	_ = syscall.Kill(-command.Process.Pid, syscall.SIGTERM)
	if after := bootstrapTree(t, filepath.Dir(launcher)); !reflect.DeepEqual(after, before) {
		t.Fatal("killed parent changed the launcher directory")
	}
	assertBootstrapOwnedInstallation(t, owned, links)
	stages, err := filepath.Glob(filepath.Join(root, "cw-bootstrap.*"))
	if err != nil {
		t.Fatal(err)
	}
	for _, stage := range stages {
		if err := os.RemoveAll(stage); err != nil {
			t.Fatal(err)
		}
	}
	writeFixture(t, curl, string(original), 0755)
	bootstrapSuccess(t, launcher, "setup", "--home", home)
	assertBootstrapOwnedInstallation(t, owned, links)
}
