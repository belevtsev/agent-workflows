package installers

import (
	"bytes"
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
				output, err := run.run(false, hostFlag, tc.host)
				if tc.fail {
					if err == nil || !strings.Contains(output, plugin) || !bytes.Equal(readFile(t, run.target), previous) {
						t.Fatalf("failed bundle must report the plugin and preserve the CLI: %v\n%s", err, output)
					}
					return
				}
				if err != nil || !strings.HasPrefix(run.installedVersion(), "kitsu "+version+"\n") {
					t.Fatalf("matching bundle install: %v\n%s", err, output)
				}
			})
		}
	}
}
