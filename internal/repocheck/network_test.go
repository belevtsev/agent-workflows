package repocheck

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func networkPackage(pkg string) bool {
	return pkg == "net" || strings.HasPrefix(pkg, "net/") || pkg == "crypto/tls"
}

type linkedPackage struct {
	ImportPath string
	Standard   bool
	Imports    []string
}

func linkedPackages(t *testing.T, goos string, targets ...string) []linkedPackage {
	t.Helper()
	args := append([]string{"list", "-deps", "-json"}, targets...)
	list := exec.Command("go", args...)
	list.Dir = repoRoot
	list.Env = append(os.Environ(), "GOOS="+goos)
	out, err := list.Output()
	if err != nil {
		t.Fatalf("go list for %s: %v", goos, err)
	}
	decoder := json.NewDecoder(bytes.NewReader(out))
	var packages []linkedPackage
	for {
		var pkg linkedPackage
		err := decoder.Decode(&pkg)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		packages = append(packages, pkg)
	}
	if len(packages) < 10 {
		t.Fatalf("unexpected dependency inventory: %d packages", len(packages))
	}
	return packages
}

// Native advisers are the only first-party package allowed to import network
// libraries. Standard HTTP/TLS dependencies and Windows address types are expected.
func TestNetworkImportsStayInNativeAdvisers(t *testing.T) {
	for _, goos := range []string{"linux", "darwin", "windows"} {
		for _, pkg := range linkedPackages(t, goos, "./cmd/kitsu") {
			if pkg.Standard || pkg.ImportPath == "github.com/belevtsev/kitsu/v3/internal/advisers" || pkg.ImportPath == "golang.org/x/sys/windows" {
				continue
			}
			for _, imported := range pkg.Imports {
				if networkPackage(imported) {
					t.Errorf("%s: %s imports network package %s outside native advisers", goos, pkg.ImportPath, imported)
				}
			}
		}
	}
}

// Gate decisions and host token collection retain their
// offline dependency graph. Windows x/sys uses net only for address types.
func TestGateAndTokensRemainOffline(t *testing.T) {
	for _, goos := range []string{"linux", "darwin", "windows"} {
		for _, pkg := range linkedPackages(t, goos, "./internal/gate", "./internal/tokens") {
			if networkPackage(pkg.ImportPath) && goos != "windows" {
				t.Errorf("%s: offline code links %s", goos, pkg.ImportPath)
			}
			for _, imported := range pkg.Imports {
				if networkPackage(imported) && !networkPackage(pkg.ImportPath) && pkg.ImportPath != "golang.org/x/sys/windows" {
					t.Errorf("%s: %s imports %s in offline code", goos, pkg.ImportPath, imported)
				}
			}
		}
	}
}
