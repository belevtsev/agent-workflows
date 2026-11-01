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
	if err := i.checkSkillDirectories(hosts); err != nil {
		return nil, err
	}
	return hosts, nil
}

// checkSkillDirectories refuses hosts that would discover each other's
// registrations. Codex also reads skills from under its own home, so a shared
// directory would commit state that later verification rejects.
func (i *Installer) checkSkillDirectories(hosts []hostTarget) error {
	if len(hosts) < 2 {
		return nil
	}
	claude := i.host(hostClaude).skills
	for _, root := range i.codexSkillRoots() {
		same, undecided, err := sameDirectory(root, claude)
		switch {
		case err != nil:
			return err
		case same:
			return fmt.Errorf("Codex and Claude Code would share a skills directory: %s; use separate --codex-home and --claude-home directories", claude)
		case undecided:
			return fmt.Errorf("cannot tell whether %s and %s are separate directories until they exist; create both and run this again", root, claude)
		}
	}
	return nil
}

// checkHomesOffRegistrations refuses a home at or below a path a skill needs.
// Codex verification would read that home as a duplicate of the skill, and a
// link there would sit above the home's own files. Other names under a skills
// directory collide with nothing.
func (i *Installer) checkHomesOffRegistrations(hosts []hostTarget, registrations Object) error {
	var roots []string
	for _, h := range hosts {
		if h.id == hostCodex {
			roots = append(roots, i.codexSkillRoots()...)
		} else {
			roots = append(roots, h.skills)
		}
	}
	for _, h := range hosts {
		for _, root := range roots {
			entry, err := entryHolding(root, Normalize(h.root))
			if err != nil {
				return err
			}
			if entry == "" {
				continue
			}
			for _, name := range keys(registrations) {
				same, undecided, err := sameDirectory(filepath.Join(root, name), filepath.Join(root, entry))
				switch {
				case err != nil:
					return err
				case same:
					return fmt.Errorf("home %s sits on the %s skill in %s; choose a home outside that path", h.root, name, root)
				case undecided:
					return fmt.Errorf("cannot tell whether home %s sits on the %s skill until it exists; create the home and run this again", h.root, name)
				}
			}
		}
	}
	return nil
}

// addsHost reports whether an install would activate a host state does not
// record yet.
func (i *Installer) addsHost(s Object) (bool, error) {
	if s == nil {
		return false, nil
	}
	hosts, err := i.selectedHosts(s)
	if err != nil {
		return false, err
	}
	return slices.ContainsFunc(hosts, func(h hostTarget) bool { return hostRecord(s, h.id) == nil }), nil
}

func (i *Installer) isInstructions(path string) bool {
	return slices.ContainsFunc(i.knownHosts(), func(h hostTarget) bool { return h.instructions == path })
}

func (i *Installer) isSkillLink(path string) bool {
	return slices.ContainsFunc(i.knownHosts(), func(h hostTarget) bool { return filepath.Dir(path) == h.skills }) && registrationName(filepath.Base(path))
}

// RecordedHomes reads the host roots an installation already uses. A journal
// comes first because interrupted uninstalls can leave no state file. A host
// with no record returns an empty root and stays free to enroll anywhere.
func RecordedHomes(stateDir string) (codex, claude string) {
	if journal, err := readJournalRecord(filepath.Join(stateDir, "journal.json")); err == nil {
		return text(journal["codex_home"]), text(journal["claude_home"])
	}
	if state, err := readOwnership(filepath.Join(stateDir, "state.json")); err == nil {
		return text(hostRecord(state, hostCodex)["home"]), text(hostRecord(state, hostClaude)["home"])
	}
	return "", ""
}
