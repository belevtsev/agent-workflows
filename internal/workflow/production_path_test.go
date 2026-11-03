package workflow

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

const productionProfileStart = "# >>> agent-workflows PATH >>>"
const productionProfileEnd = "# <<< agent-workflows PATH <<<"
const productionProfileBody = productionProfileStart + "\nexport PATH=\"/fixture/bin:$PATH\"\n" + productionProfileEnd + "\n"

func productionProfileLocate(test *testing.T, data []byte, expected string) (int, int, error) {
	test.Helper()
	defer func() {
		if recovered := recover(); recovered != nil {
			test.Fatalf("profileLocate panicked for %q: %v", data, recovered)
		}
	}()
	return profileLocate(data, expected)
}

func TestProductionProfileLocateRejectsMalformedOwnershipWithoutMutation(test *testing.T) {
	cases := []struct {
		name string
		data string
	}{
		{"reversed", productionProfileEnd + "\n" + productionProfileStart + "\n"},
		{"reversed-blank-line", productionProfileEnd + "\n\n" + productionProfileStart + "\n"},
		{"reversed-several-blank-lines", productionProfileEnd + "\n\n\n" + productionProfileStart + "\n"},
		{"reversed-with-body", productionProfileEnd + "\n\n" + productionProfileStart + "\nexport PATH=\"/fixture/bin:$PATH\"\n"},
		{"duplicate-span", productionProfileBody + productionProfileBody},
		{"duplicate-start", productionProfileStart + "\n" + productionProfileBody},
		{"duplicate-end", productionProfileBody + productionProfileEnd + "\n"},
		{"start-only", productionProfileStart + "\n"},
		{"end-only", productionProfileEnd + "\n"},
		{"missing-end", productionProfileStart + "\nexport PATH=\"/fixture/bin:$PATH\"\n"},
		{"malformed-start", productionProfileStart + " extra\nexport PATH=\"/fixture/bin:$PATH\"\n" + productionProfileEnd + "\n"},
		{"malformed-end", productionProfileStart + "\nexport PATH=\"/fixture/bin:$PATH\"\n" + productionProfileEnd + " extra\n"},
		{"changed-owned-body", productionProfileStart + "\nexport PATH=\"/different/bin:$PATH\"\n" + productionProfileEnd + "\n"},
		{"missing-body-newline", productionProfileStart + "\nexport PATH=\"/fixture/bin:$PATH\"" + productionProfileEnd + "\n"},
	}
	for _, style := range []struct {
		name    string
		leading string
	}{{"plain", ""}, {"leading-newline", "\n"}} {
		for _, entry := range cases {
			test.Run(style.name+"/"+entry.name, func(test *testing.T) {
				path := filepath.Join(test.TempDir(), "profile")
				data := []byte(entry.data)
				before := append([]byte(nil), data...)
				modelTestWrite(test, path, data, 0o640)
				if _, _, err := productionProfileLocate(test, data, style.leading+productionProfileBody); err == nil {
					test.Fatal("malformed owned PATH markers were accepted")
				}
				if !bytes.Equal(data, before) || !bytes.Equal(modelTestRead(test, path), before) {
					test.Fatal("ownership failure changed input or profile bytes")
				}
				sharedInstallerAssertMode(test, path, 0o640)
			})
		}
	}
}

func TestProductionProfileLocatePreservesExactOwnedBounds(test *testing.T) {
	cases := []struct {
		name     string
		prefix   string
		expected string
		suffix   string
	}{
		{"only-span", "", productionProfileBody, ""},
		{"personal-prefix-and-suffix", "# personal prefix\n", productionProfileBody, "# personal suffix\n"},
		{"separate-blank-lines", "# prefix\n\n", productionProfileBody, "\n\n# suffix"},
		{"owned-leading-newline", "# prefix without final newline", "\n" + productionProfileBody, "# suffix"},
		{"leading-newline-only", "", "\n" + productionProfileBody, ""},
		{"leading-and-trailing-personal-newlines", "# prefix\n", "\n" + productionProfileBody, "\n# suffix\n"},
	}
	for _, entry := range cases {
		test.Run(entry.name, func(test *testing.T) {
			data := []byte(entry.prefix + entry.expected + entry.suffix)
			before := append([]byte(nil), data...)
			start, end, err := productionProfileLocate(test, data, entry.expected)
			if err != nil || start != len(entry.prefix) || end != len(entry.prefix)+len(entry.expected) {
				test.Fatalf("wrong exact ownership bounds: start=%d end=%d err=%v", start, end, err)
			}
			if string(data[:start]) != entry.prefix || string(data[start:end]) != entry.expected || string(data[end:]) != entry.suffix || !bytes.Equal(data, before) {
				test.Fatal("owned span changed or absorbed unrelated prefix/suffix")
			}
		})
	}
}

func TestProductionProfileLocateRejectsMissingTerminalNewlineWithoutMutation(test *testing.T) {
	for _, style := range []struct {
		name    string
		leading string
	}{{"plain", ""}, {"leading-newline", "\n"}} {
		for _, expectedEnding := range []struct {
			name       string
			terminated bool
		}{{"terminated-expected", true}, {"unterminated-expected", false}} {
			test.Run(style.name+"/"+expectedEnding.name, func(test *testing.T) {
				path := filepath.Join(test.TempDir(), "profile")
				unterminated := style.leading + strings.TrimSuffix(productionProfileBody, "\n")
				data := []byte("# prefix\n" + unterminated)
				before := append([]byte(nil), data...)
				expected := unterminated
				if expectedEnding.terminated {
					expected += "\n"
				}
				modelTestWrite(test, path, data, 0o640)
				if _, _, err := productionProfileLocate(test, data, expected); err == nil {
					test.Fatal("owned span without its terminal newline was accepted")
				}
				if !bytes.Equal(data, before) || !bytes.Equal(modelTestRead(test, path), before) {
					test.Fatal("missing-newline ownership refusal changed input or profile bytes")
				}
				sharedInstallerAssertMode(test, path, 0o640)
			})
		}
	}
}

func productionPathRequireUnix(test *testing.T) {
	test.Helper()
	if runtime.GOOS == "windows" || runtime.GOOS == "plan9" || runtime.GOOS == "js" || runtime.GOOS == "wasip1" {
		test.Skip("Unix execution-permission fixture")
	}
}

func productionPathWriteExecutable(test *testing.T, directory, name string, mode os.FileMode) string {
	test.Helper()
	if err := os.MkdirAll(directory, 0o755); err != nil {
		test.Fatal(err)
	}
	path := filepath.Join(directory, name)
	modelTestWrite(test, path, []byte("#!/bin/sh\nexit 0\n"), mode)
	return path
}

func productionPathAssertEnvironmentUnchanged(test *testing.T, before string) {
	test.Helper()
	if after := os.Getenv("PATH"); after != before {
		test.Fatalf("lookPath changed process PATH: before=%q after=%q", before, after)
	}
}

func TestProductionLookPathUsesSuppliedOrderAndSkipsIneligibleCandidates(test *testing.T) {
	productionPathRequireUnix(test)
	originalPath := os.Getenv("PATH")
	defer productionPathAssertEnvironmentUnchanged(test, originalPath)
	root := test.TempDir()
	first := filepath.Join(root, "first")
	second := filepath.Join(root, "second")
	third := filepath.Join(root, "third")
	const name = "production-executable"
	productionPathWriteExecutable(test, first, name, 0o600)
	want := productionPathWriteExecutable(test, second, name, 0o755)
	productionPathWriteExecutable(test, third, name, 0o755)
	pathList := strings.Join([]string{first, second, third}, string(os.PathListSeparator))
	if got, err := lookPath(name, pathList); err != nil || got != want {
		test.Fatalf("nonexecutable first entry did not fall through in supplied order: %q, %v", got, err)
	}
	if err := os.Remove(filepath.Join(first, name)); err != nil {
		test.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(first, name), 0o755); err != nil {
		test.Fatal(err)
	}
	if got, err := lookPath(name, pathList); err != nil || got != want {
		test.Fatalf("directory candidate was not skipped: %q, %v", got, err)
	}
	reversed := strings.Join([]string{third, second}, string(os.PathListSeparator))
	if got, err := lookPath(name, reversed); err != nil || got != filepath.Join(third, name) {
		test.Fatalf("supplied path order was ignored: %q, %v", got, err)
	}
}

func TestProductionLookPathUsesEffectiveOSExecuteEligibility(test *testing.T) {
	productionPathRequireUnix(test)
	originalPath := os.Getenv("PATH")
	defer productionPathAssertEnvironmentUnchanged(test, originalPath)
	root := test.TempDir()
	first := filepath.Join(root, "owner-no-execute")
	second := filepath.Join(root, "eligible")
	const name = "production-permission-probe"
	ineligible := productionPathWriteExecutable(test, first, name, 0o641)
	if _, err := exec.LookPath(ineligible); err == nil {
		test.Skip("effective OS identity permits this user-owned 0641 fixture")
	}
	want := productionPathWriteExecutable(test, second, name, 0o755)
	if _, err := exec.LookPath(want); err != nil {
		test.Fatalf("0755 reference executable is not eligible: %v", err)
	}
	if got, err := lookPath(name, first+string(os.PathListSeparator)+second); err != nil || got != want {
		test.Fatalf("mode-bit candidate displaced an effectively executable candidate: %q, %v", got, err)
	}
	if got, err := lookPath(ineligible, second); err == nil {
		test.Fatalf("direct ineligible 0641 candidate accepted: %q", got)
	}
}

func TestProductionLookPathDirectFilesRequireRegularExecutable(test *testing.T) {
	productionPathRequireUnix(test)
	originalPath := os.Getenv("PATH")
	defer productionPathAssertEnvironmentUnchanged(test, originalPath)
	root := test.TempDir()
	executable := productionPathWriteExecutable(test, root, "direct", 0o755)
	if got, err := lookPath(executable, filepath.Join(root, "not-a-search-path")); err != nil || got != executable {
		test.Fatalf("direct 0755 executable rejected: %q, %v", got, err)
	}
	nonexecutable := productionPathWriteExecutable(test, root, "nonexecutable", 0o600)
	for _, path := range []string{nonexecutable, root} {
		if got, err := lookPath(path, filepath.Join(root, "missing")); err == nil {
			test.Errorf("nonexecutable file or directory accepted: %q => %q", path, got)
		}
	}
}

func TestProductionLookPathRelativeCandidatesKeepSpellingAndErrDot(test *testing.T) {
	productionPathRequireUnix(test)
	originalPath := os.Getenv("PATH")
	defer productionPathAssertEnvironmentUnchanged(test, originalPath)
	workingDirectory, err := os.Getwd()
	if err != nil {
		test.Fatal(err)
	}
	root := test.TempDir()
	first := filepath.Join(root, "relative")
	second := filepath.Join(root, "absolute")
	const name = "production-relative-executable"
	productionPathWriteExecutable(test, first, name, 0o755)
	absolute := productionPathWriteExecutable(test, second, name, 0o755)
	relativeDirectory, err := filepath.Rel(workingDirectory, first)
	if err != nil {
		test.Fatal(err)
	}
	want := filepath.Join(relativeDirectory, name)
	pathList := relativeDirectory + string(os.PathListSeparator) + second
	if got, err := lookPath(name, pathList); got != want || !errors.Is(err, exec.ErrDot) {
		test.Fatalf("relative candidate was normalized, bypassed, or lost ErrDot: got=%q want=%q err=%v", got, want, err)
	}
	if got, err := lookPath(want, second); err != nil || got != want {
		test.Fatalf("explicit relative executable spelling changed: %q, %v", got, err)
	}
	if err := os.Chmod(filepath.Join(first, name), 0o600); err != nil {
		test.Fatal(err)
	}
	if got, err := lookPath(name, pathList); err != nil || got != absolute {
		test.Fatalf("ineligible relative candidate prevented eligible absolute fallback: %q, %v", got, err)
	}
}
