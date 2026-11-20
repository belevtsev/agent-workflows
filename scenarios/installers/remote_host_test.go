package installers

import (
	"bytes"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestRemoteMatchedHostInstall(t *testing.T) {
	const version = "v1.2.3"
	const plugin = "kitsu-plugin.zip"
	original := fixtureAssets(t, version)
	payload := []byte("paired plugin")
	row := checksumLine(plugin, payload)
	for _, shell := range nativeInstallers(t) {
		hostFlag := "--host"
		if runtime.GOOS == "windows" {
			hostFlag = "-AgentHost"
		}
		for _, tc := range []struct {
			name, host string
			rows, data []byte
			fail       bool
		}{
			{"codex", "codex", row, payload, false},
			{"claude", "claude", row, payload, false},
			{"corrupt plugin", "claude", row, []byte("other build"), true},
			{"missing asset", "claude", row, nil, true},
			{"duplicate plugin", "codex", bytes.Repeat(row, 2), payload, true},
			{"missing checksum", "codex", nil, payload, true},
		} {
			t.Run(shell.name+"/"+tc.name, func(t *testing.T) {
				assets := map[string][]byte{
					archiveName(version): original[archiveName(version)],
					"SHA256SUMS":         bytes.Join([][]byte{original["SHA256SUMS"], tc.rows}, nil),
				}
				if tc.data != nil {
					assets[plugin] = tc.data
				}
				run := newInstallRun(t, shell, serveAssets(t, assets))
				run.env["KITSU_TEST_SETUP_HOST"] = tc.host
				run.env["KITSU_INSTALL_TEST_VERSION"] = version
				previous := []byte("previous installation")
				writeFile(t, run.target, previous)
				hostFile := filepath.Join(run.home, "installed-host")
				writeFile(t, hostFile, []byte("previous host"))
				output, err := run.run(false, hostFlag, tc.host)
				if tc.fail {
					if err == nil || !strings.Contains(output, plugin) || !bytes.Equal(readFile(t, run.target), previous) {
						t.Fatalf("failed bundle must report the plugin and preserve the CLI: %v\n%s", err, output)
					}
					if got := string(readFile(t, hostFile)); got != "previous host" {
						t.Fatalf("failed install changed default host to %q", got)
					}
					return
				}
				if err != nil || !strings.HasPrefix(run.installedVersion(), "kitsu "+version+"\n") {
					t.Fatalf("matching bundle install: %v\n%s", err, output)
				}
				if got := string(readFile(t, hostFile)); got != tc.host {
					t.Fatalf("default host = %q, want %q", got, tc.host)
				}
				other := "claude"
				if tc.host == "claude" {
					other = "codex"
				}
				run.env["KITSU_TEST_SETUP_HOST"] = other
				if output, err := run.run(false, hostFlag, other); err != nil || string(readFile(t, hostFile)) != tc.host {
					t.Fatalf("second install changed the saved default: %v\n%s", err, output)
				}
				opt := "--set-default"
				if runtime.GOOS == "windows" {
					opt = "-SetDefault"
				}
				if output, err := run.run(false, hostFlag, other, opt); err != nil || string(readFile(t, hostFile)) != other {
					t.Fatalf("explicit default was not saved: %v\n%s", err, output)
				}
			})
		}
	}
}

func TestCLIOnlyUsesLatestManifestVersion(t *testing.T) {
	const version = "v1.6.0"
	for _, shell := range nativeInstallers(t) {
		t.Run(shell.name, func(t *testing.T) {
			run := newInstallRun(t, shell, serveAssets(t, fixtureAssets(t, version)))
			run.env["KITSU_INSTALL_TEST_VERSION"] = version
			flag := "--cli-only"
			if runtime.GOOS == "windows" {
				flag = "-CliOnly"
			}
			if output, err := run.run(false, flag); err != nil || !strings.HasPrefix(run.installedVersion(), "kitsu "+version+"\n") {
				t.Fatalf("CLI-only install did not use the manifest's release: %v\n%s", err, output)
			}
		})
	}
}
