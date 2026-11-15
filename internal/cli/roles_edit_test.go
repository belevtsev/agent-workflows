package cli

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/belevtsev/kitsu/v3/internal/roles"
)

func TestConcurrentRoleSettingsKeepEverySelection(t *testing.T) {
	t.Parallel()
	repo, root := guardRepo(t, "")
	s := session{repo: repo}
	var wg sync.WaitGroup
	for _, id := range roles.IDs() {
		for _, host := range roles.Hosts {
			wg.Go(func() {
				if _, err := s.editRoles(context.Background(), host, id, roles.Model{Model: id, Effort: "high"}, false); err != nil {
					t.Errorf("%s %s: %v", host, id, err)
				}
			})
		}
	}
	wg.Wait()
	cfg, err := roles.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range roles.IDs() {
		for _, host := range roles.Hosts {
			if cfg.Models[host][id] != (roles.Model{Model: id, Effort: "high"}) {
				t.Errorf("lost %s %s selection: %+v", host, id, cfg.Models[host][id])
			}
		}
	}
}

func TestGlobalRoleSkillWritesPreserveModeAndRejectLinks(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path, lockPath := filepath.Join(dir, "roles.toml"), filepath.Join(dir, "roles.lock")
	data := "# Personal skills\nversion = 1\n[codex.reviewer]\nskills = ['keep']\n"
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for _, role := range roles.IDs() {
		wg.Go(func() {
			if _, err := writeRoleSelection(t.Context(), path, lockPath, "claude", role, roles.Model{}, []string{role}, false, true); err != nil {
				t.Errorf("%s: %v", role, err)
			}
		})
	}
	wg.Wait()
	info, err := os.Stat(path)
	if err != nil || (runtime.GOOS != "windows" && info.Mode().Perm() != 0o600) {
		t.Fatalf("permissions changed: %v, %v", info, err)
	}
	updated, err := os.ReadFile(path)
	if err != nil || !strings.HasPrefix(string(updated), "# Personal skills\n") {
		t.Fatalf("lost personal comments: %s, %v", updated, err)
	}
	cfg, err := roles.ParsePersonal(updated)
	if err != nil {
		t.Fatal(err)
	}
	for _, role := range roles.IDs() {
		if refs := cfg.Skills["claude"][role]; len(refs) != 1 || refs[0] != role {
			t.Fatalf("concurrent edit lost %s: %#v", role, cfg.Skills)
		}
	}
	target := filepath.Join(dir, "external.toml")
	if err := os.WriteFile(target, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, path); err != nil {
		t.Skipf("symbolic links unavailable: %v", err)
	}
	if _, err := writeRoleSelection(t.Context(), path, lockPath, "codex", "reviewer", roles.Model{}, []string{}, false, true); err == nil {
		t.Fatal("personal edit followed a symlink")
	}
	unchanged, err := os.ReadFile(target)
	if err != nil || string(unchanged) != data {
		t.Fatalf("symlink target changed: %s, %v", unchanged, err)
	}
}
