package pluginsetup

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/belevtsev/kitsu/v3/internal/decl"
	"github.com/belevtsev/kitsu/v3/internal/proc"
)

type pluginCommand func(context.Context, ...string) ([]byte, error)

func nativePluginCommand(host, executable string) pluginCommand {
	return func(ctx context.Context, args ...string) ([]byte, error) {
		argv := []string{"plugin"}
		if host == "codex" {
			argv = []string{"-c", "analytics.enabled=false", "-c", "check_for_update_on_startup=false", "plugin"}
		}
		env := os.Environ()
		if host == "claude" {
			env = append(env, "CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1", "DISABLE_AUTOUPDATER=1", "OTEL_SDK_DISABLED=true")
		}
		result := proc.Run(ctx, proc.Spec{
			Path: executable, Args: append(argv, args...), Env: env,
			StdoutLimit: manifestLimit, StderrLimit: 8 << 10,
		})
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if result.Overflow {
			return nil, fmt.Errorf("%s plugin %s output exceeded 1 MiB", host, args[0])
		}
		err := errors.Join(result.Err, result.Cleanup)
		if err == nil {
			return result.Stdout, nil
		}
		detail := strings.TrimSpace(string(result.Stderr))
		if detail == "" {
			detail = strings.TrimSpace(string(result.Stdout))
		}
		if detail != "" {
			return nil, fmt.Errorf("%s plugin %s: %w: %s", host, args[0], err, decl.Plain(detail))
		}
		return nil, fmt.Errorf("%s plugin %s: %w", host, args[0], err)
	}
}

func installCodex(ctx context.Context, run pluginCommand, marketplace string, r release) error {
	if err := registerCodex(ctx, run, marketplace); err != nil {
		return err
	}
	data, err := run(ctx, "add", "--json", pluginID)
	if err != nil {
		return err
	}
	var receipt struct{ PluginID, InstalledPath string }
	if json.Unmarshal(data, &receipt) != nil || receipt.PluginID != pluginID || !filepath.IsAbs(receipt.InstalledPath) {
		return errors.New("Codex did not identify the installed Kitsu directory")
	}
	if err := r.verify(receipt.InstalledPath); err != nil {
		return fmt.Errorf("Codex installed files do not match %s: %w", archiveName, err)
	}
	return verifyCodexPlugin(ctx, run, marketplace, r)
}

func verifyCodexPlugin(ctx context.Context, run pluginCommand, marketplace string, r release) error {
	data, err := run(ctx, "list", "--json", "--marketplace", "kitsu")
	if err != nil {
		return err
	}
	var inventory struct {
		Installed []struct {
			PluginID, Version  string
			Installed, Enabled bool
			Source             struct{ Source, Path string }
		}
	}
	if json.Unmarshal(data, &inventory) != nil {
		return errors.New("Codex did not return a readable plugin inventory")
	}
	count := 0
	expected := pluginVersion(r.members[pluginManifest("codex")].data)
	for _, item := range inventory.Installed {
		if item.PluginID != pluginID {
			continue
		}
		if !item.Installed || !item.Enabled || item.Version != expected || item.Source.Source != "local" || !sameDirectory(item.Source.Path, marketplace) {
			return errors.New("Codex reported a different Kitsu marketplace, version or enabled state")
		}
		count++
	}
	if count != 1 {
		return errors.New("Codex did not identify one enabled installation of kitsu@kitsu")
	}
	return nil
}

func registerCodex(ctx context.Context, run pluginCommand, marketplace string) error {
	err := addCodexMarketplace(ctx, run, marketplace)
	if err == nil || !strings.Contains(err.Error(), "already added from a different source") {
		return err
	}
	data, err := run(ctx, "marketplace", "list", "--json")
	if err != nil {
		return err
	}
	var configured struct {
		Marketplaces []struct {
			Name              string
			MarketplaceSource struct{ SourceType, Source string }
		}
	}
	if json.Unmarshal(data, &configured) != nil {
		return errors.New("Codex did not return readable marketplace registrations")
	}
	previous := ""
	for _, entry := range configured.Marketplaces {
		if entry.Name != "kitsu" {
			continue
		}
		source := entry.MarketplaceSource
		if previous != "" || source.SourceType != "local" || !localKitsu("codex", source.Source) {
			return foreignMarketplace("Codex")
		}
		previous = source.Source
	}
	if previous == "" || sameDirectory(previous, marketplace) {
		return errors.New("Codex reported a conflicting Kitsu marketplace without a distinct local Kitsu source")
	}
	if _, err := run(ctx, "marketplace", "remove", "kitsu"); err != nil {
		return err
	}
	if err := addCodexMarketplace(ctx, run, marketplace); err != nil {
		if ctx.Err() == nil {
			if restoreErr := addCodexMarketplace(ctx, run, previous); restoreErr != nil {
				return errors.Join(err, fmt.Errorf("restore previous Codex marketplace: %w", restoreErr))
			}
		}
		return err
	}
	return nil
}

func addCodexMarketplace(ctx context.Context, run pluginCommand, directory string) error {
	data, err := run(ctx, "marketplace", "add", "--json", directory)
	if err != nil {
		return err
	}
	var receipt struct{ MarketplaceName, InstalledRoot string }
	if json.Unmarshal(data, &receipt) != nil || receipt.MarketplaceName != "kitsu" || !sameDirectory(receipt.InstalledRoot, directory) {
		return errors.New("Codex did not register the verified Kitsu marketplace directory")
	}
	return nil
}

func installClaude(ctx context.Context, run pluginCommand, marketplace string, r release) error {
	if _, err := findClaudeMarketplace(ctx, run, marketplace); err != nil {
		return err
	}
	if _, err := run(ctx, "marketplace", "add", marketplace); err != nil {
		return err
	}
	current, err := findClaudeMarketplace(ctx, run, marketplace)
	if err != nil {
		return err
	}
	if !sameDirectory(current, marketplace) {
		return errors.New("Claude did not register the verified Kitsu marketplace directory")
	}
	if _, err := run(ctx, "install", pluginID); err != nil {
		return err
	}
	return ensureClaudeFiles(ctx, run, marketplace, r)
}

func findClaudeMarketplace(ctx context.Context, run pluginCommand, marketplace string) (string, error) {
	data, err := run(ctx, "marketplace", "list", "--json")
	if err != nil {
		return "", err
	}
	var configured []struct{ Name, Source, Path, InstallLocation string }
	if json.Unmarshal(data, &configured) != nil || configured == nil {
		return "", errors.New("Claude did not return readable marketplace registrations")
	}
	current := ""
	for _, entry := range configured {
		if entry.Name != "kitsu" {
			continue
		}
		knownLocal := sameDirectory(entry.Path, marketplace) || localKitsu("claude", entry.Path)
		if current != "" || entry.Source != "directory" || !knownLocal || !sameDirectory(entry.Path, entry.InstallLocation) {
			return "", foreignMarketplace("Claude")
		}
		current = entry.Path
	}
	return current, nil
}

type claudeInstallation struct {
	ID, Scope, Version, InstallPath, ReadFromFolder string
	Enabled                                         bool
}

func findClaudePlugin(ctx context.Context, run pluginCommand, marketplace string) (claudeInstallation, error) {
	data, err := run(ctx, "list", "--json")
	if err != nil {
		return claudeInstallation{}, err
	}
	var installs []claudeInstallation
	if json.Unmarshal(data, &installs) != nil {
		return claudeInstallation{}, errors.New("Claude did not return a readable plugin inventory")
	}
	var selected claudeInstallation
	for _, install := range installs {
		if install.ID != pluginID || install.Scope != "user" {
			continue
		}
		if selected.ID != "" || !install.Enabled || !filepath.IsAbs(install.InstallPath) {
			return claudeInstallation{}, errors.New("Claude did not identify one enabled user installation of kitsu@kitsu")
		}
		if install.ReadFromFolder != "" && !sameDirectory(install.ReadFromFolder, marketplace) {
			return claudeInstallation{}, errors.New("Claude is loading Kitsu from a different marketplace directory")
		}
		selected = install
	}
	if selected.ID == "" {
		return selected, errors.New("Claude did not identify an enabled user installation of kitsu@kitsu")
	}
	return selected, nil
}

func ensureClaudeFiles(ctx context.Context, run pluginCommand, marketplace string, r release) error {
	installed, err := findClaudePlugin(ctx, run, marketplace)
	if err != nil {
		return err
	}
	if r.verify(installed.InstallPath) == nil {
		return nil
	}
	// Claude's local runtime may load from the new source while its inventory
	// still points to old bytes. Refresh the installed copy for offline readers.
	if expected := pluginVersion(r.members[pluginManifest("claude")].data); installed.Version != expected {
		if _, err := run(ctx, "update", "--scope", "user", pluginID); err != nil {
			return err
		}
	} else {
		// Native update short-circuits when both builds declare the same version.
		if _, err := run(ctx, "uninstall", "--keep-data", "--scope", "user", pluginID); err != nil {
			return err
		}
		if _, err := run(ctx, "install", pluginID); err != nil {
			return err
		}
	}
	installed, err = findClaudePlugin(ctx, run, marketplace)
	if err != nil {
		return err
	}
	if err := r.verify(installed.InstallPath); err != nil {
		return fmt.Errorf("Claude installed files do not match %s: %w", archiveName, err)
	}
	return nil
}

func foreignMarketplace(host string) error {
	return fmt.Errorf("%s already uses the name kitsu for another marketplace; review that registration before replacing it", host)
}

func localKitsu(host, directory string) bool {
	if !filepath.IsAbs(directory) {
		return false
	}
	market, err := readRegular(filepath.Join(directory, filepath.FromSlash(marketManifest(host))), manifestLimit)
	if err != nil || !isKitsuMarketplace(host, market) {
		return false
	}
	manifest, err := readRegular(filepath.Join(directory, filepath.FromSlash(pluginManifest(host))), manifestLimit)
	return err == nil && pluginVersion(manifest) != ""
}

func sameDirectory(a, b string) bool {
	if !filepath.IsAbs(a) || !filepath.IsAbs(b) {
		return false
	}
	first, err := os.Stat(a)
	if err != nil || !first.IsDir() {
		return false
	}
	second, err := os.Stat(b)
	return err == nil && os.SameFile(first, second)
}
