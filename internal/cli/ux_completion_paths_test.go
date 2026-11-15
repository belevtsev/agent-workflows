package cli

import (
	"path/filepath"
	"slices"
	"testing"
)

func TestUXCompletionHomePaths(t *testing.T) {
	repo, _ := skillRepo(t)
	home := repo.In(repo.Home)
	home.Write("root file.json", "{}")
	home.Write("nested/child file.json", "{}")
	home.Write("nested/child directory/file", "content")
	home.Write("sibling/file", "content")
	separator := string(filepath.Separator)
	nested := "~/nested" + separator
	sibling := "~/sibling" + separator
	childDirectory := "~/nested/child directory" + separator
	tests := []struct {
		name        string
		words, want []string
	}{
		{
			name:  "home files and directories",
			words: []string{"adviser", "ask", "--request", "~/"},
			want:  []string{nested, "~/root file.json", sibling},
		},
		{
			name:  "home directories only",
			words: []string{"skill", "list", "--root", "~/"},
			want:  []string{nested, sibling},
		},
		{
			name:  "nested files and directories",
			words: []string{"adviser", "ask", "--request", "~/nested/"},
			want:  []string{childDirectory, "~/nested/child file.json"},
		},
		{
			name:  "nested directories only",
			words: []string{"skill", "list", "--root", "~/nested/"},
			want:  []string{childDirectory},
		},
		{
			name:  "home prefix",
			words: []string{"adviser", "ask", "--request", "~/roo"},
			want:  []string{"~/root file.json"},
		},
		{
			name:  "nested file prefix",
			words: []string{"adviser", "ask", "--request", "~/nested/child f"},
			want:  []string{"~/nested/child file.json"},
		},
		{
			name:  "nested directory prefix",
			words: []string{"skill", "list", "--root", "~/nested/child d"},
			want:  []string{childDirectory},
		},
		{
			name:  "native directory continuation",
			words: []string{"adviser", "ask", "--request", nested},
			want:  []string{nested + "child directory" + separator, nested + "child file.json"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			values := uxCompletionTestValues(uxQueryRepoCompletion(t, repo, test.words))
			if !slices.Equal(values, test.want) {
				t.Errorf("completion %q = %q, want %q", test.words, values, test.want)
			}
		})
	}
}
