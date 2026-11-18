package repocheck

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"slices"
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

// List direct imports so an HTTP client added elsewhere requires an explicit decision.
func TestCLINetworkImportBoundaries(t *testing.T) {
	allowed := map[string][]string{
		module + "internal/advisers":      {"net/http", "net/url"}, // Provider API clients.
		module + "internal/tickets":       {"net/http", "net/url"}, // Public ticket links.
		"github.com/charmbracelet/x/ansi": {"net/url"},             // Terminal hyperlink formatting.
		"golang.org/x/sys/windows":        {"net"},                 // Windows address types.
	}
	for _, goos := range []string{"linux", "darwin", "windows"} {
		for _, pkg := range linkedPackages(t, goos, "./cmd/kitsu") {
			if pkg.Standard {
				continue
			}
			for _, imported := range pkg.Imports {
				if networkPackage(imported) && !slices.Contains(allowed[pkg.ImportPath], imported) {
					t.Errorf("%s: %s imports undeclared network dependency %s", goos, pkg.ImportPath, imported)
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
