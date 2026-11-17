package gitx

import "testing"

func TestEmptyTree(t *testing.T) {
	dir := newRepo(t)
	repo := open(t, dir)
	tree, err := repo.EmptyTree(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	resolved, found, err := repo.TreeOf(t.Context(), tree)
	if err != nil || !found || resolved != tree {
		t.Fatalf("empty tree not pinned: %q, %t, %v", resolved, found, err)
	}
	if err := repo.Entries(t.Context(), tree, func(_ []byte, path string) { t.Errorf("empty tree contains %s", path) }); err != nil {
		t.Fatal(err)
	}
}
