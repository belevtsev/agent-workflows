// Package pluginsetup installs the offline Kitsu marketplace into a native host.
package pluginsetup

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"github.com/belevtsev/kitsu/v3/internal/fslock"
	"github.com/belevtsev/kitsu/v3/internal/onboarding"
)

// Install verifies the matching release plugin and uses the selected host's
// native plugin manager to install it from a durable local directory.
func Install(ctx context.Context, host, archiveDir string) (string, error) {
	if host != "codex" && host != "claude" {
		return "", errors.New("plugin setup requires host codex or claude")
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	r, err := loadRelease(archiveDir, host)
	if err != nil {
		return "", err
	}
	executable, native := onboarding.HostExecutable(host)
	if executable == "" {
		return "", fmt.Errorf("%s CLI is missing from absolute PATH entries", host)
	}
	if !native {
		return "", fmt.Errorf("%s resolves to a shell wrapper; install a native host executable", host)
	}
	store, err := marketplaceStore()
	if err != nil {
		return "", err
	}
	lock, err := fslock.Acquire(ctx, filepath.Join(store, "setup.lock"), fslock.Options{Wait: 30 * time.Second})
	if err != nil {
		return "", err
	}
	defer func() { _ = lock.Close() }()
	marketplace, err := r.save(store)
	if err != nil {
		return "", err
	}
	run := nativePluginCommand(host, executable)
	if host == "codex" {
		err = installCodex(ctx, run, marketplace, r)
	} else {
		err = installClaude(ctx, run, marketplace, r)
	}
	if err != nil {
		return "", err
	}
	if err := r.verify(marketplace); err != nil {
		return "", fmt.Errorf("marketplace changed during setup: %w", err)
	}
	return marketplace, nil
}
