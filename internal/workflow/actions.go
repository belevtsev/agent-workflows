package workflow

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

func (i *Installer) activation(s Object, sha, release string, m Object, enroll, rollback bool) ([]Object, error) {
	recorded := i.recordedHosts(s)
	var ops []Object
	plans := map[string]registrationChangePlan{}
	for _, h := range recorded {
		plan, e := i.registrationPlan(h, s, m, rollback, release)
		if e != nil {
			return nil, e
		}
		plans[h.id] = plan
		ops = append(ops, plan.Operations...)
	}
	if e := i.verifyOwned(s); e != nil {
		return nil, e
	}
	after := clone(s)
	hosts := clone(object(s["hosts"]))
	for _, h := range recorded {
		hosts[h.id] = clone(hostRecord(s, h.id))
	}
	if !equal(s["manifest_registrations"], m["registrations"]) {
		for _, h := range recorded {
			object(hosts[h.id])["registrations"] = plans[h.id].Records
		}
		after["manifest_registrations"] = m["registrations"]
	}
	if sha != s["release"] {
		history := append([]any{}, sequence(s["history"])...)
		if rollback {
			history = history[:len(history)-1]
		} else {
			history = append(history, Object{"release": s["release"]})
		}
		after["history"] = history
		after["release"] = sha
		template, e := os.ReadFile(filepath.Join(release, text(m["global_instructions"])))
		if e != nil {
			return nil, e
		}
		ops = append(ops, pathOperation(i.current, i.expectedPointer(s), Object{"kind": "symlink", "target": release}, "pointer"))
		for _, h := range recorded {
			hs := object(hosts[h.id])
			old, e := decode(hs["global_segment"])
			if e != nil {
				return nil, e
			}
			segment := managedBlock(template, old[:len(old)-len(bytes.TrimLeft(old, "\n"))])
			hs["global_segment"] = encode(segment)
			contents, e := i.readInstructions(h)
			if e != nil {
				return nil, e
			}
			replacement, e := replaceOwned(contents, old, segment)
			if e != nil {
				return nil, e
			}
			global, e := i.globalOperation(h, contents, replacement, old, segment, hostLabel(h, "global"))
			if e != nil {
				return nil, e
			}
			ops = append(ops, global)
		}
	}
	for _, h := range recorded {
		updated, change, e := i.prepareHostConfig(h, object(hosts[h.id]), release, m, enroll)
		if e != nil {
			return nil, e
		}
		hosts[h.id] = updated
		if change != nil {
			ops = append(ops, change)
		}
	}
	if !rollback {
		added, changes, e := i.addHosts(s, release, m, enroll)
		if e != nil {
			return nil, e
		}
		for id, record := range added {
			hosts[id] = record
		}
		ops = append(ops, changes...)
	}
	after["hosts"] = hosts
	after = seal(after)
	if !equal(after, s) {
		ops = append(ops, pathOperation(i.statePath, expectedState(s), expectedState(after), "state"))
	}
	return ops, nil
}

// addHosts plans the first activation of selected hosts that state does not
// record yet. Adding a host never changes the release or its history.
func (i *Installer) addHosts(s Object, release string, m Object, enroll bool) (Object, []Object, error) {
	selected, err := i.selectedHosts(s)
	if err != nil {
		return nil, nil, err
	}
	records := Object{}
	var ops []Object
	for _, h := range selected {
		if hostRecord(s, h.id) != nil {
			continue
		}
		record, changes, _, err := i.installHost(h, s, release, m)
		if err != nil {
			return nil, nil, err
		}
		record, change, err := i.prepareHostConfig(h, record, release, m, enroll)
		if err != nil {
			return nil, nil, err
		}
		records[h.id] = record
		ops = append(ops, changes...)
		if change != nil {
			ops = append(ops, change)
		}
	}
	return records, ops, nil
}

func (i *Installer) prepareHostConfig(h hostTarget, hs Object, release string, m Object, enroll bool) (Object, Object, error) {
	if h.id == hostClaude {
		return i.prepareClaudeConfig(h, hs, release, m, enroll)
	}
	shared, err := SharedConfigDefaults(release)
	if err != nil {
		return nil, nil, err
	}
	var models map[string]string
	if enroll || hs["model_config"] != nil {
		models, err = ModelDefaults(release, m)
		if err != nil {
			return nil, nil, err
		}
	}
	if shared != nil || sharedConfigStateRequired(hs) {
		return configPrepare(i.config, models, shared, hs)
	}
	after := clone(hs)
	if models == nil {
		return after, nil, nil
	}
	metadata, change, err := ModelPrepare(i.config, models, object(hs["model_config"]))
	if err != nil {
		return nil, nil, err
	}
	after["model_config"] = metadata
	return after, change, nil
}

func (i *Installer) prepareClaudeConfig(h hostTarget, hs Object, release string, m Object, enroll bool) (Object, Object, error) {
	after := clone(hs)
	if !enroll && hs["settings"] == nil {
		return after, nil, nil
	}
	defaults, err := ClaudeModelDefaults(release, m)
	if err != nil {
		return nil, nil, err
	}
	metadata, change, err := ClaudeSettingsPrepare(h.settings, defaults, object(hs["settings"]))
	if err != nil {
		return nil, nil, err
	}
	after["settings"] = metadata
	return after, change, nil
}

func (i *Installer) Setup(enroll bool) (Object, error) {
	command := "setup"
	if !enroll {
		command = "install"
	}
	return i.standaloneApply(command, true)
}

func (i *Installer) Status() (Object, error) {
	if err := legacyRecords(i.State); err != nil {
		return nil, err
	}
	if exists(i.journalPath) {
		return nil, errors.New("an unfinished mutation requires recover")
	}
	s, e := i.state(false)
	if e != nil {
		return nil, e
	}
	if s == nil {
		if exists(i.current) {
			return nil, errors.New("active pointer exists without ownership state")
		}
		return Object{"installed": false, "model_config": Object{"managed": false}, "shared_config": Object{"managed": false}, "manager_command": Object{"managed": false}}, nil
	}
	if e = i.verifyOwned(s); e != nil {
		return nil, e
	}
	release, m, e := i.release(text(s["release"]))
	if e != nil {
		return nil, e
	}
	defaults, profiles, e := modelSettings(release, m)
	if e != nil {
		return nil, e
	}
	var previous any
	h := sequence(s["history"])
	if len(h) > 0 {
		previous = object(h[len(h)-1])["release"]
	}
	codex := hostRecord(s, hostCodex)
	report := Object{"installed": true, "release": s["release"], "previous_release": previous, "registrations": keys(object(s["manifest_registrations"])), "state_dir": i.State, "model_defaults": defaults, "model_profiles": profiles, "model_config": Object{"managed": codex["model_config"] != nil}, "manager_command": Object{"managed": false}}
	shared, e := SharedConfigDefaults(release)
	if e != nil {
		return nil, e
	}
	report["shared_config"] = Object{"managed": codex["shared_config"] != nil, "defaults": shared}
	report["hosts"], e = i.hostReports(s, release, m)
	if e != nil {
		return nil, e
	}
	if manager := object(s["manager"]); manager != nil {
		report["manager"] = manager
		report["manager_command"] = Object{"managed": true, "kind": "native", "path": i.commandPath()}
	}
	prefs, _, err := i.readPreferences()
	if err != nil {
		return nil, err
	}
	report["config_dir"], report["preferences"], report["generation"], report["base_identity"], report["effective_digest"] = i.ConfigDir, prefs, s["integrity_sha256"], s["base"], strings.TrimPrefix(text(s["release"]), "effective-")
	return report, nil
}
func (i *Installer) Update() (Object, error) { return i.standaloneApply("update", true) }

func (i *Installer) Rollback() (Object, error) { return i.standaloneRollback() }

func (i *Installer) Uninstall() (Object, error) {
	if err := i.Context.Err(); err != nil {
		return nil, err
	}
	if err := legacyRecords(i.State); err != nil {
		return nil, err
	}
	s, e := i.state(true)
	if e != nil {
		return nil, e
	}
	if e = i.verifyOwned(s); e != nil {
		return nil, e
	}
	removed := i.recordedHosts(s)
	if len(i.Hosts) > 0 {
		removed = slices.DeleteFunc(removed, func(h hostTarget) bool { return !slices.Contains(i.Hosts, h.id) })
		if len(removed) != len(i.Hosts) {
			return nil, errors.New("uninstall names a host this installation does not record")
		}
	}
	ops, e := i.uninstallOperations(s, removed)
	if e != nil {
		return nil, e
	}
	if e = i.preflightMutationPaths(ops, s, false); e != nil {
		return nil, e
	}
	if !i.Apply {
		return Object{"command": "uninstall", "dry_run": true, "release": s["release"], "hosts": hostIDs(removed)}, nil
	}
	unlock, e := lockRoot(i.State, false)
	if e != nil {
		return nil, e
	}
	defer unlock()
	current, e := i.state(true)
	if e != nil {
		return nil, e
	}
	if !equal(current, s) {
		return nil, errors.New("installation changed before uninstall")
	}
	if e = i.verifyOwned(s); e != nil {
		return nil, e
	}
	ops, e = i.uninstallOperations(s, removed)
	if e != nil {
		return nil, e
	}
	if e = i.transact("uninstall", ops); e != nil {
		return nil, e
	}
	if len(removed) < len(i.recordedHosts(s)) {
		return Object{"command": "uninstall", "dry_run": false, "hosts": hostIDs(removed), "restored_adoptions": true}, nil
	}
	return Object{"command": "uninstall", "dry_run": false, "hosts": hostIDs(removed), "restored_adoptions": true, "retained_release_cache": filepath.Join(i.State, "releases")}, nil
}

func (i *Installer) uninstallOperations(s Object, removed []hostTarget) ([]Object, error) {
	ops := []Object{}
	for _, h := range removed {
		if i.Forget {
			continue
		}
		changes, e := i.hostRemoval(h, s)
		if e != nil {
			return nil, e
		}
		ops = append(ops, changes...)
	}
	if len(removed) < len(i.recordedHosts(s)) {
		after := clone(s)
		hosts := clone(object(s["hosts"]))
		for _, h := range removed {
			delete(hosts, h.id)
		}
		after["hosts"] = hosts
		ops = append(ops, pathOperation(i.statePath, expectedState(s), expectedState(seal(after)), "state"))
		return ops, nil
	}
	if manager := object(s["manager"]); manager != nil {
		changes, err := i.managerRemoval(manager)
		if err != nil {
			return nil, err
		}
		ops = append(ops, changes...)
	}
	ops = append(ops, pathOperation(i.current, i.expectedPointer(s), Object{"kind": "absent"}, "pointer"), pathOperation(i.statePath, expectedState(s), Object{"kind": "absent"}, "state"))
	return ops, nil
}

// hostRemoval restores everything one host owns and leaves the shared release,
// manager and other hosts alone.
func (i *Installer) hostRemoval(h hostTarget, s Object) ([]Object, error) {
	hs := hostRecord(s, h.id)
	ops := []Object{}
	registrations := object(hs["registrations"])
	for _, name := range keys(registrations) {
		item := object(registrations[name])
		original := object(item["original"])
		if original["kind"] == "personal" {
			origin, err := i.validatePersonalOrigin(name, original)
			if err != nil {
				return nil, err
			}
			ops = append(ops, personalOperation(name, origin, filepath.Join(h.skills, name), text(item["target"]), "managed", "personal", "restore-personal:"+name))
			continue
		}
		ops = append(ops, pathOperation(filepath.Join(h.skills, name), Object{"kind": "symlink", "target": item["target"]}, original, hostLabel(h, "registration:"+name)))
	}
	contents, e := i.readInstructions(h)
	if e != nil {
		return nil, e
	}
	segment, e := decode(hs["global_segment"])
	if e != nil {
		return nil, e
	}
	after, e := replaceOwned(contents, segment, nil)
	if e != nil {
		return nil, e
	}
	global, e := i.globalOperation(h, contents, after, segment, nil, hostLabel(h, "global"))
	if e != nil {
		return nil, e
	}
	if len(after) == 0 && object(hs["global_origin"])["kind"] == "absent" {
		global["after"] = Object{"kind": "absent"}
	}
	ops = append(ops, global)
	if h.id == hostClaude {
		if meta := object(hs["settings"]); meta != nil {
			change, e := ClaudeSettingsRemoval(h.settings, meta)
			if e != nil {
				return nil, e
			}
			if change != nil {
				ops = append(ops, change)
			}
		}
		return ops, nil
	}
	if sharedConfigStateRequired(hs) {
		change, e := ConfigRemoval(i.config, hs)
		if e != nil {
			return nil, e
		}
		ops = append(ops, change)
	} else if meta := object(hs["model_config"]); meta != nil {
		change, e := ModelRemoval(i.config, meta)
		if e != nil {
			return nil, e
		}
		ops = append(ops, change)
	}
	return ops, nil
}

func hostIDs(hosts []hostTarget) []string {
	ids := []string{}
	for _, h := range hosts {
		ids = append(ids, h.id)
	}
	return ids
}

func (i *Installer) hostReports(s Object, release string, m Object) (Object, error) {
	reports := Object{}
	for _, h := range i.recordedHosts(s) {
		report := Object{"home": h.root, "skills": h.skills, "instructions": h.instructions, "settings": h.settings}
		if h.id == hostClaude {
			defaults, err := ClaudeModelDefaults(release, m)
			if err != nil {
				return nil, err
			}
			report["model_defaults"] = defaults
			drift, err := ClaudeSettingsDrift(h.settings, object(hostRecord(s, h.id)["settings"]))
			if err != nil {
				return nil, err
			}
			report["settings_drift"] = drift
		}
		reports[h.id] = report
	}
	return reports, nil
}

func (i *Installer) Execute(command string) (Object, error) {
	switch command {
	case "setup":
		return i.Setup(true)
	case "install":
		return i.Setup(false)
	case "status":
		return i.Status()
	case "update":
		return i.Update()
	case "rollback":
		return i.Rollback()
	case "uninstall":
		return i.Uninstall()
	case "recover":
		return i.Recover()
	}
	return nil, fmt.Errorf("unknown command %s", command)
}
