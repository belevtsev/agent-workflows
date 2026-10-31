package manageruntime

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
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
	return managerScriptThreeCapabilities("cw-manager-v6", revision, "cw-model-policy-v2-dual-choice", "cw-codex-defaults-v1", "cw-openai-decisions-v1", "cw-three-adviser-v1")
}

func managerScriptIdentity(protocol, revision string) string {
	return managerScriptCapabilities(protocol, revision, "cw-model-policy-v2-dual-choice")
}

func managerScriptCapabilities(protocol, revision, policy string) string {
	return managerScriptAllCapabilities(protocol, revision, policy, "cw-codex-defaults-v1")
}

func managerScriptAllCapabilities(protocol, revision, policy, defaults string) string {
	return managerScriptOptionalCapabilities(protocol, revision, policy, defaults, "cw-openai-decisions-v1")
}

func managerScriptOptionalCapabilities(protocol, revision, policy, defaults, optional string) string {
	return managerScriptThreeCapabilities(protocol, revision, policy, defaults, optional, "")
}

func managerScriptThreeCapabilities(protocol, revision, policy, defaults, optional, required string) string {
	identity := fmt.Sprintf(`{"arch":"%s","built_at":"unknown","os":"%s","revision":"%s","version":"v1.2.3"}`, runtime.GOARCH, runtime.GOOS, revision)
	script := "#!/bin/sh\nif [ \"${1-}\" = version ]; then printf '%s\\n' '" + identity + "'; exit 0; fi\n"
	if protocol != "" {
		if protocol == "cw-manager-v6" && policy != "" {
			script += "if [ \"${1-}\" = --model-policy-capability ]; then printf '%s\\n' '" + policy + "'; exit 0; fi\n"
		}
		if protocol == "cw-manager-v6" && defaults != "" {
			script += "if [ \"${1-}\" = --codex-defaults-capability ]; then printf '%s\\n' '" + defaults + "'; exit 0; fi\n"
		}
		if protocol == "cw-manager-v6" && optional != "" {
			script += "if [ \"${1-}\" = --openai-decisions-capability ]; then printf '%s\\n' '" + optional + "'; exit 0; fi\n"
		}
		if protocol == "cw-manager-v6" && required != "" {
			script += "if [ \"${1-}\" = --three-adviser-capability ]; then printf '%s\\n' '" + required + "'; exit 0; fi\n"
		}
		script += "if [ \"${1-}\" = --manager-protocol ]; then printf '%s\\n' '" + protocol + "'; exit 0; fi\nprintf '%s\\n' '{\"forwarded\":true}'; exit 0\n"
	} else {
		script += "exit 2\n"
	}
	return script
}

func bootstrapFixture(t *testing.T, legacy bool) (string, string, string) {
	t.Helper()
	root := realTemp(t)
	checkout := filepath.Join(root, "source with spaces")
	bin := filepath.Join(checkout, ".bin")
	if err := os.MkdirAll(bin, 0755); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile("../../install.sh")
	if err != nil {
		t.Fatal(err)
	}
	launcher := filepath.Join(checkout, "install.sh")
	writeFixture(t, launcher, string(data), 0755)
	binary := filepath.Join(bin, "cw")
	if legacy {
		writeFixture(t, binary, managerScript(false), 0755)
	}
	mockBin := filepath.Join(root, "mock-bin")
	if err := os.Mkdir(mockBin, 0755); err != nil {
		t.Fatal(err)
	}
	calls := filepath.Join(root, "calls")
	writeFixture(t, filepath.Join(mockBin, "curl"), `#!/bin/sh
set -eu
output=
address=
next=false
for argument in "$@"; do
 if "$next"; then output=$argument; next=false; continue; fi
 case $argument in -o) next=true ;; https:*) address=$argument ;; esac
done
printf '%s\n' "$address" >> "$CW_MOCK_CALLS"
case $address in
 */SHA256SUMS) [ "${CW_MOCK_FAIL-}" != checksum ] || exit 22; cp "$CW_MOCK_SUMS" "$output" ;;
 *) [ "${CW_MOCK_FAIL-}" != archive ] || exit 22; cp "$CW_MOCK_ARCHIVE" "$output" ;;
esac
`, 0755)
	t.Setenv("PATH", mockBin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("CW_MOCK_CALLS", calls)
	t.Setenv("CW_MOCK_FAIL", "")
	bootstrapRelease(t, root, managerScript(true))
	return launcher, binary, calls
}

func bootstrapRelease(t *testing.T, root, script string) {
	t.Helper()
	archive := archiveFixture(t, []tar.Header{{Name: "cw", Typeflag: tar.TypeReg, Mode: 0755}}, []string{script})
	bootstrapArchive(t, root, archive)
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

func runBootstrap(ctx context.Context, launcher string, args ...string) (string, error) {
	command := exec.CommandContext(ctx, launcher, args...)
	data, err := command.CombinedOutput()
	return string(data), err
}

func bootstrapStreams(ctx context.Context, launcher string, args ...string) (string, string, error) {
	command := exec.CommandContext(ctx, launcher, args...)
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	err := command.Run()
	return stdout.String(), stderr.String(), err
}

func TestBootstrapRefreshesRecognizedLegacyManager(t *testing.T) {
	launcher, binary, calls := bootstrapFixture(t, true)
	output, err := runBootstrap(t.Context(), launcher, "status", "--home", "/fixture/home")
	if err != nil {
		t.Fatalf("%v: %s", err, output)
	}
	if strings.TrimSpace(output) != `{"forwarded":true}` {
		t.Fatalf("not forwarded: %q", output)
	}
	data, err := os.ReadFile(binary)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != managerScript(true) {
		t.Fatal("legacy manager not refreshed")
	}
	requested, err := os.ReadFile(calls)
	if err != nil {
		t.Fatal(err)
	}
	if len(strings.Split(strings.TrimSpace(string(requested)), "\n")) != 2 {
		t.Fatalf("unexpected downloads: %s", requested)
	}
}
func TestBootstrapColdDryRunAndHelpPreserveLegacy(t *testing.T) {
	for _, args := range [][]string{{"--dry-run"}, {"--dry-run=1"}, {"--dry-run=t"}, {"--dry-run=T"}, {"--dry-run=true"}, {"--dry-run=TRUE"}, {"--dry-run=True"}, {"help"}} {
		t.Run(args[0], func(t *testing.T) {
			launcher, binary, calls := bootstrapFixture(t, true)
			before, _ := os.ReadFile(binary)
			output, err := runBootstrap(t.Context(), launcher, args...)
			if err != nil {
				t.Fatalf("%v: %s", err, output)
			}
			after, _ := os.ReadFile(binary)
			if string(before) != string(after) {
				t.Fatal("read-only bootstrap replaced legacy cache")
			}
			if _, err = os.Stat(calls); !os.IsNotExist(err) {
				t.Fatal("read-only bootstrap downloaded")
			}
		})
	}
}
func TestBootstrapRefreshesOlderNativeProtocol(t *testing.T) {
	for _, protocol := range []string{"cw-manager-v2", "cw-manager-v3", "cw-manager-v4", "cw-manager-v5"} {
		t.Run(protocol, func(t *testing.T) {
			launcher, binary, calls := bootstrapFixture(t, false)
			writeFixture(t, binary, managerScriptIdentity(protocol, fixtureSHA), 0755)
			stdout, stderr, err := bootstrapStreams(t.Context(), launcher, "status")
			if err != nil || strings.TrimSpace(stdout) != `{"forwarded":true}` || stderr != "" {
				t.Fatalf("%v: stdout=%q stderr=%q", err, stdout, stderr)
			}
			data, err := os.ReadFile(binary)
			if err != nil || string(data) != managerScript(true) {
				t.Fatalf("old native protocol not refreshed: %v", err)
			}
			if _, err := os.Stat(calls); err != nil {
				t.Fatal("old native manager did not acquire a compatible release")
			}
		})
	}
}
func TestBootstrapColdDryRunBooleanFormsNeverCreateCache(t *testing.T) {
	for _, argument := range []string{"--dry-run", "--dry-run=1", "--dry-run=t", "--dry-run=T", "--dry-run=true", "--dry-run=TRUE", "--dry-run=True"} {
		t.Run(argument, func(t *testing.T) {
			launcher, binary, calls := bootstrapFixture(t, false)
			cache := filepath.Dir(binary)
			if err := os.Remove(cache); err != nil {
				t.Fatal(err)
			}
			output, err := runBootstrap(t.Context(), launcher, "install", argument)
			if err != nil || !strings.Contains(output, `"validation":"deferred"`) {
				t.Fatalf("%v: %s", err, output)
			}
			for _, path := range []string{cache, filepath.Dir(cache) + "/.bin.lock", calls} {
				if _, err := os.Lstat(path); !os.IsNotExist(err) {
					t.Fatalf("dry run created %s: %v", path, err)
				}
			}
		})
	}
}
func TestBootstrapPreservesUnidentifiedCache(t *testing.T) {
	launcher, binary, calls := bootstrapFixture(t, true)
	writeFixture(t, binary, "#!/bin/sh\nprintf '%s\\n' unknown\n", 0755)
	before, _ := os.ReadFile(binary)
	output, err := runBootstrap(t.Context(), launcher, "status")
	if err == nil || !strings.Contains(output, "unidentified .bin/cw") {
		t.Fatalf("%v: %s", err, output)
	}
	after, _ := os.ReadFile(binary)
	if string(before) != string(after) {
		t.Fatal("unidentified cache replaced")
	}
	if _, err = os.Stat(calls); !os.IsNotExist(err) {
		t.Fatal("unidentified cache downloaded")
	}
}

func TestBootstrapRejectsOlderReleasesWithoutChangingOwnedInstallation(t *testing.T) {
	for _, latest := range []string{"cw-manager-v3", "cw-manager-v4", "cw-manager-v5"} {
		t.Run(latest, func(t *testing.T) {
			for _, cache := range []string{"cold", "cw-manager-v3", "cw-manager-v4", "cw-manager-v5"} {
				t.Run(cache, func(t *testing.T) {
					launcher, binary, calls := bootstrapFixture(t, false)
					root := filepath.Dir(filepath.Dir(launcher))
					bootstrapRelease(t, root, managerScriptIdentity(latest, fixtureSHA))
					old := ""
					if cache != "cold" {
						old = managerScriptIdentity(cache, strings.Repeat("1", 40))
						writeFixture(t, binary, old, 0755)
					}
					home, owned, links := bootstrapOwnedInstallation(t, root)
					stdout, stderr, err := bootstrapStreams(t.Context(), launcher, "install", "--home", home)
					if err == nil || stdout != "" || !strings.Contains(stderr, "released native manager does not support the required bootstrap capabilities cw-manager-v6, cw-model-policy-v2-dual-choice, cw-codex-defaults-v1, cw-openai-decisions-v1, and cw-three-adviser-v1") {
						t.Fatalf("latest %s was accepted: %v: stdout=%q stderr=%q", latest, err, stdout, stderr)
					}
					assertBootstrapCacheUnchanged(t, binary, old)
					assertBootstrapOwnedInstallation(t, owned, links)
					requested, err := os.ReadFile(calls)
					if err != nil || len(strings.Split(strings.TrimSpace(string(requested)), "\n")) != 2 {
						t.Fatalf("unexpected downloads: %v: %s", err, requested)
					}
					assertBootstrapCleaned(t, launcher, binary)
				})
			}
		})
	}
}

func bootstrapOwnedInstallation(t *testing.T, root string) (string, map[string]string, map[string]string) {
	t.Helper()
	home := filepath.Join(root, "installed home")
	state := filepath.Join(home, ".local", "state", "agent-workflows")
	owned := map[string]string{
		filepath.Join(home, ".codex", "config.toml"): "model = \"fixture\"\n",
		filepath.Join(home, ".codex", "AGENTS.md"):   "fixture instructions\n",
		filepath.Join(state, "state.json"):           "{\"fixture\":\"owned state\"}\n",
		filepath.Join(state, "journal.json"):         "{\"fixture\":\"pending journal\"}\n",
		filepath.Join(home, ".bashrc"):               "export PATH=\"fixture\"\n",
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
		filepath.Join(state, "current"):                                filepath.Join(root, "active release"),
		filepath.Join(state, "runtime", "current"):                     filepath.Join(root, "active manager"),
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

func assertBootstrapCacheUnchanged(t *testing.T, binary, previous string) {
	t.Helper()
	if previous == "" {
		if _, err := os.Lstat(binary); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("refused release published a cold binary: %v", err)
		}
		return
	}
	data, err := os.ReadFile(binary)
	if err != nil || string(data) != previous {
		t.Fatalf("stale binary bytes changed: %v", err)
	}
	info, err := os.Stat(binary)
	if err != nil || info.Mode().Perm() != 0755 {
		t.Fatalf("stale binary permissions changed: %v", err)
	}
}

func TestBootstrapAcquisitionFailuresPreserveCacheAndOwnedInstallation(t *testing.T) {
	for _, failure := range []struct {
		name, message string
		prepare       func(*testing.T, string)
	}{
		{"archive-download", "native manager download failed", func(t *testing.T, _ string) { t.Setenv("CW_MOCK_FAIL", "archive") }},
		{"checksum-download", "native manager checksum download failed", func(t *testing.T, _ string) { t.Setenv("CW_MOCK_FAIL", "checksum") }},
		{"missing-checksum", "release checksum manifest is missing or duplicates the requested asset", func(t *testing.T, root string) {
			writeFixture(t, filepath.Join(root, "SHA256SUMS"), "\n", 0644)
		}},
		{"duplicate-checksum", "release checksum manifest is missing or duplicates the requested asset", func(t *testing.T, root string) {
			path := filepath.Join(root, "SHA256SUMS")
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			writeFixture(t, path, string(data)+string(data), 0644)
		}},
		{"checksum-mismatch", "native manager archive checksum mismatch", func(t *testing.T, root string) {
			writeFixture(t, filepath.Join(root, "SHA256SUMS"), fmt.Sprintf("%s  cw_%s_%s.tar.gz\n", strings.Repeat("0", 64), runtime.GOOS, runtime.GOARCH), 0644)
		}},
		{"missing-binary", "release archive has no readable cw binary", func(t *testing.T, root string) {
			archive := archiveFixture(t, []tar.Header{{Name: "unrelated", Typeflag: tar.TypeReg, Mode: 0755}}, []string{managerScript(true)})
			bootstrapArchive(t, root, archive)
		}},
		{"unidentified-binary", "release asset is not an identifiable native manager for this platform", func(t *testing.T, root string) {
			bootstrapRelease(t, root, "#!/bin/sh\nexit 2\n")
		}},
	} {
		t.Run(failure.name, func(t *testing.T) {
			for _, cache := range []string{"cold", "cw-manager-v3", "cw-manager-v4", "cw-manager-v5"} {
				t.Run(cache, func(t *testing.T) {
					launcher, binary, _ := bootstrapFixture(t, false)
					root := filepath.Dir(filepath.Dir(launcher))
					old := ""
					if cache != "cold" {
						old = managerScriptIdentity(cache, fixtureSHA)
						writeFixture(t, binary, old, 0755)
					}
					home, owned, links := bootstrapOwnedInstallation(t, root)
					failure.prepare(t, root)
					stdout, stderr, err := bootstrapStreams(t.Context(), launcher, "install", "--home", home)
					if err == nil || stdout != "" || !strings.Contains(stderr, failure.message) {
						t.Fatalf("failure was accepted: %v: stdout=%q stderr=%q", err, stdout, stderr)
					}
					assertBootstrapCacheUnchanged(t, binary, old)
					assertBootstrapOwnedInstallation(t, owned, links)
					assertBootstrapCleaned(t, launcher, binary)
				})
			}
		})
	}
}

func assertBootstrapCleaned(t *testing.T, launcher, binary string) {
	t.Helper()
	if _, err := os.Lstat(filepath.Join(filepath.Dir(launcher), ".bin.lock")); !os.IsNotExist(err) {
		t.Fatalf("bootstrap lock remains: %v", err)
	}
	entries, err := os.ReadDir(filepath.Dir(binary))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".bootstrap.") {
			t.Fatalf("temporary bootstrap remains: %s", entry.Name())
		}
	}
}

func TestBootstrapReusesCompatibleCacheWithoutRequiringSourceRevision(t *testing.T) {
	for _, args := range [][]string{{"status"}, {"recover"}, {"install", "--dry-run"}} {
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			launcher, binary, calls := bootstrapFixture(t, false)
			// The bootstrap selects capability; Go acquisition selects an exact source SHA.
			compatible := currentManagerScriptIdentity(strings.Repeat("1", 40))
			writeFixture(t, binary, compatible, 0755)
			stdout, stderr, err := bootstrapStreams(t.Context(), launcher, args...)
			if err != nil || strings.TrimSpace(stdout) != `{"forwarded":true}` || stderr != "" {
				t.Fatalf("%v: stdout=%q stderr=%q", err, stdout, stderr)
			}
			data, err := os.ReadFile(binary)
			if err != nil || string(data) != compatible {
				t.Fatalf("compatible cache changed: %v", err)
			}
			for _, path := range []string{calls, filepath.Join(filepath.Dir(launcher), ".bin.lock")} {
				if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("cached invocation acquired or wrote %s: %v", path, err)
				}
			}
		})
	}
}

func TestBootstrapRequiresPairedPolicyCapabilityFromV6(t *testing.T) {
	const paired = "cw-model-policy-v2-dual-choice"
	for _, test := range []struct {
		name, cachedPolicy, releasedPolicy string
		dryRun, wantFailure                bool
	}{
		{name: "upgrade v6 without capability", releasedPolicy: paired},
		{name: "upgrade v6 with older policy", cachedPolicy: "cw-model-policy-v2", releasedPolicy: paired},
		{name: "preserve cache when latest lacks capability", wantFailure: true},
		{name: "preserve cache when latest has older policy", cachedPolicy: "cw-model-policy-v2", releasedPolicy: "cw-model-policy-v2", wantFailure: true},
		{name: "stale v6 dry run stays offline", releasedPolicy: paired, dryRun: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			launcher, binary, calls := bootstrapFixture(t, false)
			root := filepath.Dir(filepath.Dir(launcher))
			old := managerScriptCapabilities("cw-manager-v6", strings.Repeat("1", 40), test.cachedPolicy)
			writeFixture(t, binary, old, 0o755)
			latest := managerScriptThreeCapabilities("cw-manager-v6", fixtureSHA, test.releasedPolicy, "cw-codex-defaults-v1", "cw-openai-decisions-v1", "cw-three-adviser-v1")
			bootstrapRelease(t, root, latest)
			home, owned, links := bootstrapOwnedInstallation(t, root)
			args := []string{"install", "--home", home}
			if test.dryRun {
				args = append(args, "--dry-run")
			}
			stdout, stderr, err := bootstrapStreams(t.Context(), launcher, args...)
			switch {
			case test.dryRun:
				if err != nil || stderr != "" || !strings.Contains(stdout, `"validation":"deferred"`) {
					t.Fatalf("stale v6 dry run: %v: stdout=%q stderr=%q", err, stdout, stderr)
				}
			case test.wantFailure:
				if err == nil || stdout != "" || !strings.Contains(stderr, "cw-model-policy-v2-dual-choice") {
					t.Fatalf("incompatible v6 release accepted: %v: stdout=%q stderr=%q", err, stdout, stderr)
				}
			default:
				if err != nil || stderr != "" || strings.TrimSpace(stdout) != `{"forwarded":true}` {
					t.Fatalf("v6 capability upgrade: %v: stdout=%q stderr=%q", err, stdout, stderr)
				}
			}
			if test.dryRun || test.wantFailure {
				assertBootstrapCacheUnchanged(t, binary, old)
			} else if data, readErr := os.ReadFile(binary); readErr != nil || string(data) != latest {
				t.Fatalf("stale v6 cache not replaced: %v", readErr)
			}
			assertBootstrapOwnedInstallation(t, owned, links)
			if test.dryRun {
				if _, err := os.Stat(calls); !os.IsNotExist(err) {
					t.Fatalf("stale v6 dry run downloaded: %v", err)
				}
			} else if requested, readErr := os.ReadFile(calls); readErr != nil || len(strings.Split(strings.TrimSpace(string(requested)), "\n")) != 2 {
				t.Fatalf("unexpected v6 capability downloads: %v: %s", readErr, requested)
			}
			assertBootstrapCleaned(t, launcher, binary)
		})
	}
}

func TestBootstrapRequiresSharedConfigCapabilityFromPairedPolicyV6(t *testing.T) {
	const paired = "cw-model-policy-v2-dual-choice"
	const capable = "cw-codex-defaults-v1"
	for _, test := range []struct {
		name, cachedDefaults, releasedDefaults string
		dryRun, wantFailure                    bool
	}{
		{name: "upgrade paired v6 without defaults capability", releasedDefaults: capable},
		{name: "upgrade paired v6 with older defaults capability", cachedDefaults: "cw-codex-defaults-v0", releasedDefaults: capable},
		{name: "preserve cache when latest lacks defaults capability", wantFailure: true},
		{name: "preserve cache when latest has older defaults capability", releasedDefaults: "cw-codex-defaults-v0", wantFailure: true},
		{name: "stale paired v6 dry run stays offline", releasedDefaults: capable, dryRun: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			launcher, binary, calls := bootstrapFixture(t, false)
			root := filepath.Dir(filepath.Dir(launcher))
			old := managerScriptAllCapabilities("cw-manager-v6", strings.Repeat("1", 40), paired, test.cachedDefaults)
			writeFixture(t, binary, old, 0o755)
			latest := managerScriptThreeCapabilities("cw-manager-v6", fixtureSHA, paired, test.releasedDefaults, "cw-openai-decisions-v1", "cw-three-adviser-v1")
			bootstrapRelease(t, root, latest)
			home, owned, links := bootstrapOwnedInstallation(t, root)
			args := []string{"install", "--home", home}
			if test.dryRun {
				args = append(args, "--dry-run")
			}
			stdout, stderr, err := bootstrapStreams(t.Context(), launcher, args...)
			switch {
			case test.dryRun:
				if err != nil || stderr != "" || !strings.Contains(stdout, `"validation":"deferred"`) {
					t.Fatalf("stale paired v6 dry run: %v: stdout=%q stderr=%q", err, stdout, stderr)
				}
			case test.wantFailure:
				if err == nil || stdout != "" || !strings.Contains(stderr, capable) {
					t.Fatalf("incompatible config release accepted: %v: stdout=%q stderr=%q", err, stdout, stderr)
				}
			default:
				if err != nil || stderr != "" || strings.TrimSpace(stdout) != `{"forwarded":true}` {
					t.Fatalf("config capability upgrade: %v: stdout=%q stderr=%q", err, stdout, stderr)
				}
			}
			if test.dryRun || test.wantFailure {
				assertBootstrapCacheUnchanged(t, binary, old)
			} else if data, readErr := os.ReadFile(binary); readErr != nil || string(data) != latest {
				t.Fatalf("stale defaults cache not replaced: %v", readErr)
			}
			assertBootstrapOwnedInstallation(t, owned, links)
			if test.dryRun {
				if _, err := os.Stat(calls); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("stale paired v6 dry run downloaded: %v", err)
				}
			} else if requested, readErr := os.ReadFile(calls); readErr != nil || len(strings.Split(strings.TrimSpace(string(requested)), "\n")) != 2 {
				t.Fatalf("unexpected config capability downloads: %v: %s", readErr, requested)
			}
			assertBootstrapCleaned(t, launcher, binary)
		})
	}
}

func TestBootstrapRequiresOpenAIDecisionsCapabilityFromCurrentV6(t *testing.T) {
	const capable = "cw-openai-decisions-v1"
	for _, test := range []struct {
		name, cachedOptional, releasedOptional string
		dryRun, wantFailure                    bool
	}{
		{name: "upgrade current v6 without optional adviser capability", releasedOptional: capable},
		{name: "upgrade current v6 with older optional adviser capability", cachedOptional: "cw-openai-decisions-v0", releasedOptional: capable},
		{name: "preserve cache when latest lacks optional adviser capability", wantFailure: true},
		{name: "preserve cache when latest has older optional adviser capability", releasedOptional: "cw-openai-decisions-v0", wantFailure: true},
		{name: "stale current v6 preview stays offline", releasedOptional: capable, dryRun: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			launcher, binary, calls := bootstrapFixture(t, false)
			root := filepath.Dir(filepath.Dir(launcher))
			old := managerScriptOptionalCapabilities("cw-manager-v6", strings.Repeat("1", 40), "cw-model-policy-v2-dual-choice", "cw-codex-defaults-v1", test.cachedOptional)
			writeFixture(t, binary, old, 0o755)
			latest := managerScriptThreeCapabilities("cw-manager-v6", fixtureSHA, "cw-model-policy-v2-dual-choice", "cw-codex-defaults-v1", test.releasedOptional, "cw-three-adviser-v1")
			bootstrapRelease(t, root, latest)
			home, owned, links := bootstrapOwnedInstallation(t, root)
			args := []string{"install", "--home", home}
			if test.dryRun {
				args = append(args, "--dry-run")
			}
			stdout, stderr, err := bootstrapStreams(t.Context(), launcher, args...)
			switch {
			case test.dryRun:
				if err != nil || stderr != "" || !strings.Contains(stdout, `"validation":"deferred"`) {
					t.Fatalf("stale adviser capability preview: %v: stdout=%q stderr=%q", err, stdout, stderr)
				}
			case test.wantFailure:
				if err == nil || stdout != "" || !strings.Contains(stderr, capable) {
					t.Fatalf("incompatible adviser release accepted: %v: stdout=%q stderr=%q", err, stdout, stderr)
				}
			default:
				if err != nil || stderr != "" || strings.TrimSpace(stdout) != `{"forwarded":true}` {
					t.Fatalf("adviser capability upgrade: %v: stdout=%q stderr=%q", err, stdout, stderr)
				}
			}
			if test.dryRun || test.wantFailure {
				assertBootstrapCacheUnchanged(t, binary, old)
			} else if data, readErr := os.ReadFile(binary); readErr != nil || string(data) != latest {
				t.Fatalf("stale adviser capability cache not replaced: %v", readErr)
			}
			assertBootstrapOwnedInstallation(t, owned, links)
			if test.dryRun {
				if _, err := os.Stat(calls); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("stale adviser capability preview downloaded: %v", err)
				}
			} else if requested, readErr := os.ReadFile(calls); readErr != nil || len(strings.Split(strings.TrimSpace(string(requested)), "\n")) != 2 {
				t.Fatalf("unexpected adviser capability downloads: %v: %s", readErr, requested)
			}
			assertBootstrapCleaned(t, launcher, binary)
		})
	}
}

func TestBootstrapRequiresThreeAdviserCapabilityFromOptionalPolicyV6(t *testing.T) {
	const capable = "cw-three-adviser-v1"
	for _, test := range []struct {
		name, cachedRequired, releasedRequired string
		dryRun, wantFailure                    bool
	}{
		{name: "upgrade optional v6 without three-adviser capability", releasedRequired: capable},
		{name: "upgrade optional v6 with older three-adviser capability", cachedRequired: "cw-three-adviser-v0", releasedRequired: capable},
		{name: "preserve cache when latest lacks three-adviser capability", wantFailure: true},
		{name: "preserve cache when latest has older three-adviser capability", releasedRequired: "cw-three-adviser-v0", wantFailure: true},
		{name: "optional v6 preview stays offline", releasedRequired: capable, dryRun: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			launcher, binary, calls := bootstrapFixture(t, false)
			root := filepath.Dir(filepath.Dir(launcher))
			old := managerScriptThreeCapabilities("cw-manager-v6", strings.Repeat("1", 40), "cw-model-policy-v2-dual-choice", "cw-codex-defaults-v1", "cw-openai-decisions-v1", test.cachedRequired)
			writeFixture(t, binary, old, 0o755)
			latest := managerScriptThreeCapabilities("cw-manager-v6", fixtureSHA, "cw-model-policy-v2-dual-choice", "cw-codex-defaults-v1", "cw-openai-decisions-v1", test.releasedRequired)
			bootstrapRelease(t, root, latest)
			home, owned, links := bootstrapOwnedInstallation(t, root)
			args := []string{"install", "--home", home}
			if test.dryRun {
				args = append(args, "--dry-run")
			}
			stdout, stderr, err := bootstrapStreams(t.Context(), launcher, args...)
			switch {
			case test.dryRun:
				if err != nil || stderr != "" || !strings.Contains(stdout, `"validation":"deferred"`) {
					t.Fatalf("optional v6 preview: %v: stdout=%q stderr=%q", err, stdout, stderr)
				}
			case test.wantFailure:
				if err == nil || stdout != "" || !strings.Contains(stderr, capable) {
					t.Fatalf("incompatible three-adviser release accepted: %v: stdout=%q stderr=%q", err, stdout, stderr)
				}
			default:
				if err != nil || stderr != "" || strings.TrimSpace(stdout) != `{"forwarded":true}` {
					t.Fatalf("three-adviser capability upgrade: %v: stdout=%q stderr=%q", err, stdout, stderr)
				}
			}
			if test.dryRun || test.wantFailure {
				assertBootstrapCacheUnchanged(t, binary, old)
			} else if data, readErr := os.ReadFile(binary); readErr != nil || string(data) != latest {
				t.Fatalf("optional v6 cache not replaced: %v", readErr)
			}
			assertBootstrapOwnedInstallation(t, owned, links)
			if test.dryRun {
				if _, err := os.Stat(calls); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("optional v6 preview downloaded: %v", err)
				}
			} else if requested, readErr := os.ReadFile(calls); readErr != nil || len(strings.Split(strings.TrimSpace(string(requested)), "\n")) != 2 {
				t.Fatalf("unexpected three-adviser capability downloads: %v: %s", readErr, requested)
			}
			assertBootstrapCleaned(t, launcher, binary)
		})
	}
}

func TestBootstrapColdV6Download(t *testing.T) {
	launcher, binary, calls := bootstrapFixture(t, false)
	stdout, stderr, err := bootstrapStreams(t.Context(), launcher, "install")
	if err != nil || strings.TrimSpace(stdout) != `{"forwarded":true}` || stderr != "" {
		t.Fatalf("%v: stdout=%q stderr=%q", err, stdout, stderr)
	}
	data, err := os.ReadFile(binary)
	if err != nil || string(data) != managerScript(true) {
		t.Fatalf("compatible release not placed: %v", err)
	}
	requested, err := os.ReadFile(calls)
	if err != nil || len(strings.Split(strings.TrimSpace(string(requested)), "\n")) != 2 {
		t.Fatalf("unexpected downloads: %v: %s", err, requested)
	}
	assertBootstrapCleaned(t, launcher, binary)
}

func TestBootstrapStaleManagersDryRunHasNoDownloadsOrPersistentWrites(t *testing.T) {
	for _, protocol := range []string{"cw-manager-v3", "cw-manager-v4", "cw-manager-v5"} {
		t.Run(protocol, func(t *testing.T) {
			for _, argument := range []string{"--dry-run", "--dry-run=1", "--dry-run=t", "--dry-run=T", "--dry-run=true", "--dry-run=TRUE", "--dry-run=True", "--help"} {
				t.Run(argument, func(t *testing.T) {
					launcher, binary, calls := bootstrapFixture(t, false)
					root := filepath.Dir(filepath.Dir(launcher))
					old := managerScriptIdentity(protocol, fixtureSHA)
					writeFixture(t, binary, old, 0755)
					home, owned, links := bootstrapOwnedInstallation(t, root)
					stdout, stderr, err := bootstrapStreams(t.Context(), launcher, "install", "--home", home, argument)
					if err != nil || stderr != "" {
						t.Fatalf("%v: stdout=%q stderr=%q", err, stdout, stderr)
					}
					if argument == "--help" {
						if !strings.Contains(stdout, "Usage: ./install.sh") {
							t.Fatalf("help output missing: %q", stdout)
						}
					} else if strings.TrimSpace(stdout) != `{"dry_run":true,"environment":{"kind":"native_go","status":"deferred"},"next_step":"Run ./install.sh to prepare the native manager","validation":"deferred"}` {
						t.Fatalf("dry-run validation was not deferred: %q", stdout)
					}
					assertBootstrapCacheUnchanged(t, binary, old)
					assertBootstrapOwnedInstallation(t, owned, links)
					if _, err := os.Lstat(calls); !errors.Is(err, os.ErrNotExist) {
						t.Fatalf("read-only bootstrap downloaded: %v", err)
					}
					assertBootstrapCleaned(t, launcher, binary)
				})
			}
		})
	}
}

func TestBootstrapPreservesOccupiedCacheForms(t *testing.T) {
	for _, occupied := range []string{"symbolic-link", "directory", "non-executable"} {
		t.Run(occupied, func(t *testing.T) {
			launcher, binary, calls := bootstrapFixture(t, false)
			payload := binary
			switch occupied {
			case "symbolic-link":
				payload = filepath.Join(filepath.Dir(launcher), "external-manager")
				writeFixture(t, payload, managerScript(true), 0755)
				if err := os.Symlink(payload, binary); err != nil {
					t.Fatal(err)
				}
			case "directory":
				if err := os.Mkdir(binary, 0755); err != nil {
					t.Fatal(err)
				}
				payload = filepath.Join(binary, "unrelated")
				writeFixture(t, payload, "unrelated contents\n", 0644)
			case "non-executable":
				writeFixture(t, binary, managerScriptIdentity("cw-manager-v3", fixtureSHA), 0644)
			}
			before, err := os.ReadFile(payload)
			if err != nil {
				t.Fatal(err)
			}
			info, err := os.Lstat(binary)
			if err != nil {
				t.Fatal(err)
			}
			stdout, stderr, err := bootstrapStreams(t.Context(), launcher, "install")
			if err == nil || stdout != "" || !strings.Contains(stderr, "unidentified .bin/cw; refusing to replace it") {
				t.Fatalf("occupied cache accepted: %v stdout=%q stderr=%q", err, stdout, stderr)
			}
			after, err := os.ReadFile(payload)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatalf("occupied payload changed: %v", err)
			}
			afterInfo, err := os.Lstat(binary)
			if err != nil || info.Mode() != afterInfo.Mode() {
				t.Fatalf("occupied cache type or mode changed: %v", err)
			}
			if occupied == "symbolic-link" {
				target, err := os.Readlink(binary)
				if err != nil || target != payload {
					t.Fatalf("occupied link changed: %v", err)
				}
			}
			for _, path := range []string{calls, filepath.Join(filepath.Dir(launcher), ".bin.lock")} {
				if _, err := os.Lstat(path); !os.IsNotExist(err) {
					t.Fatalf("occupied cache acquired or wrote %s: %v", path, err)
				}
			}
		})
	}
}
