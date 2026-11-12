// Package userconfig locates the user's private agent-workflows configuration
// directory and reads the nonsecret preferences the runtime needs from it.
package userconfig

import (
	"encoding/json/jsontext"
	json "encoding/json/v2"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"

	"github.com/belevtsev/agent-workflows/v3/internal/modelpolicy"
)

const (
	directoryName       = "agent-workflows"
	preferencesName     = "preferences.json"
	maxPreferencesBytes = 1 << 20
)

// Dir resolves the configuration directory: explicit (the --config-dir
// value) when given, else $XDG_CONFIG_HOME/agent-workflows, else
// <home>/.config/agent-workflows. The selected root must be an absolute
// clean path. On macOS a path under /tmp or /var is spelled under /private,
// so a credential store that refuses symlinks still reaches it. A nil getenv
// reads the process environment.
func Dir(home, explicit string, getenv func(string) string) (string, error) {
	if getenv == nil {
		getenv = os.Getenv
	}
	if explicit != "" {
		if !safeAbsolute(explicit) {
			return "", errors.New("explicit configuration directory must be an absolute clean path")
		}
		return normalizeAlias(explicit), nil
	}
	root := getenv("XDG_CONFIG_HOME")
	if root != "" {
		if !safeAbsolute(root) {
			return "", errors.New("XDG_CONFIG_HOME must be an absolute clean path")
		}
		return normalizeAlias(filepath.Join(root, directoryName)), nil
	}
	if !safeAbsolute(home) {
		return "", errors.New("home directory must be an absolute clean path")
	}
	return normalizeAlias(filepath.Join(home, ".config", directoryName)), nil
}

func safeAbsolute(path string) bool {
	return path != "" && filepath.IsAbs(path) && filepath.Clean(path) == path && !strings.ContainsAny(path, "\x00\r\n")
}

// normalizeAlias spells a path under a /tmp or /var symlink to /private as
// its target, as macOS lays them out.
func normalizeAlias(path string) string {
	for _, name := range []string{"var", "tmp"} {
		alias := "/" + name
		target, err := os.Readlink(alias)
		if err == nil && (target == "private/"+name || target == "/private/"+name) && (path == alias || strings.HasPrefix(path, alias+"/")) {
			return "/private" + path
		}
	}
	return path
}

// preferences is the shape of preferences.json. The runtime reads version
// and advisers; hosts and shared, which the installer wrote, are accepted
// unread. Any other member is refused, so a misspelled advisers member
// cannot silently leave an adviser enabled.
type preferences struct {
	Version  *int           `json:"version"`
	Hosts    jsontext.Value `json:"hosts"`
	Shared   jsontext.Value `json:"shared"`
	Advisers jsontext.Value `json:"advisers"`
}

type adviserPreferences struct {
	Enabled  []string `json:"enabled"`
	Disabled []string `json:"disabled"`
}

// Advisers is the adviser portion of the saved preferences. Each list holds
// known provider IDs in provider order and is nil when empty.
type Advisers struct {
	// Enabled lists the advisers the user opted in to (advisers.enabled).
	Enabled []string
	// Disabled lists the advisers the user turned off (advisers.disabled).
	Disabled []string
}

// AdviserPreferences returns the provider IDs that <dir>/preferences.json
// lists under advisers.enabled and advisers.disabled. A missing file enables
// and disables none and leaves the directory unchecked. An existing file is
// read only when dir is reached without symlinks and is a current-user
// directory with mode 0700, and the file is a current-user regular file with
// mode 0600 of at most 1 MiB holding a version-1 preferences document whose
// IDs are known, listed once in each list and never in both; a top-level
// member other than version, hosts, shared and advisers is refused.
func AdviserPreferences(dir string) (Advisers, error) {
	if !safeAbsolute(dir) {
		return Advisers{}, errors.New("private configuration requires an absolute clean path")
	}
	path := filepath.Join(dir, preferencesName)
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return Advisers{}, nil
	}
	if err != nil {
		return Advisers{}, err
	}
	if err := privateDirectory(dir); err != nil {
		return Advisers{}, err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 || !ownedByCaller(info) || info.Size() > maxPreferencesBytes {
		return Advisers{}, errors.New("preferences must be a current-user regular file with mode 0600")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Advisers{}, err
	}
	return decodeAdvisers(data)
}

// privateDirectory requires every existing ancestor of dir, and dir itself,
// to be a directory that is not a symbolic link, and dir to be owned by the
// caller with mode 0700.
func privateDirectory(dir string) error {
	for current := dir; ; current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if err == nil && (!info.IsDir() || info.Mode()&os.ModeSymlink != 0) {
			return fmt.Errorf("unsafe directory: %s", current)
		}
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if filepath.Dir(current) == current {
			break
		}
	}
	info, err := os.Lstat(dir)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode().Perm() != 0o700 || !ownedByCaller(info) {
		return errors.New("private configuration directory must be owned by the current user with mode 0700")
	}
	return nil
}

func ownedByCaller(info os.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && int(stat.Uid) == os.Geteuid()
}

func decodeAdvisers(data []byte) (Advisers, error) {
	var prefs preferences
	if err := json.Unmarshal(data, &prefs, json.RejectUnknownMembers(true)); err != nil {
		return Advisers{}, fmt.Errorf("invalid preferences: %w", err)
	}
	if prefs.Version == nil || *prefs.Version != 1 {
		return Advisers{}, errors.New("preferences require version 1")
	}
	if len(prefs.Advisers) == 0 || string(prefs.Advisers) == "null" {
		return Advisers{}, nil
	}
	var advisers adviserPreferences
	if err := json.Unmarshal(prefs.Advisers, &advisers, json.RejectUnknownMembers(true)); err != nil {
		return Advisers{}, fmt.Errorf("invalid preferences: %w", err)
	}
	enabled, err := providerList("advisers.enabled", advisers.Enabled)
	if err != nil {
		return Advisers{}, err
	}
	disabled, err := providerList("advisers.disabled", advisers.Disabled)
	if err != nil {
		return Advisers{}, err
	}
	for _, id := range enabled {
		if slices.Contains(disabled, id) {
			return Advisers{}, fmt.Errorf("adviser %q is listed in both advisers.enabled and advisers.disabled", id)
		}
	}
	return Advisers{Enabled: enabled, Disabled: disabled}, nil
}

// providerList checks that every ID in one advisers list is known and listed
// once, and returns them in provider order; nil when none.
func providerList(member string, ids []string) ([]string, error) {
	for i, id := range ids {
		if !modelpolicy.Known(id) {
			return nil, fmt.Errorf("unknown adviser %q in %s", id, member)
		}
		if slices.Contains(ids[:i], id) {
			return nil, fmt.Errorf("adviser %q is listed twice in %s", id, member)
		}
	}
	var ordered []string
	for _, id := range modelpolicy.Providers() {
		if slices.Contains(ids, id) {
			ordered = append(ordered, id)
		}
	}
	return ordered, nil
}
