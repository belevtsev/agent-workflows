package repocheck

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// networkPackage says whether a package of the standard library could open a
// network connection.
func networkPackage(pkg string) bool {
	return pkg == "net" || strings.HasPrefix(pkg, "net/") || pkg == "crypto/tls"
}

// kitsu promises to open no network connection. The promise is kept by what kitsu
// links, for each system it is built for, whatever system the test runs on.
//
// On Linux and macOS kitsu links no package that could open one. On Windows the
// package net is linked by golang.org/x/sys/windows, which kitsu calls for job
// objects and file locks and which uses the address types of net. There the
// test holds that this package is the only one that imports a network
// package: no package of kitsu does, and no other dependency.
func TestCwLinksNoNetworkPackage(t *testing.T) {
	for _, goos := range []string{"linux", "darwin", "windows"} {
		list := exec.Command("go", "list", "-deps", "-f", `{{.ImportPath}}{{range .Imports}} {{.}}{{end}}`, "./cmd/kitsu")
		list.Dir = repoRoot
		list.Env = append(os.Environ(), "GOOS="+goos)
		out, err := list.Output()
		if err != nil {
			t.Fatalf("go list -deps ./cmd/kitsu for %s: %v", goos, err)
		}
		lines := strings.Split(strings.TrimSpace(string(out)), "\n")
		if len(lines) < 10 {
			t.Fatalf("go list -deps ./cmd/kitsu for %s named only %d packages", goos, len(lines))
		}
		for _, line := range lines {
			fields := strings.Fields(line)
			pkg, imports := fields[0], fields[1:]
			if networkPackage(pkg) && goos != "windows" {
				t.Errorf("%s: kitsu links the network package %s", goos, pkg)
			}
			for _, imported := range imports {
				if networkPackage(imported) && !networkPackage(pkg) && pkg != "golang.org/x/sys/windows" {
					t.Errorf("%s: %s imports the network package %s", goos, pkg, imported)
				}
			}
		}
	}
}
