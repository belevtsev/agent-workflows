package workflow

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func (i *Installer) activation(s Object, sha, release string, m Object, enroll, rollback bool) ([]Object, error) {
	plan, e := i.registrationPlan(s, m, rollback, release)
	if e != nil {
		return nil, e
	}
	if e := i.verifyOwned(s); e != nil {
		return nil, e
	}
	after := clone(s)
	after["version"] = i.nextStateVersion(s)
	ops := plan.Operations
	if !equal(s["manifest_registrations"], m["registrations"]) {
		after["registrations"] = plan.Records
		after["manifest_registrations"] = m["registrations"]
	}
	if sha != s["release"] {
		history := append([]any{}, sequence(s["history"])...)
		if rollback {
			history = history[:len(history)-1]
		} else {
			history = append(history, Object{"release": s["release"], "global_segment": s["global_segment"]})
		}
		after["history"] = history
		after["release"] = sha
		old, e := decode(s["global_segment"])
		if e != nil {
			return nil, e
		}
		template, e := os.ReadFile(filepath.Join(release, text(m["global_instructions"])))
		if e != nil {
			return nil, e
		}
		segment := managedBlock(template, old[:len(old)-len(bytes.TrimLeft(old, "\n"))])
		after["global_segment"] = encode(segment)
		contents, e := i.readAgents()
		if e != nil {
			return nil, e
		}
		replacement, e := replaceOwned(contents, old, segment)
		if e != nil {
			return nil, e
		}
		global, e := i.globalOperation(contents, replacement, old, segment, "global", nil, nil)
		if e != nil {
			return nil, e
		}
		ops = append(ops, pathOperation(i.current, i.expectedPointer(s), Object{"kind": "symlink", "target": release}, "pointer"), global)
	}
	config, change, e := i.prepareConfig(s, release, m, enroll)
	if e != nil {
		return nil, e
	}
	for _, field := range []string{"model_config", "shared_config", "config_original_exists"} {
		if value, present := config[field]; present {
			after[field] = value
		} else {
			delete(after, field)
		}
	}
	if change != nil {
		ops = append(ops, change)
	}
	if !rollback && i.nativeRequested() && i.RuntimeCandidate != "" {
		manager, changes, e := i.managerPlan(s)
		if e != nil {
			return nil, e
		}
		after["manager"], after["source"] = manager, i.Source
		delete(after, "command_alias")
		ops = append(ops, changes...)
	} else if enroll && !i.nativeRequested() && s["manager"] == nil {
		meta, change, _, e := AliasPrepare(i.Paths, i.Shell, object(s["command_alias"]))
		if e != nil {
			return nil, e
		}
		if meta != nil {
			after["command_alias"] = meta
		}
		if change != nil {
			ops = append(ops, change)
		}
	}
	after = seal(after)
	if !equal(after, s) {
		ops = append(ops, pathOperation(i.statePath, expectedState(s), expectedState(after), "state"))
	}
	return ops, nil
}

func (i *Installer) prepareConfig(s Object, release string, m Object, enroll bool) (Object, Object, error) {
	if i.Apply && i.RuntimeCandidate != "" {
		if err := i.verifyPolicyRuntime(i.RuntimeCandidate, release, m); err != nil {
			return nil, nil, err
		}
	}
	shared, err := SharedConfigDefaults(release)
	if err != nil {
		return nil, nil, err
	}
	if (shared != nil || sharedConfigStateRequired(s)) && i.RuntimeCandidate != "" {
		if err := i.verifySharedConfigRuntime(i.RuntimeCandidate); err != nil {
			return nil, nil, err
		}
	}
	var models map[string]string
	if enroll || s["model_config"] != nil {
		models, err = ModelDefaults(release, m)
		if err != nil {
			return nil, nil, err
		}
	}
	if shared != nil || sharedConfigStateRequired(s) {
		return configPrepare(i.config, models, shared, s)
	}
	after := clone(s)
	if models == nil {
		return after, nil, nil
	}
	metadata, change, err := ModelPrepare(i.config, models, object(s["model_config"]))
	if err != nil {
		return nil, nil, err
	}
	after["model_config"] = metadata
	return after, change, nil
}

func (i *Installer) fresh(sha, release string, m Object, enroll bool) ([]Object, Object, error) {
	records, origin, segment, ops, e := i.preflight(release, m)
	if e != nil {
		return nil, nil, e
	}
	s := i.newState(sha, m, records, origin, segment)
	ops = append([]Object{pathOperation(i.current, Object{"kind": "absent"}, Object{"kind": "symlink", "target": release}, "pointer")}, ops...)
	aliasReport := Object{"managed": false}
	s, change, e := i.prepareConfig(s, release, m, enroll)
	if e != nil {
		return nil, nil, e
	}
	if change != nil {
		ops = append(ops, change)
	}
	if i.nativeRequested() {
		if enroll || s["shared_config"] != nil {
			if i.RuntimeCandidate != "" {
				manager, changes, e := i.managerPlan(nil)
				if e != nil {
					return nil, nil, e
				}
				s["manager"] = manager
				ops = append(ops, changes...)
				aliasReport = Object{"managed": true, "kind": "native", "path": i.commandPath()}
			} else {
				aliasReport = Object{"managed": false, "kind": "native", "status": "deferred"}
			}
		}
	} else if enroll {
		am, ac, ar, e := AliasPrepare(i.Paths, i.Shell, nil)
		if e != nil {
			return nil, nil, e
		}
		aliasReport = ar
		if am != nil {
			s["command_alias"] = am
		}
		if ac != nil {
			ops = append(ops, ac)
		}
	}
	s = seal(s)
	ops = append(ops, pathOperation(i.statePath, Object{"kind": "absent"}, expectedState(s), "state"))
	return ops, aliasReport, nil
}
func (i *Installer) Setup(enroll bool) (Object, error) {
	if err := i.Context.Err(); err != nil {
		return nil, err
	}
	if i.AdoptPersonalSkills && !i.nativeRequested() {
		return nil, errors.New("personal skill adoption requires a native v6 manager")
	}
	if exists(i.journalPath) {
		return nil, errors.New("an unfinished mutation requires recover")
	}
	sha, e := cleanHeadContext(i.Context, i.Source)
	if e != nil {
		return nil, e
	}
	s, e := i.state(false)
	if e != nil {
		return nil, e
	}
	release, m, cleanup, e := i.preview(sha)
	if e != nil {
		return nil, e
	}
	defer cleanup()
	plan, e := i.registrationPlan(s, m, false, release)
	if e != nil {
		return nil, e
	}
	if e = i.prepareRuntimeFor(sha); e != nil {
		return nil, e
	}
	var ops []Object
	var ar Object
	if s == nil {
		ops, ar, e = i.fresh(sha, release, m, enroll)
	} else {
		if !enroll {
			return nil, errors.New("an installation exists; use setup or update")
		}
		if e = descendantContext(i.Context, i.Source, text(s["release"]), sha); e == nil {
			ops, e = i.activation(s, sha, release, m, enroll, false)
		}
		if e == nil && !i.nativeRequested() && s["manager"] == nil {
			_, _, ar, e = AliasPrepare(i.Paths, i.Shell, object(s["command_alias"]))
		}
	}
	if e != nil {
		return nil, e
	}
	defaults, e := ModelDefaults(release, m)
	if e != nil {
		return nil, e
	}
	changed := len(ops) > 0
	command := "setup"
	if !enroll {
		command = "install"
	}
	report := Object{"command": command, "dry_run": !i.Apply, "release": sha, "registrations": keys(object(m["registrations"])), "registration_changes": plan.Changes, "changed": changed, "model_defaults": defaults, "command_alias": ar, "model_config": Object{"managed": enroll}}
	shared, e := SharedConfigDefaults(release)
	if e != nil {
		return nil, e
	}
	report["shared_config"] = Object{"managed": shared != nil, "defaults": shared}
	if !i.Apply {
		runtime, err := i.prepareConsultationRuntimeFor(release)
		if err != nil {
			return nil, err
		}
		report["consultation_runtime"] = runtime
		return report, nil
	}
	unlock, e := lockRoot(i.State, false)
	if e != nil {
		return nil, e
	}
	defer unlock()
	currentHead, e := cleanHeadContext(i.Context, i.Source)
	if e != nil {
		return nil, e
	}
	currentState, e := i.state(false)
	if e != nil {
		return nil, e
	}
	if currentHead != sha || !equal(s, currentState) {
		return nil, errors.New("source HEAD or installation changed before setup")
	}
	if i.RuntimeCandidate != "" {
		if e = i.stageRuntime(); e != nil {
			return nil, e
		}
		if _, _, e = i.managerPlan(s); e != nil {
			return nil, e
		}
	}
	release, m, e = i.stage(sha)
	if e != nil {
		return nil, e
	}
	if s == nil {
		ops, ar, e = i.fresh(sha, release, m, enroll)
	} else {
		ops, e = i.activation(s, sha, release, m, enroll, false)
	}
	if e != nil {
		return nil, e
	}
	runtime, e := i.prepareConsultationRuntimeFor(release)
	if e != nil {
		return nil, e
	}
	report["consultation_runtime"] = runtime
	head, e := cleanHeadContext(i.Context, i.Source)
	if e != nil || head != sha {
		return nil, errors.New("source HEAD changed during setup")
	}
	if len(ops) > 0 {
		if e = i.transact(command, ops); e != nil {
			return nil, e
		}
	}
	report["changed"] = len(ops) > 0 || runtime["changed"] == true
	report["command_alias"] = ar
	return report, nil
}
func (i *Installer) Status() (Object, error) {
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
		return Object{"installed": false, "model_config": Object{"managed": false}, "shared_config": Object{"managed": false}, "command_alias": Object{"managed": false}}, nil
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
	report := Object{"installed": true, "release": s["release"], "previous_release": previous, "registrations": keys(object(s["registrations"])), "state_dir": i.State, "model_defaults": defaults, "model_profiles": profiles, "model_config": Object{"managed": s["model_config"] != nil}, "command_alias": AliasReport(object(s["command_alias"]))}
	shared, e := SharedConfigDefaults(release)
	if e != nil {
		return nil, e
	}
	report["shared_config"] = Object{"managed": s["shared_config"] != nil, "defaults": shared}
	if manager := object(s["manager"]); manager != nil {
		report["manager"] = manager
		report["command_alias"] = Object{"managed": true, "kind": "native", "path": i.commandPath()}
	}
	return report, nil
}
func (i *Installer) allowedOrigin() error {
	url, e := gitContext(i.Context, i.Source, "config", "--get", "remote.origin.url")
	if e != nil {
		return e
	}
	value := strings.TrimSpace(string(url))
	for _, base := range []string{"git@github.com:belevtsev/agent-workflows", "https://github.com/belevtsev/agent-workflows", "ssh://git@github.com/belevtsev/agent-workflows"} {
		if value == base || value == base+".git" {
			return nil
		}
	}
	return errors.New("origin must be the belevtsev/agent-workflows GitHub repository")
}
func (i *Installer) Update() (Object, error) {
	if i.AdoptPersonalSkills {
		return nil, errors.New("personal skill adoption is available only for install and setup")
	}
	if err := i.Context.Err(); err != nil {
		return nil, err
	}
	s, e := i.state(true)
	if e != nil {
		return nil, e
	}
	if e = i.verifyOwned(s); e != nil {
		return nil, e
	}
	head, e := cleanHeadContext(i.Context, i.Source)
	if e != nil {
		return nil, e
	}
	if e = i.allowedOrigin(); e != nil {
		return nil, e
	}
	if !i.Apply {
		if e = descendantContext(i.Context, i.Source, text(s["release"]), head); e != nil {
			if e = descendantContext(i.Context, i.Source, head, text(s["release"])); e != nil {
				return nil, e
			}
			// A no-checkout update can leave an older source snapshot. It is
			// not a forward candidate; the remote candidate remains deferred.
			return Object{"command": "update", "dry_run": true, "local_head": head, "active_release": s["release"], "registration_changes": []RegistrationChange{}, "source_behind_active": true, "fetch": "origin/main only with apply"}, nil
		}
		release, m, cleanup, e := i.preview(head)
		if e != nil {
			return nil, e
		}
		defer cleanup()
		plan, e := i.registrationPlan(s, m, false)
		if e != nil {
			return nil, e
		}
		shared, e := SharedConfigDefaults(release)
		if e != nil {
			return nil, e
		}
		if _, _, e = i.prepareConfig(s, release, m, false); e != nil {
			return nil, e
		}
		runtime, e := i.prepareConsultationRuntimeFor(release)
		if e != nil {
			return nil, e
		}
		return Object{"command": "update", "dry_run": true, "local_head": head, "active_release": s["release"], "registration_changes": plan.Changes, "shared_config": Object{"managed": shared != nil, "defaults": shared}, "consultation_runtime": runtime, "fetch": "origin/main only with apply"}, nil
	}
	unlock, e := lockRoot(i.State, false)
	if e != nil {
		return nil, e
	}
	defer unlock()
	s, e = i.state(true)
	if e != nil {
		return nil, e
	}
	if e = i.verifyOwned(s); e != nil {
		return nil, e
	}
	head, e = cleanHeadContext(i.Context, i.Source)
	if e != nil {
		return nil, e
	}
	if e = i.allowedOrigin(); e != nil {
		return nil, e
	}
	if _, e = gitContext(i.Context, i.Source, "fetch", "--no-tags", "origin", "main"); e != nil {
		return nil, e
	}
	output, e := gitContext(i.Context, i.Source, "rev-parse", "FETCH_HEAD^{commit}")
	if e != nil {
		return nil, e
	}
	sha := strings.TrimSpace(string(output))
	if e = descendantContext(i.Context, i.Source, head, sha); e != nil {
		return nil, e
	}
	if e = descendantContext(i.Context, i.Source, text(s["release"]), sha); e != nil {
		return nil, e
	}
	release, m, e := i.stage(sha)
	if e != nil {
		return nil, e
	}
	plan, e := i.registrationPlan(s, m, false)
	if e != nil {
		return nil, e
	}
	if e = i.prepareRuntimeFor(sha); e != nil {
		return nil, e
	}
	if i.RuntimeCandidate != "" {
		if e = i.stageRuntime(); e != nil {
			return nil, e
		}
		if _, _, e = i.managerPlan(s); e != nil {
			return nil, e
		}
	}
	if _, _, e = i.prepareConfig(s, release, m, false); e != nil {
		return nil, e
	}
	runtime, e := i.prepareConsultationRuntimeFor(release)
	if e != nil {
		return nil, e
	}
	// Runtime preparation can take time while discovery roots remain mutable.
	// Reject newly occupied additions before advancing the source checkout.
	plan, e = i.registrationPlan(s, m, false)
	if e != nil {
		return nil, e
	}
	current, e := cleanHeadContext(i.Context, i.Source)
	if e != nil || current != head {
		return nil, errors.New("source HEAD changed during update")
	}
	if !i.NoCheckout && head != sha {
		if _, e = gitContext(i.Context, i.Source, "merge", "--ff-only", sha); e != nil {
			return nil, e
		}
	}
	ops, e := i.activation(s, sha, release, m, false, false)
	if e != nil {
		return nil, e
	}
	if len(ops) > 0 {
		if e = i.transact("update", ops); e != nil {
			return nil, e
		}
	}
	shared, e := SharedConfigDefaults(release)
	if e != nil {
		return nil, e
	}
	return Object{"command": "update", "dry_run": false, "release": sha, "registration_changes": plan.Changes, "shared_config": Object{"managed": shared != nil, "defaults": shared}, "consultation_runtime": runtime, "changed": len(ops) > 0 || runtime["changed"] == true}, nil
}
func (i *Installer) Rollback() (Object, error) {
	if err := i.Context.Err(); err != nil {
		return nil, err
	}
	s, e := i.state(true)
	if e != nil {
		return nil, e
	}
	if e = i.verifyOwned(s); e != nil {
		return nil, e
	}
	history := sequence(s["history"])
	if len(history) == 0 {
		return nil, errors.New("no previous release is available")
	}
	previous := object(history[len(history)-1])
	sha := text(previous["release"])
	release, m, e := i.release(sha)
	if e != nil {
		return nil, e
	}
	plan, e := i.registrationPlan(s, m, true)
	if e != nil {
		return nil, e
	}
	segment, e := decode(previous["global_segment"])
	if e != nil {
		return nil, e
	}
	template, e := os.ReadFile(filepath.Join(release, text(m["global_instructions"])))
	if e != nil {
		return nil, e
	}
	if !bytes.Equal(segment, managedBlock(template, segment[:len(segment)-len(bytes.TrimLeft(segment, "\n"))])) {
		return nil, errors.New("previous managed block does not match its validated release")
	}
	if !i.Apply {
		return Object{"command": "rollback", "dry_run": true, "release": sha, "registration_changes": plan.Changes}, nil
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
		return nil, errors.New("installation changed before rollback")
	}
	ops, e := i.activation(s, sha, release, m, false, true)
	if e != nil {
		return nil, e
	}
	if e = i.transact("rollback", ops); e != nil {
		return nil, e
	}
	return Object{"command": "rollback", "dry_run": false, "release": sha, "registration_changes": plan.Changes}, nil
}
func (i *Installer) Uninstall() (Object, error) {
	if err := i.Context.Err(); err != nil {
		return nil, err
	}
	s, e := i.state(true)
	if e != nil {
		return nil, e
	}
	if e = i.verifyOwned(s); e != nil {
		return nil, e
	}
	if !i.Apply {
		return Object{"command": "uninstall", "dry_run": true, "release": s["release"]}, nil
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
	ops := []Object{}
	for _, name := range keys(object(s["registrations"])) {
		item := object(object(s["registrations"])[name])
		original := object(item["original"])
		if original["kind"] == "personal" {
			origin, err := i.validatePersonalOrigin(name, original)
			if err != nil {
				return nil, err
			}
			ops = append(ops, personalOperation(name, origin, filepath.Join(i.skills, name), text(item["target"]), "managed", "personal", "restore-personal:"+name))
			continue
		}
		restored := original
		if original["kind"] == "legacy" {
			restored = Object{"kind": "absent"}
		}
		ops = append(ops, pathOperation(filepath.Join(i.skills, name), Object{"kind": "symlink", "target": item["target"]}, restored, "registration:"+name))
		if original["kind"] == "legacy" {
			destination := text(original["path"])
			if destination != filepath.Join(i.Home, ".codex", "skills", "typesafe-ai") || exists(destination) {
				return nil, errors.New("legacy restore destination is unsafe or occupied")
			}
			ops = append(ops, Object{"kind": "rename", "path": original["backup"], "destination": destination, "observation": original["observation"], "label": "legacy"})
		}
	}
	contents, e := i.readAgents()
	if e != nil {
		return nil, e
	}
	segment, e := decode(s["global_segment"])
	if e != nil {
		return nil, e
	}
	origin := object(s["global_origin"])
	var replacement []byte
	if origin["kind"] == "prefix" {
		replacement, e = decode(origin["prefix"])
		if e != nil {
			return nil, e
		}
	}
	after, e := replaceOwned(contents, segment, replacement)
	if e != nil {
		return nil, e
	}
	global, e := i.globalOperation(contents, after, segment, nil, "global", nil, replacement)
	if e != nil {
		return nil, e
	}
	if len(after) == 0 && origin["kind"] == "absent" {
		global["after"] = Object{"kind": "absent"}
	}
	ops = append(ops, global)
	if sharedConfigStateRequired(s) {
		change, e := ConfigRemoval(i.config, s)
		if e != nil {
			return nil, e
		}
		ops = append(ops, change)
	} else if meta := object(s["model_config"]); meta != nil {
		change, e := ModelRemoval(i.config, meta)
		if e != nil {
			return nil, e
		}
		ops = append(ops, change)
	}
	if meta := object(s["command_alias"]); meta != nil {
		change, e := AliasRemoval(meta, i.Paths)
		if e != nil {
			return nil, e
		}
		ops = append(ops, change)
	}
	if manager := object(s["manager"]); manager != nil {
		changes, err := i.managerRemoval(manager)
		if err != nil {
			return nil, err
		}
		ops = append(ops, changes...)
	}
	ops = append(ops, pathOperation(i.current, i.expectedPointer(s), Object{"kind": "absent"}, "pointer"), pathOperation(i.statePath, expectedState(s), Object{"kind": "absent"}, "state"))
	if e = i.transact("uninstall", ops); e != nil {
		return nil, e
	}
	return Object{"command": "uninstall", "dry_run": false, "restored_adoptions": true, "retained_release_cache": filepath.Join(i.State, "releases")}, nil
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
