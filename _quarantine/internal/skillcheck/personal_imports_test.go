package skillcheck

import (
	"crypto/sha256"
	"encoding/hex"
	json "encoding/json/v2"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

type personalImportFile struct {
	Path        string `json:"path"`
	Mode        int    `json:"mode"`
	SourceSHA   string `json:"source_sha256"`
	ImportedSHA string `json:"imported_sha256"`
	SourceBlob  string `json:"source_git_blob"`
	Adaptation  string `json:"adaptation"`
}

func TestPersonalImportHashesAndCompleteOriginalInventories(t *testing.T) {
	source := filepath.Clean(filepath.Join("..", ".."))
	var provenance struct {
		Version int    `json:"version"`
		Format  string `json:"inventory_digest_format"`
		Imports map[string]struct {
			Classification string `json:"classification"`
			Destination    string `json:"destination"`
			Source         struct {
				Kind            string `json:"kind"`
				Repository      string `json:"repository"`
				DeclaredVersion string `json:"declared_version"`
				Tag             string `json:"tag"`
				Commit          string `json:"commit"`
				CommitVerified  bool   `json:"commit_verified"`
				FileCount       int    `json:"original_file_count"`
				EntryCount      int    `json:"original_entry_count"`
				InventorySHA    string `json:"inventory_sha256"`
				Limits          string `json:"verification_limits"`
			} `json:"source"`
			Files    []personalImportFile `json:"files"`
			Excluded []string             `json:"excluded_source_files"`
		} `json:"imports"`
		Notices []struct {
			Path       string `json:"path"`
			Source     string `json:"source"`
			Blob       string `json:"git_blob"`
			SHA        string `json:"sha256"`
			Unmodified bool   `json:"unmodified"`
		} `json:"additional_notices"`
	}
	read := func(path string, value any) {
		t.Helper()
		data, err := os.ReadFile(filepath.Join(source, path))
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(data, value, json.RejectUnknownMembers(true)); err != nil {
			t.Fatal(err)
		}
	}
	read("third_party/personal-skill-imports.json", &provenance)
	if provenance.Version != 1 || len(provenance.Imports) != 13 {
		t.Fatalf("incomplete personal provenance: version=%d imports=%d", provenance.Version, len(provenance.Imports))
	}
	digest := func(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }
	for name, imported := range provenance.Imports {
		t.Run(name, func(t *testing.T) {
			if !filepath.IsLocal(imported.Destination) || strings.Contains(imported.Destination, "slack-pr-review") {
				t.Fatal("unsafe import root")
			}
			files := map[string]personalImportFile{}
			for _, file := range imported.Files {
				if !filepath.IsLocal(file.Path) || filepath.ToSlash(filepath.Clean(file.Path)) != file.Path {
					t.Fatalf("unsafe imported path %s", file.Path)
				}
				if _, exists := files[file.Path]; exists {
					t.Fatal("duplicate imported file")
				}
				files[file.Path] = file
				if file.Mode != 0o644 && file.Mode != 0o755 {
					t.Fatalf("recorded mode %#o is not a mode git stores: %s", file.Mode, file.Path)
				}
				if file.SourceSHA != file.ImportedSHA && file.Adaptation == "" {
					t.Fatalf("unexplained adaptation: %s", file.Path)
				}
			}
			if !slices.IsSorted(imported.Excluded) {
				t.Fatal("excluded inventory is not sorted")
			}
			if len(slices.Compact(slices.Clone(imported.Excluded))) != len(imported.Excluded) {
				t.Fatal("duplicated exclusion")
			}
			root := filepath.Join(source, imported.Destination)
			err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
				if err != nil {
					return err
				}
				if entry.Type()&os.ModeSymlink != 0 {
					t.Fatalf("imported link: %s", path)
				}
				if entry.IsDir() {
					if entry.Name() == "node_modules" || entry.Name() == ".git" || entry.Name() == ".cache" {
						t.Fatalf("excluded runtime state: %s", path)
					}
					return nil
				}
				relative, err := filepath.Rel(root, path)
				if err != nil {
					return err
				}
				file, exists := files[filepath.ToSlash(relative)]
				if !exists {
					t.Fatalf("unrecorded import: %s", path)
				}
				data, err := os.ReadFile(path)
				if err != nil {
					return err
				}
				info, err := entry.Info()
				if err != nil {
					return err
				}
				// Oracle: git stores a regular file as 0644 or 0755 and checks
				// it out under the user's umask, so the bytes and the owner's
				// executable bit (the bit git reads) are all a checkout keeps
				// of the recorded mode; 0664 under umask 002 is still 0644.
				if !info.Mode().IsRegular() || info.Mode().Perm()&0o100 != fs.FileMode(file.Mode)&0o100 || digest(data) != file.ImportedSHA {
					t.Fatalf("import changed without provenance: %s", path)
				}
				delete(files, filepath.ToSlash(relative))
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			if len(files) != 0 {
				t.Fatal("provenance references missing files")
			}
		})
	}
	for _, notice := range provenance.Notices {
		if !filepath.IsLocal(notice.Path) || !notice.Unmodified {
			t.Fatal("invalid additional notice")
		}
		data, err := os.ReadFile(filepath.Join(source, notice.Path))
		if err != nil || digest(data) != notice.SHA {
			t.Fatalf("notice changed: %s %v", notice.Path, err)
		}
	}
}
