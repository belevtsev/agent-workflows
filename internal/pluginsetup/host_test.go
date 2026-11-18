package pluginsetup

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
)

func TestCodexInstallCanRetryAfterPartialSetup(t *testing.T) {
	t.Parallel()
	r := releaseFixture(t, "1.2.3", "new skill")
	market := savedRelease(t, r)
	install := codexInstallStep(savedRelease(t, r))
	install.err = errors.New("installation failed")
	withFailure := hostScript(t, codexMarketStep(market), install)
	if err := installCodex(t.Context(), withFailure, market, r); !errors.Is(err, install.err) {
		t.Fatalf("partial setup = %v, want native error", err)
	}
	withRetry := hostScript(t, codexMarketStep(market), codexInstallStep(savedRelease(t, r)), codexPluginStep(market, "1.2.3"))
	if err := installCodex(t.Context(), withRetry, market, r); err != nil {
		t.Fatalf("retry after partial setup: %v", err)
	}
}

func TestCodexReplacesPriorLocalMarketplace(t *testing.T) {
	t.Parallel()
	r := releaseFixture(t, "1.2.3", "new skill")
	market := savedRelease(t, r)
	old := releaseFixture(t, "1.2.3", "old skill")
	previous := savedRelease(t, old)

	conflict := codexMarketStep(market)
	conflict.err = errors.New("marketplace 'kitsu' is already added from a different source; remove it before adding this source")
	local := codexMarketListStep("local", previous)
	remove := hostStep{args: []string{"marketplace", "remove", "kitsu"}}
	install := codexInstallStep(savedRelease(t, r))
	t.Run("native registration moves to the matching build", func(t *testing.T) {
		run := hostScript(t, conflict, local, remove, codexMarketStep(market), install, codexPluginStep(market, "1.2.3"))
		if err := installCodex(t.Context(), run, market, r); err != nil {
			t.Fatal(err)
		}
		if err := old.verify(previous); err != nil {
			t.Fatalf("previous durable source: %v", err)
		}
	})
	t.Run("failed re-registration restores old source", func(t *testing.T) {
		blocked := codexMarketStep(market)
		blocked.err = errors.New("new registration failed")
		run := hostScript(t, conflict, local, remove, blocked, codexMarketStep(previous))
		if err := installCodex(t.Context(), run, market, r); !errors.Is(err, blocked.err) {
			t.Fatalf("failed re-registration = %v", err)
		}
	})
	t.Run("foreign marketplace is preserved", func(t *testing.T) {
		run := hostScript(t, conflict, codexMarketListStep("git", "example.org/kitsu"))
		assertSetupError(t, installCodex(t.Context(), run, market, r), "another marketplace")
	})
	t.Run("stale installed bytes fail even after native success", func(t *testing.T) {
		run := hostScript(t, codexMarketStep(market), codexInstallStep(previous))
		assertSetupError(t, installCodex(t.Context(), run, market, r), "installed files do not match")
	})
	t.Run("stale effective source fails even with matching installed bytes", func(t *testing.T) {
		run := hostScript(t, codexMarketStep(market), install, codexPluginStep(previous, "1.2.3"))
		assertSetupError(t, installCodex(t.Context(), run, market, r), "different Kitsu marketplace")
	})
}

func TestClaudeRefreshesSameVersionInstallAndPreservesData(t *testing.T) {
	t.Parallel()
	r := releaseFixture(t, "1.2.3", "new skill")
	market := savedRelease(t, r)
	installed := savedRelease(t, r)
	previous := savedRelease(t, releaseFixture(t, "1.2.3", "old skill"))
	before := claudeMarketStep("directory", previous)
	current := claudeMarketStep("directory", market)
	add := hostStep{args: []string{"marketplace", "add", market}}
	install := hostStep{args: []string{"install", pluginID}}
	fresh := claudePluginStep(installed, market, "1.2.3")
	t.Run("same package reuses native installation", func(t *testing.T) {
		run := hostScript(t, current, add, current, install, fresh)
		if err := installClaude(t.Context(), run, market, r); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("same version refresh preserves data", func(t *testing.T) {
		stale := claudePluginStep(previous, market, "1.2.3")
		remove := hostStep{args: []string{"uninstall", "--keep-data", "--scope", "user", pluginID}}
		run := hostScript(t, before, add, current, install, stale, remove, install, fresh)
		if err := installClaude(t.Context(), run, market, r); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("different version refresh through native update", func(t *testing.T) {
		stale := claudePluginStep(previous, market, "1.2.2")
		update := hostStep{args: []string{"update", "--scope", "user", pluginID}}
		run := hostScript(t, before, add, current, install, stale, update, fresh)
		if err := installClaude(t.Context(), run, market, r); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("foreign marketplace is preserved", func(t *testing.T) {
		run := hostScript(t, claudeMarketStep("github", previous))
		assertSetupError(t, installClaude(t.Context(), run, market, r), "another marketplace")
	})
	t.Run("native add must repoint the marketplace before install", func(t *testing.T) {
		run := hostScript(t, before, add, before)
		assertSetupError(t, installClaude(t.Context(), run, market, r), "did not register the verified")
	})
	t.Run("native list must report the matching effective source", func(t *testing.T) {
		stale := claudePluginStep(installed, previous, "1.2.3")
		run := hostScript(t, before, add, current, install, stale)
		assertSetupError(t, installClaude(t.Context(), run, market, r), "different marketplace directory")
	})
}

func savedRelease(t *testing.T, r release) string {
	t.Helper()
	marketplace, err := r.save(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return marketplace
}

func assertSetupError(t *testing.T, err error, want string) {
	t.Helper()
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("setup failed with %v, want %q", err, want)
	}
}

type hostStep struct {
	args    []string
	receipt any
	err     error
}

func hostScript(t *testing.T, steps ...hostStep) pluginCommand {
	t.Helper()
	var index int
	t.Cleanup(func() {
		if index != len(steps) {
			t.Errorf("skipped native commands: %v", steps[index:])
		}
	})
	return func(_ context.Context, args ...string) ([]byte, error) {
		t.Helper()
		if index == len(steps) || !slices.Equal(args, steps[index].args) {
			t.Fatalf("unexpected native arguments %q, remaining commands %v", args, steps[index:])
		}
		step := steps[index]
		index++
		if step.err != nil || step.receipt == nil {
			return nil, step.err
		}
		return json.Marshal(step.receipt)
	}
}

func codexMarketStep(path string) hostStep {
	return hostStep{
		args:    []string{"marketplace", "add", "--json", path},
		receipt: map[string]string{"marketplaceName": "kitsu", "installedRoot": path},
	}
}

func codexMarketListStep(kind, path string) hostStep {
	return hostStep{args: []string{"marketplace", "list", "--json"}, receipt: map[string]any{
		"marketplaces": []any{map[string]any{
			"name": "kitsu", "marketplaceSource": map[string]string{"sourceType": kind, "source": path},
		}},
	}}
}

func codexInstallStep(path string) hostStep {
	return hostStep{
		args:    []string{"add", "--json", pluginID},
		receipt: map[string]string{"pluginId": pluginID, "installedPath": path},
	}
}

func codexPluginStep(source, version string) hostStep {
	return hostStep{args: []string{"list", "--json", "--marketplace", "kitsu"}, receipt: map[string]any{
		"installed": []any{map[string]any{
			"pluginId": pluginID, "version": version, "installed": true, "enabled": true,
			"source": map[string]string{"source": "local", "path": source},
		}},
	}}
}

func claudeMarketStep(kind, path string) hostStep {
	return hostStep{args: []string{"marketplace", "list", "--json"}, receipt: []any{
		map[string]string{"name": "kitsu", "source": kind, "path": path, "installLocation": path},
	}}
}

func claudePluginStep(installed, effective, version string) hostStep {
	return hostStep{args: []string{"list", "--json"}, receipt: []any{map[string]any{
		"id": pluginID, "scope": "user", "version": version, "enabled": true,
		"installPath": installed, "readFromFolder": effective,
	}}}
}
