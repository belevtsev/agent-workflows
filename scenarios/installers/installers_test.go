package installers

import (
	"bytes"
	"crypto/sha256"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestInstallWithoutCloneOrBuildTools(t *testing.T) {
	assets := fixtureAssets(t, preparedVersion)
	for _, host := range nativeInstallers(t) {
		t.Run(host.name, func(t *testing.T) {
			base := serveAssets(t, assets)
			run := newInstallRun(t, host, base)
			for attempt := range 2 {
				output, err := run.run(true)
				if err != nil {
					t.Fatalf("install attempt %d: %v\n%s", attempt+1, err, output)
				}
				if output := run.installedVersion(); !strings.Contains(output, "kitsu "+preparedVersion) {
					t.Fatalf("unexpected installed version: %s", output)
				}
			}
		})
	}
}

func TestInstallFailuresPreserveExistingBinary(t *testing.T) {
	good := fixtureAssets(t, preparedVersion)
	name := archiveName(preparedVersion)
	cases := []struct {
		name  string
		alter func(map[string][]byte)
	}{
		{"checksum mismatch", func(files map[string][]byte) {
			data := bytes.Clone(files[name])
			offset := 9 // The gzip OS byte does not affect decompression.
			if runtime.GOOS == "windows" {
				offset = 10 // ZIP timestamp metadata; preserve its compression method and payload.
			}
			data[offset] ^= 1
			assertReadableArchive(t, data)
			files[name] = data
		}},
		{"missing checksum", func(files map[string][]byte) { delete(files, "SHA256SUMS") }},
		{"missing checksum row", func(files map[string][]byte) { files["SHA256SUMS"] = checksumLine("another.tar.gz", files[name]) }},
		{"duplicate checksum row", func(files map[string][]byte) {
			files["SHA256SUMS"] = append(bytes.Clone(files["SHA256SUMS"]), files["SHA256SUMS"]...)
		}},
		{"invalid checksum", func(files map[string][]byte) { files["SHA256SUMS"] = []byte("not-a-checksum  " + name + "\n") }},
		{"missing archive", func(files map[string][]byte) { delete(files, name) }},
		{"missing binary", func(files map[string][]byte) {
			files[name] = archiveBytes(t, map[string][]byte{"LICENSE": []byte("fixture")})
			files["SHA256SUMS"] = checksumLine(name, files[name])
		}},
		{"empty binary", func(files map[string][]byte) {
			files[name] = archiveBytes(t, map[string][]byte{binaryName(): {}})
			files["SHA256SUMS"] = checksumLine(name, files[name])
		}},
	}
	for _, host := range nativeInstallers(t) {
		for _, test := range cases {
			t.Run(host.name+"/"+test.name, func(t *testing.T) {
				assets := make(map[string][]byte, len(good))
				for key, value := range good {
					assets[key] = value
				}
				test.alter(assets)
				run := newInstallRun(t, host, serveAssets(t, assets))
				original := []byte("the existing installation")
				writeFile(t, run.target, original)
				if output, err := run.run(false); err == nil {
					t.Fatalf("bad release was installed:\n%s", output)
				}
				if actual := readFile(t, run.target); !bytes.Equal(actual, original) {
					t.Fatal("a failed installation changed the existing binary")
				}
			})
		}
	}
}

func TestTruncatedShellInstallerPreservesInstallation(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires the POSIX installer")
	}
	script := readFile(t, filepath.Join("..", "..", "install.sh"))
	base := serveAssets(t, fixtureAssets(t, preparedVersion))
	for _, point := range []string{"# Parse the entire", "configure_path\nif", "\n}\n"} {
		t.Run(point, func(t *testing.T) {
			end := bytes.LastIndex(script, []byte(point))
			if end < 0 {
				t.Fatalf("truncation point %q missing from the installer", point)
			}
			run := newInstallRun(t, nativeInstallers(t)[0], base)
			run.env["KITSU_NO_MODIFY_PATH"] = "0"
			profile := filepath.Join(run.home, ".zshrc")
			original := []byte("existing binary")
			setup := []byte("# existing shell configuration\n")
			writeFile(t, run.target, original)
			writeFile(t, profile, setup)
			prefix := filepath.Join(run.dir, "truncated.sh")
			writeFile(t, prefix, script[:end])
			output, err := run.runScript(prefix, true)
			if err == nil {
				t.Fatalf("incomplete installer reported success: %s", output)
			}
			if !bytes.Equal(readFile(t, run.target), original) || !bytes.Equal(readFile(t, profile), setup) {
				t.Fatal("incomplete installer changed the binary or shell setup")
			}
		})
	}
}

func TestInstallPinnedVersionAndOfflineArchives(t *testing.T) {
	const version = "v3.0.1"
	assets := fixtureAssets(t, version)
	for _, host := range nativeInstallers(t) {
		t.Run(host.name, func(t *testing.T) {
			run := newInstallRun(t, host, "http://127.0.0.1:1")
			archives := filepath.Join(run.dir, "downloaded artifacts")
			for name, data := range assets {
				writeFile(t, filepath.Join(archives, name), data)
			}
			destination := filepath.Join(run.dir, "bin 'quoted' $(touch unintended)")
			run.target = filepath.Join(destination, binaryName())
			run.env["KITSU_INSTALL_TEST_VERSION"] = version
			run.env["KITSU_VERSION"] = "v0.0.1"
			run.env["KITSU_INSTALL_DIR"] = filepath.Join(run.dir, "ignored destination")
			run.env["KITSU_ARCHIVE_DIR"] = filepath.Join(run.dir, "ignored archives")
			args := []string{"--version", version, "--archive-dir", archives, "--install-dir", destination, "--no-modify-path"}
			if runtime.GOOS == "windows" {
				args = []string{"-Version", version, "-ArchiveDir", archives, "-InstallDir", destination, "-NoModifyPath"}
			}
			if output, err := run.run(false, args...); err != nil {
				t.Fatalf("offline install: %v\n%s", err, output)
			}
			if output := run.installedVersion(); !strings.Contains(output, "kitsu "+version) {
				t.Fatalf("the requested version was not installed: %s", output)
			}
			if _, err := os.Stat(filepath.Join(run.dir, "unintended")); !os.IsNotExist(err) {
				t.Fatalf("the destination was interpreted as shell code: %v", err)
			}
		})
	}
}

func TestShellPathSetupIsIdempotent(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell startup files are a POSIX installation concern")
	}
	for _, host := range nativeInstallers(t) {
		t.Run(host.name, func(t *testing.T) {
			run := newInstallRun(t, host, serveAssets(t, fixtureAssets(t, preparedVersion)))
			run.env["KITSU_NO_MODIFY_PATH"] = "0"
			run.env["KITSU_INSTALL_DIR"] = filepath.Join(run.home, "bin 'quoted' $(touch unintended)")
			run.target = filepath.Join(run.env["KITSU_INSTALL_DIR"], binaryName())
			profile := filepath.Join(run.home, ".zshrc")
			writeFile(t, profile, []byte("# existing shell configuration\n"))
			var first []byte
			for attempt := range 2 {
				if output, err := run.run(true); err != nil {
					t.Fatalf("install with PATH setup: %v\n%s", err, output)
				} else if !strings.Contains(string(output), "Use kitsu in this shell now: export PATH=") {
					t.Fatal("missing current-shell setup", string(output))
				}
				contents := readFile(t, profile)
				if attempt == 0 {
					first = contents
				} else if !bytes.Equal(contents, first) {
					t.Fatal("reinstalling duplicated or changed the shell PATH setup")
				}
			}
			if !bytes.HasPrefix(first, []byte("# existing shell configuration\n")) {
				t.Fatalf("PATH setup lost the existing configuration:\n%s", first)
			}
			command := exec.CommandContext(t.Context(), host.program, "-c", `. "$1"; . "$1"; command -v kitsu`, "test-shell", profile)
			command.Dir, command.Env = run.dir, run.environment()
			if output, err := command.CombinedOutput(); err != nil || strings.TrimSpace(string(output)) != run.target {
				t.Fatalf("the updated profile does not find the installed binary: %v\n%s", err, output)
			}
			if _, err := os.Stat(filepath.Join(run.dir, "unintended")); !os.IsNotExist(err) {
				t.Fatalf("the PATH setup interpreted the directory as shell code: %v", err)
			}
		})
	}
}

func TestBashPathSetupCoversLoginAndInteractiveShells(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("bash startup files are a POSIX installation concern")
	}
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash is not installed")
	}
	base := serveAssets(t, fixtureAssets(t, preparedVersion))
	cases := []struct {
		name     string
		profiles []string
	}{
		{"no login profile", nil},
		{"bash profile", []string{".bash_profile", ".bash_login", ".profile"}},
		{"bash login", []string{".bash_login", ".profile"}},
		{"profile", []string{".profile"}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			run := newInstallRun(t, nativeInstallers(t)[0], base)
			run.env["SHELL"] = bash
			run.env["KITSU_NO_MODIFY_PATH"] = "0"
			for _, profile := range test.profiles {
				writeFile(t, filepath.Join(run.home, profile), []byte("# existing shell configuration\n"))
			}
			if output, err := run.run(true); err != nil {
				t.Fatalf("install with bash PATH setup: %v\n%s", err, output)
			}
			for _, shell := range []struct {
				name string
				args []string
			}{
				{"login", []string{"--login", "-c", "command -v kitsu"}},
				{"interactive", []string{"--noprofile", "--rcfile", filepath.Join(run.home, ".bashrc"), "-i", "-c", "command -v kitsu"}},
			} {
				t.Run(shell.name, func(t *testing.T) {
					command := exec.CommandContext(t.Context(), bash, shell.args...)
					command.Dir, command.Env = run.dir, run.environment()
					if output, err := command.Output(); err != nil || strings.TrimSpace(string(output)) != run.target {
						t.Fatalf("new bash shell does not find the installed binary: %v\n%s", err, output)
					}
				})
			}
		})
	}
}

// CI supplies real GoReleaser assets; ordinary tests use native executable fixtures.
func TestReleaseArtifactsInstallWithoutBuildTools(t *testing.T) {
	directory := os.Getenv("KITSU_INSTALL_ARCHIVES")
	if directory == "" {
		t.Skip("set KITSU_INSTALL_ARCHIVES to test the actual release artifacts")
	}
	directory, err := filepath.Abs(directory)
	if err != nil {
		t.Fatal(err)
	}
	version := os.Getenv("KITSU_INSTALL_TAG")
	if version == "" {
		version = preparedVersion
	}
	assets := map[string][]byte{}
	for _, name := range []string{archiveName(version), pluginArchiveName, "SHA256SUMS", "install.sh", "install.ps1"} {
		assets[name] = readFile(t, filepath.Join(directory, name))
	}
	for i, host := range nativeInstallers(t) {
		t.Run(host.name+"/download", func(t *testing.T) {
			installed, plugin := checkReleaseInstallation(t, host, version, serveAssets(t, assets), "")
			if i == 0 {
				checkNativeMarketplaces(t, installed, plugin)
			}
		})
		t.Run(host.name+"/offline", func(t *testing.T) {
			checkReleaseInstallation(t, host, version, "http://127.0.0.1:1", directory)
		})
	}
}

func TestPublishedReleaseInstaller(t *testing.T) {
	if os.Getenv("KITSU_INSTALL_PUBLISHED") != "1" {
		t.Skip("the release workflow enables this after publication")
	}
	version := os.Getenv("KITSU_INSTALL_TAG")
	if version == "" {
		t.Fatal("KITSU_INSTALL_TAG must identify the published release")
	}
	base := "https://github.com/belevtsev/kitsu/releases/download/" + version
	for i, host := range nativeInstallers(t) {
		t.Run(host.name, func(t *testing.T) {
			installed, plugin := checkReleaseInstallation(t, host, version, base, "")
			if i == 0 {
				checkNativeMarketplaces(t, installed, plugin)
			}
		})
	}
}

func checkReleaseInstallation(t *testing.T, host installer, version, base, archiveDir string) (string, []byte) {
	t.Helper()
	run := newInstallRun(t, host, base)
	run.env["KITSU_VERSION"] = version
	load := func(name string, limit int64) []byte {
		if archiveDir != "" {
			return readFile(t, filepath.Join(archiveDir, name))
		}
		return fetchReleaseAsset(t, base, name, limit)
	}
	sums := load("SHA256SUMS", 1<<20)
	script := filepath.Join(run.dir, host.script)
	scriptData := load(host.script, 1<<20)
	checkReleaseChecksum(t, sums, host.script, scriptData)
	writeFile(t, script, scriptData)
	plugin := load(pluginArchiveName, 32<<20)
	checkReleaseChecksum(t, sums, pluginArchiveName, plugin)
	var args []string
	if archiveDir != "" {
		flag := "--archive-dir"
		if runtime.GOOS == "windows" {
			flag = "-ArchiveDir"
		}
		args = []string{flag, archiveDir}
	}
	if output, err := run.runScript(script, archiveDir == "", args...); err != nil {
		t.Fatalf("release artifact installation: %v\n%s", err, output)
	}
	identity := strings.Fields(run.installedVersion())
	if len(identity) < 4 || identity[0] != "kitsu" || identity[1] != version || identity[2] != "revision" {
		t.Fatalf("unexpected installed CLI identity: %v", identity)
	}
	if revision := os.Getenv("KITSU_INSTALL_REVISION"); revision != "" && identity[3] != revision {
		t.Fatalf("installed CLI revision %s, want %s", identity[3], revision)
	}
	t.Logf("%s/%s installer=%s exit=0 tag=%s revision=%s CLI sha256=%x plugin sha256=%x",
		runtime.GOOS, runtime.GOARCH, host.name, version, identity[3], sha256.Sum256(readFile(t, run.target)), sha256.Sum256(plugin))
	checkInstalledMarketplace(t, run, plugin)
	checkInstalledHarness(t, run)
	return run.target, plugin
}
