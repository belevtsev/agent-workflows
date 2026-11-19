package installers

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestOfflineInstallerVersionSelection(t *testing.T) {
	const preview = "v0.0.1-routing-preview+local.1"
	assets := fixtureAssets(t, preview)
	native := archiveName(preview)
	nativeRow := assets["SHA256SUMS"]
	pinnedRow := checksumLine(archiveName(preparedVersion), assets[native])
	multiple := bytes.Join([][]byte{nativeRow, pinnedRow}, nil)
	suffix := strings.TrimPrefix(native, "kitsu_"+preview)
	otherAssets := bytes.Join([][]byte{
		checksumLine("kitsu_v9.0.0_other_amd64.tar.gz", nil),
		checksumLine("kitsu"+suffix, nil), // Overlapping prefix and suffix leave no separator for the tag.
		checksumLine("nested/"+archiveName(preparedVersion), nil),
		checksumLine(archiveName(preparedVersion)+".sbom.json", nil),
		checksumLine("kitsu-plugin.zip", nil),
	}, nil)
	markedRow := strings.Replace(strings.TrimSuffix(string(nativeRow), "\n"), "  ", " *", 1) + "\r\n"
	tests := []struct {
		name     string
		manifest []byte
		envTag   string
		flagTag  string
		wantErr  string
	}{
		{name: "native preview", manifest: bytes.Join([][]byte{otherAssets, []byte(markedRow)}, nil)},
		{name: "environment tag", manifest: multiple, envTag: preview},
		{name: "explicit tag over environment", manifest: multiple, envTag: preparedVersion, flagTag: preview},
		{name: "environment tag is required when set", manifest: nativeRow, envTag: preparedVersion, wantErr: "exactly once"},
		{name: "ambiguous tags", manifest: multiple, wantErr: "lists multiple Kitsu archives"},
		{name: "duplicate row", manifest: bytes.Repeat(nativeRow, 2), wantErr: "exactly once"},
		{name: "missing native archive", manifest: otherAssets, wantErr: "does not list a Kitsu archive"},
		{name: "invalid tag", manifest: checksumLine(archiveName("v0.0.1/unsafe"), nil), wantErr: "release tag"},
	}
	for _, host := range nativeInstallers(t) {
		archiveFlag, versionFlag := "--archive-dir", "--version"
		if runtime.GOOS == "windows" {
			archiveFlag, versionFlag = "-ArchiveDir", "-Version"
		}
		for _, test := range tests {
			t.Run(host.name+"/"+test.name, func(t *testing.T) {
				run := newInstallRun(t, host, "http://127.0.0.1:1")
				archives := "offline archives"
				writeFile(t, filepath.Join(run.dir, archives, native), assets[native])
				writeFile(t, filepath.Join(run.dir, archives, "SHA256SUMS"), test.manifest)
				run.env["KITSU_INSTALL_TEST_VERSION"] = preview
				run.env["KITSU_VERSION"] = test.envTag
				args := []string{archiveFlag, archives}
				if test.flagTag != "" {
					args = append(args, versionFlag, test.flagTag)
				}
				if test.wantErr != "" {
					original := []byte("existing binary")
					writeFile(t, run.target, original)
					output, err := run.run(false, args...)
					if err == nil || !strings.Contains(output, test.wantErr) {
						t.Fatalf("wanted %q from failed installation, got %v\n%s", test.wantErr, err, output)
					}
					if test.name == "ambiguous tags" && !strings.Contains(output, versionFlag+" TAG") {
						t.Fatalf("missing version selection guidance:\n%s", output)
					}
					if actual := readFile(t, run.target); !bytes.Equal(actual, original) {
						t.Fatal("a failed installation changed the existing binary")
					}
					return
				}
				if output, err := run.run(false, args...); err != nil {
					t.Fatalf("offline install: %v\n%s", err, output)
				}
				if output := run.installedVersion(); !strings.HasPrefix(output, "kitsu "+preview+"\n") {
					t.Fatalf("unexpected installed version: %s", output)
				}
			})
		}
	}
}

func TestRemoteInstallerUsesPinnedVersion(t *testing.T) {
	const preview = "v0.0.1-routing-preview"
	base := serveAssets(t, fixtureAssets(t, preview))
	for _, host := range nativeInstallers(t) {
		t.Run(host.name, func(t *testing.T) {
			run := newInstallRun(t, host, base)
			run.env["KITSU_INSTALL_TEST_VERSION"] = preview
			if output, err := run.run(false); err == nil || !strings.Contains(output, archiveName(preparedVersion)) {
				t.Fatalf("expected a failure requesting the pinned version: %v\n%s", err, output)
			}
			if _, err := os.Stat(run.target); !os.IsNotExist(err) {
				t.Fatalf("expected no installation, got %v", err)
			}
		})
	}
}

func TestPowerShellInstallerParameterValidation(t *testing.T) {
	var shells []installer
	if runtime.GOOS == "windows" {
		shells = nativeInstallers(t)
	} else {
		program, err := exec.LookPath("pwsh")
		if err != nil {
			t.Skip("PowerShell is unavailable")
		}
		shells = []installer{{name: "pwsh", program: program, script: "install.ps1"}}
	}
	for _, shell := range shells {
		for _, test := range []struct {
			name, want string
			piped      bool
			args       []string
		}{
			{name: "piped without AgentHost", piped: true, want: "Version must be a v-prefixed release tag"},
			{name: "unknown AgentHost", args: []string{"-AgentHost", "other"}, want: "-AgentHost must be codex or claude"},
			{name: "empty AgentHost", args: []string{"-AgentHost", ""}, want: "-AgentHost must be codex or claude"},
		} {
			t.Run(shell.name+"/"+test.name, func(t *testing.T) {
				// Use the validation guards before the Windows-only installation begins.
				run := newInstallRun(t, shell, "http://127.0.0.1:1")
				run.env["KITSU_VERSION"] = "invalid"
				output, err := run.run(test.piped, test.args...)
				if err == nil || !strings.Contains(output, test.want) {
					t.Fatalf("expected %q from the installer: %v\n%s", test.want, err, output)
				}
			})
		}
	}
}

func TestInstallerAgentHostValidation(t *testing.T) {
	base := serveAssets(t, fixtureAssets(t, preparedVersion))
	for _, host := range nativeInstallers(t) {
		archiveFlag, hostFlag, cliOnlyFlag := "--archive-dir", "--host", "--cli-only"
		if runtime.GOOS == "windows" {
			archiveFlag, hostFlag, cliOnlyFlag = "-ArchiveDir", "-AgentHost", "-CliOnly"
		}
		for _, test := range []struct {
			name string
			args []string
			want string
		}{
			{name: "missing plugin", args: []string{hostFlag, "codex"}, want: "kitsu-plugin.zip"},
			{name: "validates host", args: []string{archiveFlag, "nonexistent", hostFlag, "other"}, want: hostFlag},
			{name: "conflicting host options", args: []string{cliOnlyFlag, hostFlag, "codex"}, want: cliOnlyFlag},
		} {
			t.Run(host.name+"/"+test.name, func(t *testing.T) {
				run := newInstallRun(t, host, base)
				output, err := run.run(false, test.args...)
				if err == nil || !strings.Contains(output, test.want) {
					t.Fatalf("expected invalid installer arguments: %v\n%s", err, output)
				}
				if _, err := os.Stat(run.target); !os.IsNotExist(err) {
					t.Fatalf("expected no installation, got %v", err)
				}
			})
		}
	}
}

func TestInstallerAgentFailurePreservesCLIAndShowsRetry(t *testing.T) {
	assets := fixtureAssets(t, preparedVersion)
	for _, host := range nativeInstallers(t) {
		t.Run(host.name, func(t *testing.T) {
			run := newInstallRun(t, host, "http://127.0.0.1:1")
			archives := "archives 'preview' $(touch unintended)"
			absoluteArchives := filepath.Join(run.dir, archives)
			for name, data := range assets {
				writeFile(t, filepath.Join(absoluteArchives, name), data)
			}
			archiveFlag, hostFlag := "--archive-dir", "--host"
			quote := func(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'" }
			prefix := ""
			if runtime.GOOS == "windows" {
				archiveFlag, hostFlag = "-ArchiveDir", "-AgentHost"
				quote = func(value string) string { return "'" + strings.ReplaceAll(value, "'", "''") + "'" }
				prefix = "& "
			}
			output, err := run.run(false, archiveFlag, archives, hostFlag, "claude")
			if err == nil {
				t.Fatalf("agent setup failure did not fail the installer:\n%s", output)
			}
			if _, err := os.Stat(filepath.Join(run.home, "installed-host")); !os.IsNotExist(err) {
				t.Fatalf("saving a default after setup failure: %v", err)
			}
			wantArgs := []string{"agent", "setup", "--host", "claude", "--archive-dir", absoluteArchives}
			if !strings.Contains(output, fmt.Sprintf("fixture agent arguments %q", wantArgs)) {
				t.Fatalf("agent setup did not receive the expected arguments:\n%s", output)
			}
			wantRetry := prefix + quote(run.target) + " agent setup --host claude --archive-dir " + quote(absoluteArchives)
			if !strings.Contains(output, wantRetry) {
				t.Fatalf("missing retry command %s:\n%s", wantRetry, output)
			}
			if output := run.installedVersion(); !strings.HasPrefix(output, "kitsu "+preparedVersion+"\n") {
				t.Fatalf("unexpected version after agent setup failure: %s", output)
			}
			if _, err := os.Stat(filepath.Join(run.dir, "unintended")); !os.IsNotExist(err) {
				t.Fatalf("the archive directory was interpreted as shell code: %v", err)
			}
		})
	}
}
