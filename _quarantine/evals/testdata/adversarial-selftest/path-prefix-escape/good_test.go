package jail

import (
	"errors"
	"testing"
)

// A1 (reference): R5 and DR1. Fails when a sibling of the root whose name
// starts with the root's base name is accepted as inside the root.
func TestRefSiblingPrefixIsOutside(t *testing.T) {
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
