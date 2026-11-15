package installers

import (
	"bytes"
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

// CI supplies real GoReleaser assets; ordinary tests use native executable fixtures.
func TestReleaseArtifactsInstallWithoutBuildTools(t *testing.T) {
	directory := os.Getenv("KITSU_INSTALL_ARCHIVES")
	if directory == "" {
		t.Skip("set KITSU_INSTALL_ARCHIVES to test the actual release artifacts")
	}
	version := os.Getenv("KITSU_INSTALL_TAG")
	if version == "" {
		version = preparedVersion
	}
	assets := map[string][]byte{}
	for _, name := range []string{archiveName(version), "SHA256SUMS", "install.sh", "install.ps1"} {
		assets[name] = readFile(t, filepath.Join(directory, name))
	}
	for _, host := range nativeInstallers(t) {
		t.Run(host.name, func(t *testing.T) {
			base := serveAssets(t, assets)
			run := newInstallRun(t, host, base)
			run.env["KITSU_VERSION"] = version
			script := filepath.Join(run.dir, host.script)
			downloadInstaller(t, base, host.script, script)
			if output, err := run.runScript(script, true); err != nil {
				t.Fatalf("release artifact installation: %v\n%s", err, output)
			}
			output := run.installedVersion()
			if !strings.Contains(output, "kitsu "+version) || !strings.Contains(output, "revision "+os.Getenv("KITSU_INSTALL_REVISION")) {
				t.Fatalf("the installed binary does not identify this release:\n%s", output)
			}
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
	for _, host := range nativeInstallers(t) {
		t.Run(host.name, func(t *testing.T) {
			run := newInstallRun(t, host, base)
			run.env["KITSU_VERSION"] = version
			script := filepath.Join(run.dir, host.script)
			downloadInstaller(t, base, host.script, script)
			if output, err := run.runScript(script, true); err != nil {
				t.Fatalf("published installer: %v\n%s", err, output)
			}
			if output := run.installedVersion(); !strings.Contains(output, "kitsu "+version) {
				t.Fatalf("the published installer selected another version: %s", output)
			} else if revision := os.Getenv("KITSU_INSTALL_REVISION"); revision != "" && !strings.Contains(output, "revision "+revision) {
				t.Fatalf("the published installer selected another commit: %s", output)
			}
		})
	}
}
