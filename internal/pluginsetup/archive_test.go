package pluginsetup

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReleaseRequiresOneMatchingChecksum(t *testing.T) {
	t.Parallel()
	data := fixtureZIP(t, releaseFiles("1.2.3", "task skill"))
	valid := fmt.Sprintf("%x  %s\n", sha256.Sum256(data), archiveName)
	tests := []struct{ name, sums, wantError string }{
		{name: "one entry among other artifacts", sums: valid + strings.Repeat("0", 64) + "  kitsu.tar.gz\n"},
		{name: "uppercase binary digest with CRLF", sums: fmt.Sprintf("%X *%s\r\n", sha256.Sum256(data), archiveName)},
		{name: "missing entry", sums: "ignored  other.zip\n", wantError: "exactly once"},
		{name: "duplicate entry", sums: valid + valid, wantError: "exactly once"},
		{name: "malformed duplicate", sums: valid + "bad  *" + archiveName + "\n", wantError: "exactly once"},
		{name: "unexpected fields", sums: strings.TrimSpace(valid) + " unexpected\n", wantError: "exactly once"},
		{name: "wrong digest", sums: strings.Repeat("0", 64) + "  " + archiveName + "\n", wantError: "checksum mismatch"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			writeFixture(t, filepath.Join(dir, archiveName), data)
			writeFixture(t, filepath.Join(dir, "SHA256SUMS"), []byte(test.sums))
			_, err := loadRelease(dir, "codex")
			if test.wantError == "" && err != nil {
				t.Fatal(err)
			}
			if test.wantError != "" && (err == nil || !strings.Contains(err.Error(), test.wantError)) {
				t.Fatalf("load release = %v, want %q", err, test.wantError)
			}
		})
	}
}

func TestReleaseRejectsUnsafeArchiveMembers(t *testing.T) {
	t.Parallel()
	market := marketplaceName + "/"
	regular := func(name string) zipEntry { return zipEntry{name: name, contents: "content"} }
	tests := []struct {
		name    string
		entries []zipEntry
	}{
		{name: "outside marketplace", entries: []zipEntry{regular("other/file")}},
		{name: "absolute path", entries: []zipEntry{regular("/" + market + "file")}},
		{name: "traversal", entries: []zipEntry{regular(market + "../file")}},
		{name: "backslash", entries: []zipEntry{regular(market + `dir\file`)}},
		{name: "alternate stream", entries: []zipEntry{regular(market + "file:stream")}},
		{name: "symbolic link", entries: []zipEntry{{name: market + "link", mode: os.ModeSymlink | 0o777}}},
		{name: "duplicate file", entries: []zipEntry{regular(market + "file"), regular(market + "file")}},
		{name: "case collision", entries: []zipEntry{regular(market + "Dir/a"), regular(market + "dir/b")}},
		{name: "file and directory conflict", entries: []zipEntry{regular(market + "dir/file"), regular(market + "dir")}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			data := fixtureZIP(t, append(releaseFiles("1.2.3", "skill"), test.entries...))
			dir := writeBundle(t, data)
			if _, err := loadRelease(dir, "codex"); err == nil {
				t.Fatal("unsafe release was accepted")
			}
		})
	}
}

func TestSavedMarketplaceReusesOnlyVerifiedBytes(t *testing.T) {
	t.Parallel()
	r := releaseFixture(t, "1.2.3", "task skill")
	store := t.TempDir()
	dir, err := r.save(store)
	if want := filepath.Join(store, r.digest, marketplaceName); dir != want || err != nil {
		t.Fatalf("save marketplace = %q, %v, want %q", dir, err, want)
	}
	if again, err := r.save(store); again != dir || err != nil {
		t.Fatalf("repeat save = %q, %v, want %q", again, err, dir)
	}
	skill := filepath.Join(dir, "skills", "task", "SKILL.md")
	writeFixture(t, skill, []byte("stale copy"))
	if _, err := r.save(store); err == nil || !strings.Contains(err.Error(), "saved marketplace") {
		t.Fatalf("repeat save with changed bytes = %v", err)
	}
	writeFixture(t, skill, []byte("task skill"))
	writeFixture(t, filepath.Join(dir, "unexpected"), []byte("extra content"))
	if _, err := r.save(store); err == nil || !strings.Contains(err.Error(), "unexpected marketplace entry") {
		t.Fatalf("repeat save with extra files = %v", err)
	}
}

type zipEntry struct {
	name, contents string
	mode           os.FileMode
}

func releaseFiles(version, skill string) []zipEntry {
	files := []zipEntry{
		{name: marketManifest("codex"), contents: `{"name":"kitsu","plugins":[{"name":"kitsu","source":{"source":"local","path":"./"}}]}`},
		{name: marketManifest("claude"), contents: `{"name":"kitsu","plugins":[{"name":"kitsu","source":"./"}]}`},
		{name: pluginManifest("codex"), contents: fmt.Sprintf(`{"name":"kitsu","version":%q}`, version)},
		{name: pluginManifest("claude"), contents: fmt.Sprintf(`{"name":"kitsu","version":%q}`, version)},
		{name: "skills/task/SKILL.md", contents: skill},
	}
	for i := range files {
		files[i].name = marketplaceName + "/" + files[i].name
	}
	return files
}

func fixtureZIP(t *testing.T, entries []zipEntry) []byte {
	t.Helper()
	var data bytes.Buffer
	w := zip.NewWriter(&data)
	for _, entry := range entries {
		header := &zip.FileHeader{Name: entry.name}
		if entry.mode != 0 {
			header.SetMode(entry.mode)
		}
		file, err := w.CreateHeader(header)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := file.Write([]byte(entry.contents)); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return data.Bytes()
}

func writeBundle(t *testing.T, data []byte) string {
	t.Helper()
	dir := t.TempDir()
	writeFixture(t, filepath.Join(dir, archiveName), data)
	writeFixture(t, filepath.Join(dir, "SHA256SUMS"), fmt.Appendf(nil, "%x  %s\n", sha256.Sum256(data), archiveName))
	return dir
}

func releaseFixture(t *testing.T, version, skill string) release {
	t.Helper()
	r, err := loadRelease(writeBundle(t, fixtureZIP(t, releaseFiles(version, skill))), "codex")
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func writeFixture(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

// A marketplace is Kitsu's own when it lists the default plugin at its root,
// alone or followed by the opt-in plugin in its own directory.
func TestMarketplaceListsTheDefaultPluginAndAtMostTheOptInOne(t *testing.T) {
	entry := func(host, name, path string) string {
		if host == "codex" {
			return fmt.Sprintf(`{"name":%q,"source":{"source":"local","path":%q}}`, name, path)
		}
		return fmt.Sprintf(`{"name":%q,"source":%q}`, name, path)
	}
	for _, host := range []string{"codex", "claude"} {
		kitsu, team := entry(host, "kitsu", "./"), entry(host, "kitsu-team", "./plugins/kitsu-team")
		for plugins, want := range map[string]bool{
			kitsu:               true,
			kitsu + "," + team:  true,
			"":                  false,
			team:                false,
			team + "," + kitsu:  false,
			kitsu + "," + kitsu: false,
			kitsu + "," + entry(host, "kitsu-team", "./"):                  false,
			kitsu + "," + entry(host, "other", "./plugins/kitsu-team"):     false,
			kitsu + "," + team + "," + entry(host, "third", "./plugins/x"): false,
			entry(host, "kitsu", "./plugins/kitsu-team"):                   false,
		} {
			if got := isKitsuMarketplace(host, []byte(`{"name":"kitsu","plugins":[`+plugins+`]}`)); got != want {
				t.Errorf("%s marketplace with [%s] accepted = %v, want %v", host, plugins, got, want)
			}
		}
		if isKitsuMarketplace(host, []byte(`{"name":"other","plugins":[`+kitsu+`]}`)) {
			t.Errorf("%s accepted a marketplace of another name", host)
		}
	}
	if isKitsuMarketplace("codex", []byte(`{"name":"kitsu","plugins":[{"name":"kitsu","source":{"source":"git","path":"./"}}]}`)) {
		t.Error("Codex accepted a plugin that is not local")
	}
}
