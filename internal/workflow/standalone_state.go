package workflow

import (
	json "encoding/json/v2"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/belevtsev/agent-workflows/internal/bundled"
)

type standaloneActivationRecord struct {
	Release string           `json:"release"`
	Base    bundled.Identity `json:"base"`
}

// Version four is deliberately separate from the sealed legacy v3 boundary.
type standaloneOwnershipRecord struct {
	Version               int                          `json:"version"`
	Release               string                       `json:"release"`
	Source                string                       `json:"source,omitempty"`
	Home                  string                       `json:"home"`
	StateDir              string                       `json:"state_dir"`
	ConfigDir             string                       `json:"config_dir"`
	Base                  bundled.Identity             `json:"base"`
	PreferencesSHA256     string                       `json:"preferences_sha256"`
	ManifestRegistrations map[string]string            `json:"manifest_registrations"`
	Hosts                 map[string]HostRecord        `json:"hosts"`
	History               []standaloneActivationRecord `json:"history"`
	Manager               *ManagerRecord               `json:"manager,omitempty"`
	Integrity             string                       `json:"integrity_sha256"`
}

type standaloneJournalRecord struct {
	Version            int               `json:"version"`
	Command            string            `json:"command"`
	Home               string            `json:"home"`
	CodexHome          string            `json:"codex_home"`
	ClaudeHome         string            `json:"claude_home"`
	StateDir           string            `json:"state_dir"`
	ConfigDir          string            `json:"config_dir"`
	Operations         []OperationRecord `json:"operations"`
	RecoveryOperations []OperationRecord `json:"recovery_operations,omitempty"`
	RecoveryRuntime    *RuntimeIdentity  `json:"recovery_runtime,omitempty"`
	RecoveryRuntimeKey string            `json:"recovery_runtime_key,omitempty"`
	Integrity          string            `json:"integrity_sha256"`
}

func safeAbsolute(path string) bool {
	return path != "" && filepath.IsAbs(path) && Normalize(path) == path && !strings.ContainsAny(path, "\x00\r\n")
}

func decodeStandaloneOwnership(value Object) (standaloneOwnershipRecord, error) {
	var state standaloneOwnershipRecord
	if err := json.Unmarshal(legacyJSON(value), &state, json.RejectUnknownMembers(true)); err != nil {
		return state, fmt.Errorf("invalid standalone ownership: %w", err)
	}
	if state.Version != 4 || !effectivePattern.MatchString(state.Release) || !validBaseIdentity(state.Base) || !digestPattern.MatchString(state.PreferencesSHA256) || !safeAbsolute(state.Home) || !safeAbsolute(state.StateDir) || !safeAbsolute(state.ConfigDir) || len(state.Hosts) == 0 || state.ManifestRegistrations == nil || state.History == nil || state.Manager == nil || state.Manager.Version != 2 {
		return state, errors.New("unsupported or incomplete standalone ownership")
	}
	if state.Source != "" && !safeAbsolute(state.Source) {
		return state, errors.New("unsafe retained source path")
	}
	for id, host := range state.Hosts {
		if (id != hostCodex && id != hostClaude) || !safeAbsolute(host.Home) || host.Registrations == nil || host.GlobalOrigin == nil {
			return state, errors.New("invalid standalone host ownership")
		}
	}
	for _, previous := range state.History {
		if !validReleaseKey(previous.Release) || !validBaseIdentity(previous.Base) || commitSHA.MatchString(previous.Release) && previous.Release != previous.Base.Revision {
			return state, errors.New("invalid standalone activation history")
		}
	}
	return state, nil
}

func (i *Installer) validateStandaloneState(state Object) error {
	record, err := decodeStandaloneOwnership(state)
	if err != nil {
		return err
	}
	if record.Home != i.Home || record.StateDir != i.State {
		return errors.New("standalone ownership roots do not match")
	}
	config, err := ResolveConfigDir(i.Home, i.ConfigDir, record.ConfigDir, i.Getenv)
	if err != nil {
		return err
	}
	i.ConfigDir = config
	if len(i.recordedHosts(state)) != len(record.Hosts) {
		return errors.New("installation records a host outside these roots")
	}
	for _, h := range i.recordedHosts(state) {
		if err := i.validateHostStructure(h, hostRecord(state, h.id), object(state["manifest_registrations"])); err != nil {
			return err
		}
	}
	if record.Manager != nil {
		if err := i.validateManager(object(state["manager"])); err != nil {
			return err
		}
		if record.Manager.Source != record.Source {
			return errors.New("standalone source metadata differs from its manager")
		}
	}
	return i.verifySnapshotBinding(state)
}

func decodeStandaloneJournal(value Object) error {
	var record standaloneJournalRecord
	if err := json.Unmarshal(legacyJSON(value), &record, json.RejectUnknownMembers(true)); err != nil {
		return fmt.Errorf("invalid standalone journal: %w", err)
	}
	if record.Version != 4 || record.Operations == nil || !safeAbsolute(record.Home) || !safeAbsolute(record.StateDir) || !safeAbsolute(record.ConfigDir) {
		return errors.New("unsupported or incomplete standalone journal")
	}
	switch record.Command {
	case "install", "setup", "update", "rollback", "uninstall":
	default:
		return errors.New("unknown standalone journal command")
	}
	return nil
}

// RecordedInstallation reads only the exact selected state root. It never
// searches cwd, parents, or environment-derived replacement roots.
func RecordedInstallation(stateDir string) (Installation, error) {
	if stateDir == "" {
		return Installation{}, errors.New("an explicit installation state root is required")
	}
	result := Installation{Paths: Paths{State: Normalize(stateDir)}}
	if !safeAbsolute(result.State) {
		return result, errors.New("unsafe installation state root")
	}
	if err := realDirectory(result.State, false); err != nil {
		return result, err
	}
	journalPath := filepath.Join(result.State, "journal.json")
	if exists(journalPath) {
		j, err := readJournalRecord(journalPath)
		if err != nil {
			return result, err
		}
		if err = verifySeal(j); err != nil {
			return result, err
		}
		if err = decodeJournal(j); err != nil {
			return result, err
		}
		if j["state_dir"] != result.State {
			return result, errors.New("journal belongs to another state root")
		}
		result.Pending, result.Home, result.Codex, result.Claude, result.ConfigDir = true, text(j["home"]), text(j["codex_home"]), text(j["claude_home"]), text(j["config_dir"])
	}
	path := filepath.Join(result.State, "state.json")
	if !exists(path) {
		return result, nil
	}
	state, err := readOwnership(path)
	if err != nil {
		return result, err
	}
	if err = verifySeal(state); err != nil {
		return result, err
	}
	if state["state_dir"] != result.State {
		return result, errors.New("ownership belongs to another state root")
	}
	result.Installed, result.StateVersion, result.Generation = true, integer(state["version"]), text(state["integrity_sha256"])
	result.Source = text(state["source"])
	if !result.Pending {
		result.Home = text(state["home"])
		result.Codex, result.Claude = text(hostRecord(state, hostCodex)["home"]), text(hostRecord(state, hostClaude)["home"])
		result.ConfigDir = text(state["config_dir"])
	} else if result.Home != state["home"] || integer(state["version"]) == 4 && result.ConfigDir != state["config_dir"] {
		return result, errors.New("pending journal and ownership roots differ")
	}
	return result, nil
}

func privateDirectory(path string, create bool) error {
	if !safeAbsolute(path) {
		return errors.New("private configuration requires an absolute clean path")
	}
	if err := realDirectory(path, create); err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) && !create {
		return nil
	}
	if err != nil {
		return err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !info.IsDir() || info.Mode().Perm() != 0700 || !ok || int(stat.Uid) != os.Geteuid() {
		return errors.New("private configuration directory must be owned by the current user with mode 0700")
	}
	return nil
}

func (i *Installer) preferencesPath() string { return filepath.Join(i.ConfigDir, "preferences.json") }

func (i *Installer) readPreferences() (Preferences, Object, error) {
	prefs := Preferences{Version: 1}
	if err := privateDirectory(i.ConfigDir, false); err != nil {
		return prefs, nil, err
	}
	path := i.preferencesPath()
	if !exists(path) {
		return prefs, Object{"kind": "absent"}, nil
	}
	info, err := os.Lstat(path)
	if err != nil {
		return prefs, nil, err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || !ok || int(stat.Uid) != os.Geteuid() || info.Size() > 1<<20 {
		return prefs, nil, errors.New("preferences must be a current-user regular file with mode 0600")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return prefs, nil, err
	}
	prefs, err = DecodePreferences(data)
	if err != nil {
		return prefs, nil, err
	}
	return prefs, Object{"kind": "file", "data": encode(data), "mode": 0600}, nil
}

func (i *Installer) verifyPreferences(state Object) error {
	if integer(state["version"]) != 4 {
		return nil
	}
	prefs, observation, err := i.readPreferences()
	if err != nil {
		return err
	}
	if observation["kind"] != "file" || hash(legacyJSON(prefs)) != state["preferences_sha256"] {
		return errors.New("owned preferences changed; refusing activation")
	}
	return nil
}

func (i *Installer) validatePrivateDestinations() error {
	prefs := i.preferencesPath()
	for _, path := range []string{i.statePath, i.current, i.journalPath, i.commandPath(), i.runtimeCurrent(), i.locatorPath(), i.config, i.host(hostClaude).settings, i.agents, i.host(hostClaude).instructions} {
		overlap, err := pathsOverlap(prefs, path)
		if err != nil {
			return err
		}
		if overlap {
			return fmt.Errorf("preferences alias another managed destination: %s", path)
		}
	}
	for _, root := range []string{i.State, i.Codex, i.Claude, i.skills, i.host(hostClaude).skills} {
		if root == "" {
			continue
		}
		overlap, err := pathsOverlap(root, i.ConfigDir)
		if err != nil {
			return err
		}
		if overlap {
			return errors.New("private configuration root overlaps a managed state or host root")
		}
	}
	return nil
}
