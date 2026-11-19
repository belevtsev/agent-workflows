package installers

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

type nativeInstallEvidence struct {
	OS           string                 `json:"os"`
	Arch         string                 `json:"arch"`
	Shell        string                 `json:"shell"`
	ShellVersion string                 `json:"shell_version,omitempty"`
	Host         string                 `json:"host"`
	HostVersion  string                 `json:"host_version,omitempty"`
	Tag          string                 `json:"tag"`
	Revision     string                 `json:"revision,omitempty"`
	Artifacts    map[string]string      `json:"artifact_sha256,omitempty"`
	Attempts     []nativeInstallAttempt `json:"attempts,omitempty"`
	ResumedTask  json.RawMessage        `json:"resumed_task,omitempty"`
	Result       string                 `json:"result"`
}

type nativeInstallAttempt struct {
	Name            string          `json:"name"`
	ExitCode        *int            `json:"installer_exit,omitempty"`
	InstallerOutput string          `json:"installer_output"`
	BinarySHA256    string          `json:"binary_sha256,omitempty"`
	SkillSHA256     string          `json:"task_skill_sha256,omitempty"`
	DiscoveredSkill string          `json:"host_discovered_skill,omitempty"`
	Check           json.RawMessage `json:"agent_check,omitempty"`
	Handoff         json.RawMessage `json:"task_preview,omitempty"`
}

func TestReleaseArtifactsInstallMatchedPlugin(t *testing.T) {
	evidencePath := os.Getenv("KITSU_NATIVE_INSTALL_EVIDENCE")
	required := evidencePath != ""
	directory := os.Getenv("KITSU_INSTALL_ARCHIVES")
	if directory == "" && !required {
		t.Skip("set KITSU_INSTALL_ARCHIVES to test the actual release artifacts")
	}
	version := os.Getenv("KITSU_INSTALL_TAG")
	if version == "" {
		version = preparedVersion
	}
	revision := os.Getenv("KITSU_INSTALL_REVISION")
	shells := installerChoices()
	expectedRows := len(shells) * len(nativeClients)
	rows := make([]*nativeInstallEvidence, 0, expectedRows)
	if required {
		t.Cleanup(func() {
			if len(rows) != expectedRows {
				t.Errorf("ran %d of %d required native installer rows", len(rows), expectedRows)
			}
			data, err := json.MarshalIndent(rows, "", "  ")
			if err != nil {
				t.Fatal(err)
			}
			writeFile(t, evidencePath, append(data, '\n'))
		})
	}
	for _, shell := range shells {
		t.Run(shell.name, func(t *testing.T) {
			program, shellErr := exec.LookPath(shell.name)
			shell.program = program
			withNativeClients(t, func(t *testing.T, host nativeClient) {
				row := &nativeInstallEvidence{OS: runtime.GOOS, Arch: runtime.GOARCH, Shell: shell.name,
					Host: host.name, Tag: version, Revision: revision}
				rows = append(rows, row)
				t.Cleanup(func() {
					if required && t.Skipped() {
						t.Error("required native installer row was skipped")
					}
					row.Result = "passed"
					if t.Failed() {
						row.Result = "failed"
					} else if t.Skipped() {
						row.Result = "skipped"
					}
				})
				if directory == "" {
					t.Fatal("KITSU_INSTALL_ARCHIVES is required for native install evidence")
				}
				if shellErr != nil {
					missingNativeCapability(t, required, "%s installer shell unavailable on %s/%s: %v", shell.name, runtime.GOOS, runtime.GOARCH, shellErr)
				}
				profile := newNativeProfile(t, host.resolve(t, required), "")
				row.ShellVersion = checkInstallerShell(t, profile, shell, required)
				row.HostVersion = profile.hostVersion()
				wantVersion := os.Getenv("KITSU_NATIVE_" + strings.ToUpper(host.name) + "_VERSION")
				if required && wantVersion != "" && !slices.Contains(strings.Fields(row.HostVersion), wantVersion) {
					t.Fatalf("%s reported %q, want version %s", host.name, row.HostVersion, wantVersion)
				}
				profile.requireMarketplaceCommands(row.HostVersion, required)
				git := profile.initRepository()
				checkMatchedArtifactInstall(t, profile, shell, directory, row, required)
				row.ResumedTask = checkExistingPatch(t, filepath.Join(profile.root, "passing-patch"), profile.kitsu, git,
					profile.commandIn, patchFixture{"passing-patch", "ready\n", "done", "passed", 0, 0})
			})
		})
	}
}

func checkInstallerShell(t *testing.T, profile nativeProfile, shell installer, required bool) string {
	t.Helper()
	if !required || runtime.GOOS != "windows" {
		return ""
	}
	version := strings.TrimSpace(string(profile.command(shell.program, "-NoProfile", "-NonInteractive", "-Command", "$PSVersionTable.PSVersion.ToString()")))
	prefix := "7."
	if shell.name == "powershell" {
		prefix = "5.1."
	}
	if !strings.HasPrefix(version, prefix) {
		t.Fatalf("%s reported %q, want a %s release", shell.name, version, prefix)
	}
	return version
}

func checkMatchedArtifactInstall(t *testing.T, profile nativeProfile, shell installer, source string, row *nativeInstallEvidence, required bool) {
	t.Helper()
	archives := filepath.Join(profile.root, "downloaded artifacts")
	sums := readFile(t, filepath.Join(source, "SHA256SUMS"))
	assets := map[string][]byte{"SHA256SUMS": sums}
	row.Artifacts = map[string]string{"SHA256SUMS": fmt.Sprintf("%x", sha256.Sum256(sums))}
	for _, name := range []string{archiveName(row.Tag), pluginArchiveName, shell.script} {
		asset := readFile(t, filepath.Join(source, name))
		row.Artifacts[name] = checkReleaseChecksum(t, sums, name, asset)
		assets[name] = asset
	}
	plugin := assets[pluginArchiveName]
	archive, err := zip.NewReader(bytes.NewReader(plugin), int64(len(plugin)))
	if err != nil {
		t.Fatal(err)
	}
	wantTaskSkill, err := fs.ReadFile(archive, "kitsu-marketplace/skills/task/SKILL.md")
	if err != nil {
		t.Fatalf("read task skill from matched plugin: %v", err)
	}

	// Use the version-free command from the artifact directory. Suppress PATH
	// edits because PowerShell otherwise changes the runner's user registry.
	env := append(profile.env, "KITSU_NO_MODIFY_PATH=1", "KITSU_DOWNLOAD_BASE=http://127.0.0.1:1")
	script := "." + string(filepath.Separator) + shell.script
	args := []string{"--archive-dir", ".", "--host", profile.host.name}
	if runtime.GOOS == "windows" {
		args = []string{"-ArchiveDir", ".", "-AgentHost", profile.host.name}
	}
	for _, name := range []string{"fresh install", "same-build reinstall"} {
		for filename, data := range assets {
			writeFile(t, filepath.Join(archives, filename), data)
		}
		row.Attempts = append(row.Attempts, nativeInstallAttempt{Name: name})
		attempt := &row.Attempts[len(row.Attempts)-1]
		output, err := invokeInstaller(t, shell, archives, env, script, false, args...)
		attempt.InstallerOutput = output
		if err != nil {
			var exit *exec.ExitError
			if errors.As(err, &exit) {
				attempt.ExitCode = new(exit.ExitCode())
			}
			t.Fatalf("%s with %s: %v\n%s", name, shell.script, err, output)
		}
		attempt.ExitCode = new(0)
		if host := strings.TrimSpace(string(profile.command(profile.kitsu, "config", "get", "host"))); host != profile.host.name {
			t.Fatalf("%s: saved host = %q, want %q", name, host, profile.host.name)
		}
		identity := strings.Fields(string(profile.command(profile.kitsu, "version")))
		if len(identity) < 4 || identity[0] != "kitsu" || identity[1] != row.Tag || identity[2] != "revision" ||
			(row.Revision != "" && identity[3] != row.Revision) {
			t.Fatalf("%s: unexpected installed CLI identity %v (tag %q, revision %q)", name, identity, row.Tag, row.Revision)
		}
		row.Revision = identity[3]
		attempt.BinarySHA256 = fmt.Sprintf("%x", sha256.Sum256(readFile(t, profile.kitsu)))
		if err := os.RemoveAll(archives); err != nil {
			t.Fatal(err)
		}
		t.Logf("%s %s exit=0 tag=%s revision=%s", shell.script, name, identity[1], identity[3])
		report, check, handoff := profile.checkHandoff()
		attempt.Check, attempt.Handoff = check, handoff
		checkMatchedTaskSkill(t, report.check("task_skill").Detail, wantTaskSkill)
		attempt.SkillSHA256 = fmt.Sprintf("%x", sha256.Sum256(wantTaskSkill))
		if required {
			attempt.DiscoveredSkill = profile.discoverTaskSkill(wantTaskSkill)
		}
	}
}

func checkMatchedTaskSkill(t *testing.T, detail string, want []byte) {
	t.Helper()
	// Read the active skill identified by the diagnostic, even when the host
	// caches it at a different location than the registered marketplace.
	path, foundPath := strings.CutPrefix(detail, "kitsu@kitsu:task at ")
	path, foundEnd := strings.CutSuffix(path, "; host discovery has not been executed.")
	if !foundPath || !foundEnd || !filepath.IsAbs(path) {
		t.Fatalf("agent check did not identify the installed task skill: %s", detail)
	}
	if !bytes.Equal(readFile(t, path), want) {
		t.Fatalf("%s differs from the task skill in the verified plugin archive", path)
	}
}
