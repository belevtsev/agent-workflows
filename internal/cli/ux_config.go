package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/belevtsev/kitsu/v3/internal/gitx"
)

const uxCheckoutConfigFile = "kitsu-cli.toml"

func uxValidHost(host string) bool { return host == "codex" || host == "claude" }

func uxPersonalConfigPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "kitsu", "config.toml"), nil
}

func uxInstalledHost() (string, error) {
	path, err := uxPersonalConfigPath()
	if err != nil {
		return "", err
	}
	host, _, err := uxReadPreference(path)
	return host, err
}

func uxConfigPaths(ctx context.Context) (checkout, personal string, err error) {
	personal, err = uxPersonalConfigPath()
	if err != nil {
		return "", "", err
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", "", err
	}
	repo, err := gitx.Open(ctx, cwd)
	if errors.Is(err, gitx.ErrNotRepo) {
		return "", personal, nil
	}
	if err != nil {
		return "", "", err
	}
	if repo.Root != "" {
		checkout = filepath.Join(repo.GitDir, uxCheckoutConfigFile)
	}
	return checkout, personal, nil
}

func uxHostPreference(ctx context.Context) (host, source string, err error) {
	if value := os.Getenv("KITSU_HOST"); value != "" {
		if !uxValidHost(value) {
			return "", "KITSU_HOST", usagef("KITSU_HOST must be codex or claude")
		}
		return value, "KITSU_HOST", nil
	}
	checkout, personal, err := uxConfigPaths(ctx)
	if err != nil {
		return "", "", err
	}
	for _, path := range []string{checkout, personal} {
		if path == "" {
			continue
		}
		value, _, err := uxReadPreference(path)
		if err != nil {
			return "", path, err
		}
		if value != "" {
			return value, path, nil
		}
	}
	return "", "unset", nil
}

func uxHostArgs(ctx context.Context, args []string, required bool) ([]string, error) {
	insert := len(args)
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			insert = i
			break
		}
		if arg == "--host" || arg == "-host" || strings.HasPrefix(arg, "--host=") || strings.HasPrefix(arg, "-host=") {
			return args, nil
		}
		if uxHostArgHasValue(arg) {
			i++
		}
	}
	host, _, err := uxHostPreference(ctx)
	if err != nil {
		return nil, err
	}
	if host == "" {
		if required {
			return nil, usagef("select --host codex|claude or save a preference with `kitsu config set host <host>`")
		}
		return args, nil
	}
	out := append([]string{}, args[:insert]...)
	out = append(out, "--host", host)
	return append(out, args[insert:]...), nil
}

func uxHostArgHasValue(arg string) bool {
	if !strings.HasPrefix(arg, "-") || strings.Contains(arg, "=") {
		return false
	}
	name := strings.TrimLeft(arg, "-")
	for _, command := range uxCommands() {
		for _, option := range command.flags {
			if option.name == name && option.value != "" {
				return true
			}
		}
	}
	return false
}

func runUXConfig(ctx context.Context, cfg Config, args []string) error {
	if len(args) == 0 {
		return usagef("config requires show, get, set, reset, or edit")
	}
	if args[0] == "edit" {
		return runUXConfigEdit(ctx, cfg, args[1:])
	}
	flags := newFlags("config " + args[0])
	asJSON := flags.Bool("json", false, "")
	global := flags.Bool("global", false, "")
	ifUnset := flags.Bool("if-unset", false, "")
	p, err := parseArgs(flags, args[1:])
	if err != nil {
		return err
	}
	if err := uxConfigOperands(args[0], p, *global); err != nil {
		return err
	}
	if *ifUnset && (!*global || args[0] != "set") {
		return usagef("--if-unset is available for config set --global")
	}
	if args[0] == "show" || args[0] == "get" {
		return uxShowConfig(ctx, cfg, args[0] == "get", *asJSON)
	}
	path, err := uxConfigTarget(ctx, *global)
	if err != nil {
		return err
	}
	saved, before, err := uxReadPreference(path)
	if err != nil {
		return failf("%v", err)
	}
	if *ifUnset && saved != "" {
		return uxSavedPreference(cfg, path, saved, *asJSON)
	}
	host := ""
	if args[0] == "set" {
		host = p.pos[1]
	}
	if err := uxSavePreference(ctx, before, host); err != nil {
		return failf("%v", err)
	}
	return uxSavedPreference(cfg, path, host, *asJSON)
}

func uxSavedPreference(cfg Config, path, host string, asJSON bool) error {
	if asJSON {
		return printJSON(cfg, map[string]string{"host": host, "source": path})
	}
	if host == "" {
		host = "inherit / unset"
	}
	fmt.Fprintf(cfg.Out, "Saved host preference: %s\nSource: %s\n", host, path)
	return nil
}

func uxConfigOperands(action string, p parsed, global bool) error {
	valid := !p.dash
	switch action {
	case "show":
		valid = valid && len(p.pos) == 0 && !global
	case "get":
		valid = valid && len(p.pos) == 1 && p.pos[0] == "host" && !global
	case "set":
		valid = valid && len(p.pos) == 2 && p.pos[0] == "host" && uxValidHost(p.pos[1])
	case "reset":
		valid = valid && len(p.pos) == 1 && p.pos[0] == "host"
	default:
		valid = false
	}
	if !valid {
		return usagef("invalid config %s arguments; see `kitsu config %s --help`", action, action)
	}
	return nil
}

func uxConfigTarget(ctx context.Context, global bool) (string, error) {
	if global {
		return uxPersonalConfigPath()
	}
	checkout, _, err := uxConfigPaths(ctx)
	if err != nil {
		return "", failf("%v", err)
	}
	if checkout == "" {
		return "", usagef("checkout configuration requires a git working tree; use --global for personal settings")
	}
	return checkout, nil
}

func uxShowConfig(ctx context.Context, cfg Config, get, asJSON bool) error {
	host, source, err := uxHostPreference(ctx)
	if err != nil {
		return err
	}
	if get {
		if asJSON {
			return printJSON(cfg, map[string]string{"key": "host", "value": host, "source": source})
		}
		fmt.Fprintln(cfg.Out, host)
		return nil
	}
	checkout, personal, err := uxConfigPaths(ctx)
	if err != nil {
		return failf("%v", err)
	}
	values := map[string]string{}
	for _, path := range []string{checkout, personal} {
		if path != "" {
			value, _, err := uxReadPreference(path)
			if err != nil {
				return failf("%v", err)
			}
			values[path] = value
		}
	}
	if asJSON {
		return printJSON(cfg, map[string]any{"host": host, "source": source, "checkout": checkout, "personal": personal, "values": values})
	}
	fmt.Fprintf(cfg.Out, "Host: %s\nSource: %s\n", host, source)
	if checkout != "" {
		fmt.Fprintf(cfg.Out, "Checkout: %s (%s)\n", checkout, values[checkout])
	}
	fmt.Fprintf(cfg.Out, "Personal: %s (%s)\n", personal, values[personal])
	return nil
}
