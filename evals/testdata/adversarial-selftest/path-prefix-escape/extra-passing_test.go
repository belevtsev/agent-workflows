package jail

import (
	"errors"
	"testing"
)

// Self-test fixture (AG4): one probe that fails on the seeded source and one
// test that passes on it. TestResolve is a prefix of the probe's name, so the
// gate only reports it as passing when it runs each kept test alone
// (-run '^Name$').

// R5 and DR1: the reference probe, renamed. Fails when a sibling of the root
// whose name starts with the root's base name is accepted as inside the root.
func TestResolveSiblingPrefixIsOutside(t *testing.T) {
	r, err := New("/srv/jail/data")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"../data-evil/x", "../data2", "../database/secret"} {
		got, err := r.Resolve(name)
		if !errors.Is(err, ErrOutside) || got != "" {
			t.Errorf("Resolve(%q) = %q, %v; want \"\", ErrOutside", name, got, err)
		}
	}
}

// R2 for a plain name below the root: behaviour the seeded source already
// has, so the gate must name this test. Fails when it is not joined to the
// root.
func TestResolve(t *testing.T) {
	r, err := New("/srv/jail/data")
	if err != nil {
		t.Fatal(err)
	}
	got, err := r.Resolve("a/b")
	if err != nil || got != "/srv/jail/data/a/b" {
		t.Fatalf("Resolve(%q) = %q, %v; want %q, nil", "a/b", got, err, "/srv/jail/data/a/b")
	}
}
