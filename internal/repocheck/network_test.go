package repocheck

import (
	"os/exec"
	"strings"
	"testing"
)

// cw promises to open no network connection. The promise is kept by not
// linking a package that could: this test fails as soon as cw, or anything
// it imports, imports one.
func TestCwLinksNoNetworkPackage(t *testing.T) {
	list := exec.Command("go", "list", "-deps", "./cmd/cw")
	list.Dir = repoRoot
	out, err := list.Output()
	if err != nil {
		t.Fatalf("go list -deps ./cmd/cw: %v", err)
	}
	for _, pkg := range strings.Fields(string(out)) {
		if pkg == "net" || strings.HasPrefix(pkg, "net/") || pkg == "crypto/tls" {
			t.Errorf("cw links the network package %s", pkg)
		}
	}
}
