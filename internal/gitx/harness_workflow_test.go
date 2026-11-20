package gitx

import (
	"os"
	"path/filepath"
	"testing"
)

func TestApplyWorkflowFilesPreservesRunnerBytes(t *testing.T) {
	for _, tc := range []struct {
		name, attributes string
		useCommitAttrs   bool
	}{
		{name: "autocrlf"},
		{name: "explicit-eol", attributes: "*.rule text eol=crlf\n"},
		{name: "commit-attrs", attributes: "*.rule text eol=crlf\n", useCommitAttrs: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := newRepo(t)
			git(t, dir, "config", "core.autocrlf", "true")
			write(t, dir, ".gitattributes", tc.attributes)
			write(t, dir, "checks/existing.rule", "original\r\n")
			git(t, dir, "add", "--", ".")
			if tc.useCommitAttrs {
				git(t, dir, "commit", "-qm", "original candidate")
				t.Setenv("GIT_ATTR_SOURCE", "HEAD")
			}
			index := git(t, dir, "write-tree")
			repo, err := Open(t.Context(), dir)
			if err != nil {
				t.Fatal(err)
			}
			before := []WorkflowFile{
				{Path: "checks/existing.rule", Mode: "100644", Data: []byte("original\r\n")},
			}
			after := []WorkflowFile{
				{Path: "checks/existing.rule", Mode: "100644", Data: []byte("approved edit\n")},
				{Path: "checks/new.rule", Mode: "100644", Data: []byte("approved addition\n")},
			}
			if err := repo.ApplyWorkflowFiles(t.Context(), before, after); err != nil {
				t.Fatal(err)
			}
			for _, file := range after {
				got, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(file.Path)))
				if err != nil || string(got) != string(file.Data) {
					t.Errorf("%s = %q, %v; want exact runner bytes %q", file.Path, got, err, file.Data)
				}
			}
			if got := git(t, dir, "write-tree"); got != index {
				t.Errorf("staged tree changed from %s to %s", index, got)
			}
			if got := git(t, dir, "config", "--local", "core.autocrlf"); got != "true" {
				t.Errorf("core.autocrlf = %q; want true", got)
			}
		})
	}
}
