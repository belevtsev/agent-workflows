package jail_test

import (
	"errors"
	"sync"
	"testing"

	"example.com/jail"
)

const rootDir = "/srv/jail/data"

func newRoot(t *testing.T) *jail.Root {
	t.Helper()
	r, err := jail.New(rootDir)
	if err != nil {
		t.Fatalf("New(%q): %v", rootDir, err)
	}
	return r
}

// R1: fails if an invalid root is accepted or a valid one is rejected.
func TestNewValidatesRoot(t *testing.T) {
	for _, dir := range []string{"relative/dir", "", ".", "/", "//", "/a/.."} {
		if r, err := jail.New(dir); err == nil || r != nil {
			t.Errorf("New(%q) = %v, %v; want nil, non-nil error", dir, r, err)
		}
	}
	r, err := jail.New("/srv/jail/./data/")
	if err != nil || r == nil {
		t.Fatalf("New(unclean absolute) = %v, %v; want a Root", r, err)
	}
	if got, err := r.Resolve("."); err != nil || got != rootDir {
		t.Fatalf("Resolve(.) on unclean root = %q, %v; want %q (the cleaned root)", got, err, rootDir)
	}
}

// R2: fails if a name inside the root does not resolve to the joined path.
func TestResolveInside(t *testing.T) {
	r := newRoot(t)
	cases := map[string]string{
		"a":              rootDir + "/a",
		"a/b/c.txt":      rootDir + "/a/b/c.txt",
		"a/../b":         rootDir + "/b",
		"./a//b/":        rootDir + "/a/b",
		"a/b/../../c":    rootDir + "/c",
		"x/../../data/y": rootDir + "/y",
	}
	for name, want := range cases {
		got, err := r.Resolve(name)
		if err != nil || got != want {
			t.Errorf("Resolve(%q) = %q, %v; want %q, nil", name, got, err, want)
		}
	}
}

// R3: fails if "." or "" does not resolve to the root.
func TestResolveRootItself(t *testing.T) {
	r := newRoot(t)
	for _, name := range []string{".", "", "a/..", "./."} {
		got, err := r.Resolve(name)
		if err != nil || got != rootDir {
			t.Errorf("Resolve(%q) = %q, %v; want %q, nil", name, got, err, rootDir)
		}
	}
}

// R4: fails if an absolute name is accepted.
func TestResolveRejectsAbsolute(t *testing.T) {
	r := newRoot(t)
	for _, name := range []string{"/etc/passwd", rootDir + "/a", rootDir, "/"} {
		got, err := r.Resolve(name)
		if !errors.Is(err, jail.ErrOutside) || got != "" {
			t.Errorf("Resolve(%q) = %q, %v; want \"\", ErrOutside", name, got, err)
		}
	}
}

// R5: fails if a .. escape is accepted.
func TestResolveRejectsDotDotEscape(t *testing.T) {
	r := newRoot(t)
	for _, name := range []string{
		"..",
		"../..",
		"../../../etc/passwd",
		"../other/file",
		"a/../../secret",
		"a/b/../../../../../tmp",
	} {
		got, err := r.Resolve(name)
		if !errors.Is(err, jail.ErrOutside) || got != "" {
			t.Errorf("Resolve(%q) = %q, %v; want \"\", ErrOutside", name, got, err)
		}
	}
}

// R6: fails on a data race or a wrong result under concurrent use.
func TestResolveConcurrent(t *testing.T) {
	r := newRoot(t)
	var wg sync.WaitGroup
	for g := 0; g < 16; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				if got, err := r.Resolve("a/b"); err != nil || got != rootDir+"/a/b" {
					t.Errorf("Resolve(a/b) = %q, %v", got, err)
					return
				}
				if _, err := r.Resolve("../x"); !errors.Is(err, jail.ErrOutside) {
					t.Errorf("Resolve(../x) err = %v, want ErrOutside", err)
					return
				}
			}
		}()
	}
	wg.Wait()
}
