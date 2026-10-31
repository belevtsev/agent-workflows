package workflow

import (
	"errors"
	"fmt"
	"path/filepath"
	"slices"
)

const (
	hostCodex  = "codex"
	hostClaude = "claude"
)

// hostTarget names where one agent host discovers skills, global instructions
// and settings. Everything else about an installation is shared between hosts.
type hostTarget struct {
	id, root, instructions, settings, skills string
}

func (i *Installer) host(id string) hostTarget {
	if id == hostClaude {
		return hostTarget{id: hostClaude, root: i.Claude, instructions: filepath.Join(i.Claude, "CLAUDE.md"), settings: filepath.Join(i.Claude, "settings.json"), skills: filepath.Join(i.Claude, "skills")}
	}
	return hostTarget{id: hostCodex, root: i.Codex, instructions: i.agents, settings: i.config, skills: i.skills}
}

// knownHosts lists every host this installer has roots for, in activation order.
func (i *Installer) knownHosts() []hostTarget {
	hosts := []hostTarget{i.host(hostCodex)}
	if i.Claude != "" {
		hosts = append(hosts, i.host(hostClaude))
	}
	return hosts
}

func hostRecord(s Object, id string) Object { return object(object(s["hosts"])[id]) }

func (i *Installer) recordedHosts(s Object) []hostTarget {
	var hosts []hostTarget
	for _, h := range i.knownHosts() {
		if hostRecord(s, h.id) != nil {
			hosts = append(hosts, h)
		}
	}
	return hosts
}

// selectedHosts resolves what an install acts on. Explicit hosts are additive to
// the recorded ones, and a fresh install uses the only host it can detect rather
// than writing into a second tool's configuration unasked.
func (i *Installer) selectedHosts(s Object) ([]hostTarget, error) {
	if i.AdoptPersonalSkills && len(i.Hosts) > 0 && !slices.Contains(i.Hosts, hostCodex) {
		return nil, errors.New("personal skill adoption applies to the Codex host")
	}
	if len(i.Hosts) == 0 {
		if s != nil {
			return i.recordedHosts(s), nil
		}
		var detected []hostTarget
		for _, h := range i.knownHosts() {
			if exists(h.root) {
				detected = append(detected, h)
			}
		}
		if len(detected) != 1 {
			return nil, errors.New("cannot choose a host; pass --host codex, claude or all")
		}
		return detected, nil
	}
	var hosts []hostTarget
	for _, h := range i.knownHosts() {
		if slices.Contains(i.Hosts, h.id) || hostRecord(s, h.id) != nil {
			hosts = append(hosts, h)
		}
	}
	for _, id := range i.Hosts {
		if !slices.ContainsFunc(hosts, func(h hostTarget) bool { return h.id == id }) {
			return nil, fmt.Errorf("unsupported host: %s", id)
		}
	}
	return hosts, nil
}

func (i *Installer) isInstructions(path string) bool {
	return slices.ContainsFunc(i.knownHosts(), func(h hostTarget) bool { return h.instructions == path })
}

func (i *Installer) isSkillLink(path string) bool {
	return slices.ContainsFunc(i.knownHosts(), func(h hostTarget) bool { return filepath.Dir(path) == h.skills }) && registrationName(filepath.Base(path))
}

// RecordedClaudeHome reads the Claude root an installation already uses. A
// journal comes first because interrupted uninstalls can leave no state file.
func RecordedClaudeHome(stateDir string) string {
	if journal, err := readJournalRecord(filepath.Join(stateDir, "journal.json")); err == nil {
		return text(journal["claude_home"])
	}
	if state, err := readOwnership(filepath.Join(stateDir, "state.json")); err == nil {
		return text(hostRecord(state, hostClaude)["home"])
	}
	return ""
}
