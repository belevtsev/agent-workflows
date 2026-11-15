package cli

import (
	"context"
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
