package workflow

import (
	"bytes"
	"context"
	json "encoding/json/v2"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"time"

	"github.com/belevtsev/agent-workflows/internal/bundled"
	"github.com/belevtsev/agent-workflows/internal/distribution"
)

type standaloneGuard struct {
	files      map[string]Object
	identities map[string]os.FileInfo
	anchors    map[string]os.FileInfo
}

func (i *Installer) captureStandaloneGuard(manifest Object) (standaloneGuard, error) {
	g := standaloneGuard{files: map[string]Object{}, identities: map[string]os.FileInfo{}, anchors: map[string]os.FileInfo{}}
	for _, root := range []string{i.Home, i.State, i.Codex, i.Claude, i.ConfigDir} {
		if root == "" {
			continue
		}
		if err := realDirectory(root, false); err != nil {
			return g, err
		}
		anchor := root
		for {
			info, err := os.Lstat(anchor)
			if err == nil {
				g.anchors[anchor] = info
				break
			}
			if !errors.Is(err, os.ErrNotExist) || anchor == filepath.Dir(anchor) {
				return g, err
			}
			anchor = filepath.Dir(anchor)
		}
	}
	paths := []string{i.statePath, i.current, i.locatorPath(), i.runtimeCurrent(), i.commandPath(), i.preferencesPath()}
	for _, host := range i.knownHosts() {
		paths = append(paths, host.instructions, host.settings)
		for name := range object(manifest["registrations"]) {
			paths = append(paths, filepath.Join(host.skills, name))
		}
	}
	if i.RuntimeCandidate != "" {
		paths = append(paths, i.RuntimeCandidate)
	}
	for _, path := range paths {
		ob, err := observe(path)
		if err != nil {
			return g, err
		}
		g.files[path] = ob
		if ob["kind"] != "absent" {
			info, err := os.Lstat(path)
			if err != nil {
				return g, err
			}
			g.identities[path] = info
		}
	}
	return g, nil
}

func (g standaloneGuard) verify() error {
	for path, before := range g.anchors {
		after, err := os.Lstat(path)
		if err != nil || !os.SameFile(before, after) {
			return errors.New("an installation root changed during configuration")
		}
	}
	for path, before := range g.identities {
		after, err := os.Lstat(path)
		if err != nil || !os.SameFile(before, after) {
			return fmt.Errorf("configuration target identity changed before apply: %s", path)
		}
	}
	for path, before := range g.files {
		if err := realDirectory(filepath.Dir(path), false); err != nil {
			return err
		}
		after, err := observe(path)
		if err != nil {
			return err
		}
		if !equal(before, after) {
			return fmt.Errorf("configuration target changed before apply: %s", path)
		}
	}
	return nil
}

func (i *Installer) standaloneState(required bool) (Object, error) {
	if err := i.Context.Err(); err != nil {
		return nil, err
	}
	if err := legacyRecords(i.State); err != nil {
		return nil, err
	}
	// Ownership records keep an empty source field; a checkout source would
	// write state that every later read refuses as legacy.
	if i.Source != "" {
		return nil, errors.New("source checkout installations are no longer supported")
	}
	if exists(i.journalPath) {
		return nil, errors.New("an unfinished mutation requires explicit recover before configuration")
	}
	state, err := i.state(required)
	if err != nil {
		return nil, err
	}
	if i.ExpectedGeneration != nil && text(state["integrity_sha256"]) != *i.ExpectedGeneration {
		return nil, errors.New("installation generation differs from prepared handoff")
	}
	if state != nil {
		if err = i.verifyOwned(state); err != nil {
			return nil, err
		}
	}
	recorded := text(state["config_dir"])
	i.ConfigDir, err = ResolveConfigDir(i.Home, i.ConfigDir, recorded, i.Getenv)
	if err != nil {
		return nil, err
	}
	if err = i.validatePrivateDestinations(); err != nil {
		return nil, err
	}
	return state, nil
}

func (i *Installer) standaloneOrder(state Object, target bundled.Identity) error {
	if err := distribution.CheckOrder(target, target); err != nil {
		return err
	}
	// Only a manager referenced by committed ownership establishes version
	// authority. Prepared runtimes can remain after cancellation; rollback
	// retains this manager, so it remains the installation's high-water mark.
	manager := object(state["manager"])
	if manager == nil {
		return nil
	}
	var owned ManagerRecord
	if err := json.Unmarshal(legacyJSON(manager), &owned, json.RejectUnknownMembers(true)); err != nil {
		return err
	}
	receipt, err := i.verifiedRuntimeReceipt(managerRuntimeKey(owned))
	if err != nil {
		return err
	}
	if receipt.Version != 3 || receipt.ManagerProtocol != ManagerProtocol || receipt.Identity != owned.Runtime || receipt.BaseDigest != owned.BaseDigest {
		return errors.New("installed capable manager differs from its receipt")
	}
	high := bundled.Identity{Version: receipt.Identity.Version, Revision: receipt.Identity.Revision, OS: receipt.Identity.OS, Arch: receipt.Identity.Arch, Digest: receipt.BaseDigest}
	if err := distribution.CheckOrder(target, high); err != nil {
		return err
	}
	if high.Version == target.Version && i.RuntimeCandidate != "" {
		data, err := os.ReadFile(i.RuntimeCandidate)
		if err != nil {
			return err
		}
		if hash(data) != receipt.SHA256 {
			return errors.New("equal-version manager has conflicting immutable content")
		}
	}
	return nil
}

func (i *Installer) inspectStandaloneCandidate() error {
	id := i.bundleIdentity
	if i.RuntimeCandidate == "" {
		return errors.New("standalone activation requires its verified manager executable")
	}
	if i.RuntimeIdentity != (RuntimeIdentity{Revision: id.Revision, Version: id.Version, OS: id.OS, Arch: id.Arch}) {
		return errors.New("standalone manager and embedded base identities differ")
	}
	protocol, err := i.inspectRuntimeProtocol(i.RuntimeCandidate)
	if err != nil {
		return err
	}
	if protocol != ManagerProtocol {
		return errors.New("standalone manager lacks the required capability")
	}
	ctx, cancel := context.WithTimeout(i.Context, 5*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, i.RuntimeCandidate, "--bundle-identity")
	var out bytes.Buffer
	command.Stdout = &boundedProtocolWriter{buffer: &out}
	if err = command.Run(); err != nil {
		return fmt.Errorf("inspect embedded manager identity: %w", err)
	}
	var actual bundled.Identity
	if err = json.Unmarshal(out.Bytes(), &actual, json.RejectUnknownMembers(true)); err != nil {
		return err
	}
	if actual != id {
		return errors.New("manager executable contains a different embedded base")
	}
	i.runtimeProtocol = protocol
	return nil
}

func (i *Installer) standaloneApply(command string, enroll bool) (Object, error) {
	state, err := i.standaloneState(command == "update")
	if err != nil {
		return nil, err
	}
	if i.Bundle == nil {
		return nil, errors.New("this installer has no embedded workflow payload")
	}
	if i.Selection != nil && i.Configure != nil {
		return nil, errors.New("explicit selection and configuration callback are mutually exclusive")
	}
	cached := cachedStandaloneConfiguration(command, state)
	actor := i
	var id bundled.Identity
	var base string
	var manifest Object
	if cached {
		// Setup configures the selected installation, including after rollback.
		// Its executing manager's bundled release is not an activation request.
		id, base, manifest, err = i.activeStandaloneBase(state)
		if err != nil {
			return nil, err
		}
		actor = i.reconfigurationInstaller()
	} else {
		bundle, loadErr := i.Bundle(i.Context)
		if loadErr != nil {
			return nil, loadErr
		}
		if bundle == nil {
			return nil, errors.New("standalone bundle is unavailable")
		}
		id = bundle.Identity()
		if !validBaseIdentity(id) {
			return nil, errors.New("invalid embedded base identity")
		}
		if i.ExpectedBundle != nil && *i.ExpectedBundle != id {
			return nil, errors.New("embedded base differs from prepared target")
		}
		i.bundleIdentity = id
		if err = i.standaloneOrder(state, id); err != nil {
			return nil, err
		}
		staging, stageErr := os.MkdirTemp("", "agent-workflows-base-")
		if stageErr != nil {
			return nil, stageErr
		}
		defer os.RemoveAll(staging)
		staging = Normalize(staging)
		base = filepath.Join(staging, "payload")
		if err = bundle.Extract(base); err != nil {
			return nil, err
		}
		manifest, err = LoadManifestValidated(base)
		if err != nil {
			return nil, err
		}
	}
	prefs, beforePrefs, err := i.readPreferences()
	if err != nil {
		return nil, err
	}
	view, err := i.configurationView(command, base, manifest, state, prefs, id)
	if err != nil {
		return nil, err
	}
	savedPreferences := legacyJSON(prefs)
	guard, err := actor.captureStandaloneGuard(manifest)
	if err != nil {
		return nil, err
	}
	selection := ConfigurationSelection{}
	if i.Selection != nil {
		selection = *i.Selection
	}
	if i.Configure != nil {
		selection, err = i.Configure(i.Context, view)
		if err != nil {
			return nil, err
		}
	}
	// The callback receives mutable Go collections for UI convenience; rebuild
	// the authority from the pristine base before validating its selection.
	pristinePreferences, err := DecodePreferences(savedPreferences)
	if err != nil {
		return nil, err
	}
	view, err = i.configurationView(command, base, manifest, state, pristinePreferences, id)
	if err != nil {
		return nil, err
	}
	if err = i.Context.Err(); err != nil {
		return nil, err
	}
	prefs, err = PreferencesForSelection(view, selection)
	if err != nil {
		return nil, err
	}
	if len(selection.Hosts) > 0 {
		i.Hosts = slices.Clone(selection.Hosts)
		actor.Hosts = slices.Clone(selection.Hosts)
	}
	hosts, err := i.selectedHosts(state)
	if err != nil {
		return nil, err
	}
	// Setup, install and update all refuse a home that a skill registration
	// of the release they would activate needs.
	if err = i.checkHomesOffRegistrations(hosts, object(manifest["registrations"])); err != nil {
		return nil, err
	}
	var effective, name string
	var receipt Object
	if cached && equal(prefs, pristinePreferences) {
		// Reuse the existing receipted bytes, not a recompilation whose encoding
		// could change across manager versions while preferences stay identical.
		name = text(state["release"])
		effective, manifest, err = actor.release(name)
		if err != nil {
			return nil, err
		}
		receipt, err = readJSON(effective + ".receipt.json")
		if err != nil {
			return nil, err
		}
	} else {
		effective, err = os.MkdirTemp("", "agent-workflows-effective-")
		if err != nil {
			return nil, err
		}
		defer os.RemoveAll(effective)
		effective = Normalize(effective)
		if err = copySnapshot(base, effective); err != nil {
			return nil, err
		}
		if err = compileEffective(effective, manifest, view, prefs); err != nil {
			return nil, err
		}
		name, receipt, err = effectiveReceipt(effective, id, prefs)
		if err != nil {
			return nil, err
		}
	}
	if i.Apply && !cached {
		if err = i.inspectStandaloneCandidate(); err != nil {
			return nil, err
		}
	}
	if err = guard.verify(); err != nil {
		return nil, err
	}
	plan := func(root string, m Object) ([]Object, error) {
		ops, err := actor.standaloneOperations(state, name, root, m, id, prefs, beforePrefs, enroll, false)
		if err != nil {
			return nil, err
		}
		if cached {
			return actor.retainStandaloneActivation(ops, state)
		}
		return ops, nil
	}
	ops, err := plan(effective, manifest)
	if err != nil {
		return nil, err
	}
	if err = actor.preflightMutationPaths(ops, state, !cached); err != nil {
		return nil, err
	}
	report := Object{"command": command, "dry_run": !i.Apply, "changed": len(ops) > 0, "configuration_committed": false, "preferences_applied": false, "config_dir": i.ConfigDir, "base_identity": asObject(id), "effective_digest": receipt["effective_digest"], "release": name}
	if !i.Apply {
		return report, nil
	}
	unlock, err := lockRoot(i.State, false)
	if err != nil {
		return nil, err
	}
	defer unlock()
	if err = guard.verify(); err != nil {
		return nil, err
	}
	current, err := i.state(false)
	if err != nil {
		return nil, err
	}
	if !equal(state, current) {
		return nil, errors.New("installation changed before standalone apply")
	}
	_, currentPreferences, err := i.readPreferences()
	if err != nil {
		return nil, err
	}
	if !equal(beforePrefs, currentPreferences) {
		return nil, errors.New("preferences changed before standalone apply")
	}
	if !cached {
		if err = i.standaloneOrder(state, id); err != nil {
			return nil, err
		}
	}
	if err = privateDirectory(i.ConfigDir, true); err != nil {
		return nil, err
	}
	if !cached {
		if err = i.stageBase(base, id); err != nil {
			return nil, err
		}
		if err = i.stageRuntime(); err != nil {
			return nil, err
		}
	}
	installed, installedManifest := effective, manifest
	if !cached || name != text(state["release"]) {
		installed, installedManifest, err = actor.stageEffective(effective, name, receipt)
		if err != nil {
			return nil, err
		}
	}
	ops, err = plan(installed, installedManifest)
	if err != nil {
		return nil, err
	}
	if err = actor.preflightMutationPaths(ops, state, !cached); err != nil {
		return nil, err
	}
	if len(ops) > 0 {
		if err = actor.transact(command, ops); err != nil {
			return nil, err
		}
	}
	report["configuration_committed"], report["preferences_applied"] = true, true
	after, err := i.state(true)
	if err != nil {
		return nil, err
	}
	report["generation"] = after["integrity_sha256"]
	return report, nil
}

func (i *Installer) standaloneOperations(state Object, name, root string, manifest Object, id bundled.Identity, prefs Preferences, beforePrefs Object, enroll, rollback bool) ([]Object, error) {
	var ops []Object
	var after Object
	if state == nil {
		hosts, changes, err := i.addHosts(nil, root, manifest, enroll)
		if err != nil {
			return nil, err
		}
		after = i.newState(name, manifest, hosts)
		ops = append(ops, pathOperation(i.current, Object{"kind": "absent"}, Object{"kind": "symlink", "target": filepath.Join(i.State, "releases", name)}, "pointer"))
		ops = append(ops, changes...)
	} else {
		planner := *i
		planner.RuntimeCandidate = ""
		changes, err := planner.activation(state, name, root, manifest, enroll, rollback)
		if err != nil {
			return nil, err
		}
		after = clone(state)
		for _, op := range changes {
			if op["path"] == i.statePath {
				after, err = stateFromObservation(object(op["after"]))
				if err != nil {
					return nil, err
				}
			} else {
				ops = append(ops, op)
			}
		}
	}
	history := []any{}
	for _, raw := range sequence(after["history"]) {
		previous := clone(object(raw))
		if object(previous["base"]) == nil && state != nil && previous["release"] == state["release"] {
			previous["base"] = state["base"]
		}
		history = append(history, previous)
	}
	// A v4 activation appended by the shared planner needs the old base identity.
	if state != nil && !rollback && name != state["release"] && len(history) > 0 {
		object(history[len(history)-1])["base"] = state["base"]
	}
	if rollback {
		history = append([]any{}, sequence(state["history"])[:len(sequence(state["history"]))-1]...)
	}
	after["version"], after["release"], after["base"], after["config_dir"], after["history"] = 4, name, asObject(id), i.ConfigDir, history
	after["source"] = i.Source
	after["preferences_sha256"] = hash(legacyJSON(prefs))
	if !rollback {
		manager, changes, err := i.managerPlan(state)
		if err != nil {
			return nil, err
		}
		if manager != nil {
			after["manager"] = manager
		}
		ops = append(ops, changes...)
	}
	prefsAfter := Object{"kind": "file", "data": encode(legacyJSON(prefs)), "mode": 0600}
	if !equal(beforePrefs, prefsAfter) {
		ops = append([]Object{pathOperation(i.preferencesPath(), beforePrefs, prefsAfter, "preferences")}, ops...)
	}
	after = seal(after)
	if !equal(state, after) {
		before := Object{"kind": "absent"}
		if state != nil {
			before = expectedState(state)
		}
		ops = append(ops, pathOperation(i.statePath, before, expectedState(after), "state"))
	}
	return ops, nil
}

func (i *Installer) standaloneRollback() (Object, error) {
	state, err := i.standaloneState(true)
	if err != nil {
		return nil, err
	}
	history := sequence(state["history"])
	if len(history) == 0 {
		return nil, errors.New("no previous release is available")
	}
	previous := object(history[len(history)-1])
	var id bundled.Identity
	if err = json.Unmarshal(legacyJSON(previous["base"]), &id, json.RejectUnknownMembers(true)); err != nil {
		return nil, err
	}
	base, manifest, err := i.baseRelease(id)
	if err != nil {
		return nil, err
	}
	prefs, beforePrefs, err := i.readPreferences()
	if err != nil {
		return nil, err
	}
	view, err := i.configurationView("rollback", base, manifest, state, prefs, id)
	if err != nil {
		return nil, err
	}
	prefs, err = PreferencesForSelection(view, ConfigurationSelection{})
	if err != nil {
		return nil, err
	}
	guard, err := i.captureStandaloneGuard(manifest)
	if err != nil {
		return nil, err
	}
	effective, err := os.MkdirTemp("", "agent-workflows-rollback-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(effective)
	effective = Normalize(effective)
	if err = copySnapshot(base, effective); err != nil {
		return nil, err
	}
	if err = compileEffective(effective, manifest, view, prefs); err != nil {
		return nil, err
	}
	name, receipt, err := effectiveReceipt(effective, id, prefs)
	if err != nil {
		return nil, err
	}
	ops, err := i.standaloneOperations(state, name, effective, manifest, id, prefs, beforePrefs, false, true)
	if err != nil {
		return nil, err
	}
	// Rollback consumes a history entry even when current overrides produce the
	// same effective snapshot as before.
	for _, op := range ops {
		if op["path"] == i.statePath {
			after, e := stateFromObservation(object(op["after"]))
			if e != nil {
				return nil, e
			}
			after["history"] = history[:len(history)-1]
			op["after"] = expectedState(seal(after))
		}
	}
	if err = i.preflightMutationPaths(ops, state, false); err != nil {
		return nil, err
	}
	report := Object{"command": "rollback", "dry_run": !i.Apply, "release": name, "base_identity": asObject(id), "effective_digest": receipt["effective_digest"], "config_dir": i.ConfigDir, "configuration_committed": false}
	if !i.Apply {
		return report, nil
	}
	unlock, err := lockRoot(i.State, false)
	if err != nil {
		return nil, err
	}
	defer unlock()
	if err = guard.verify(); err != nil {
		return nil, err
	}
	if _, _, err = i.stageEffective(effective, name, receipt); err != nil {
		return nil, err
	}
	// Rebuild against the actual immutable path; no Git or network is involved.
	ops, err = i.standaloneOperations(state, name, filepath.Join(i.State, "releases", name), manifest, id, prefs, beforePrefs, false, true)
	if err != nil {
		return nil, err
	}
	for _, op := range ops {
		if op["path"] == i.statePath {
			after, e := stateFromObservation(object(op["after"]))
			if e != nil {
				return nil, e
			}
			after["history"] = history[:len(history)-1]
			op["after"] = expectedState(seal(after))
		}
	}
	if err = i.transact("rollback", ops); err != nil {
		return nil, err
	}
	report["configuration_committed"] = true
	return report, nil
}
