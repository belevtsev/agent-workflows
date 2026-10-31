package workflow

import (
	"bytes"
	json "encoding/json/v2"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

func (i *Installer) perform(op Object) error {
	if err := i.Context.Err(); err != nil {
		return err
	}
	path := text(op["path"])
	if err := i.registrationAdditionCheck([]Object{op}); err != nil {
		return err
	}
	if path != "" {
		if err := realDirectory(filepath.Dir(path), false); err != nil {
			return err
		}
	}
	switch op["kind"] {
	case "personal_skill":
		return i.performPersonal(op)
	case "command_profile":
		data, mode, remove, err := i.renderProfile(op)
		if err != nil {
			return err
		}
		if remove {
			return writeObservation(path, Object{"kind": "absent"})
		}
		return atomicWrite(path, data, mode)
	case "manager_path":
		if err := i.validateManagerPath(op); err != nil {
			return err
		}
	case "model_config":
		if e := realDirectory(filepath.Dir(path), false); e != nil {
			return e
		}
		data, mode, remove, e := ModelRender(path, op)
		if e != nil {
			return e
		}
		if remove {
			return writeObservation(path, Object{"kind": "absent"})
		}
		return atomicWrite(path, data, mode)
	case sharedConfigOperationKind:
		if path != i.config {
			return errors.New("unsafe shared Codex config path")
		}
		data, mode, remove, err := ConfigRender(path, op)
		if err != nil {
			return err
		}
		if remove {
			return writeObservation(path, Object{"kind": "absent"})
		}
		return atomicWrite(path, data, mode)
	}
	current, e := observe(path)
	if e != nil {
		return e
	}
	if !equal(current, op["before"]) {
		return fmt.Errorf("path changed during mutation: %s", path)
	}
	return writeObservation(path, object(op["after"]))
}
func (i *Installer) transact(command string, ops []Object) error {
	if err := i.Context.Err(); err != nil {
		return err
	}
	if err := i.registrationAdditionCheck(ops); err != nil {
		return err
	}
	j := Object{"version": 2, "command": command, "home": i.Home, "codex_home": i.Codex, "state_dir": i.State, "operations": ops}
	var operations []any
	for _, op := range ops {
		operations = append(operations, op)
	}
	if err := i.validateMutationPaths(operations, "operations"); err != nil {
		return err
	}
	identity, found, err := i.recoveryRuntimeFor(ops)
	if err != nil {
		return err
	}
	if found {
		j["recovery_runtime"] = asObject(identity)
	}
	if e := atomicWrite(i.journalPath, legacyJSON(seal(j)), 0600); e != nil {
		return e
	}
	if e := fault("after-journal"); e != nil {
		return e
	}
	for _, op := range ops {
		if e := i.perform(op); e != nil {
			return e
		}
		if e := fault("after-" + text(op["label"])); e != nil {
			return e
		}
	}
	if e := os.Remove(i.journalPath); e != nil {
		return e
	}
	return syncDir(i.State)
}
func (i *Installer) loadJournal() (Object, error) {
	j, e := readJournalRecord(i.journalPath)
	if e != nil {
		return nil, e
	}
	if e = verifySeal(j); e != nil {
		return nil, e
	}
	if e = decodeJournal(j); e != nil {
		return nil, e
	}
	if integer(j["version"]) != 2 || j["home"] != i.Home || j["codex_home"] != i.Codex || j["state_dir"] != i.State || sequence(j["operations"]) == nil {
		return nil, errors.New("corrupt journal or roots do not match")
	}
	if raw, present := j["recovery_runtime"]; present {
		var identity RuntimeIdentity
		if err := json.Unmarshal(legacyJSON(raw), &identity, json.RejectUnknownMembers(true)); err != nil {
			return nil, err
		}
		if e = i.verifyRecoveryRuntime(identity); e != nil {
			return nil, e
		}
	}
	if recovery, present := j["recovery_operations"]; present {
		if _, ok := recovery.([]any); !ok {
			return nil, errors.New("corrupt recovery operations must be an array")
		}
	}
	for _, field := range []string{"operations", "recovery_operations"} {
		if e = i.validateMutationPaths(sequence(j[field]), field); e != nil {
			return nil, e
		}
	}
	all := append(append([]any{}, sequence(j["operations"])...), sequence(j["recovery_operations"])...)
	for _, raw := range all {
		op := object(raw)
		path := text(op["path"])
		if path == "" || Normalize(path) != path {
			return nil, errors.New("unsafe journal path")
		}
		if e = realDirectory(filepath.Dir(path), false); e != nil {
			return nil, e
		}
		kind := text(op["kind"])
		if (kind == "global" || kind == "global_recovery") && path != i.agents {
			return nil, errors.New("global journal operation requires AGENTS.md")
		}
		if kind == "path" && path == i.agents {
			return nil, errors.New("AGENTS.md requires a managed global operation")
		}
		switch kind {
		case "personal_skill":
			if _, e = i.validatePersonalOperation(op); e != nil {
				return nil, e
			}
			continue
		case "manager_path":
			if e = i.validateManagerPath(op); e != nil {
				return nil, e
			}
			continue
		case "command_profile":
			if e = i.validateProfileOperation(op); e != nil {
				return nil, e
			}
			continue
		case "model_config":
			if path != i.config {
				return nil, errors.New("unsafe model config path in journal")
			}
			if e = ModelValidateOperation(op); e != nil {
				return nil, e
			}
			continue
		case sharedConfigOperationKind:
			if path != i.config {
				return nil, errors.New("unsafe shared Codex config path in journal")
			}
			if e = ConfigValidateOperation(op); e != nil {
				return nil, e
			}
			continue
		case "path":
			if !exactFields(op, "kind", "path", "before", "after", "label") {
				return nil, errors.New("invalid path journal fields")
			}
		case "global":
			if !exactFields(op, "kind", "path", "before", "after", "before_segment", "after_segment", "label") {
				return nil, errors.New("invalid global journal fields")
			}
		case "global_recovery":
			if !exactFields(op, "kind", "path", "before", "after", "source_segment", "target_segment", "restoration", "label") {
				return nil, errors.New("invalid global recovery fields")
			}
		default:
			return nil, errors.New("unsafe operation kind in journal")
		}
		if path == i.config {
			return nil, errors.New("model config requires keyed journal operation")
		}
		if path != i.current && path != i.statePath && path != i.agents && !(filepath.Dir(path) == i.skills && registrationName(filepath.Base(path))) {
			return nil, errors.New("unsafe path in mutation journal")
		}
		for _, side := range []string{"before", "after"} {
			ob := object(op[side])
			if e = validateObservation(ob, false); e != nil {
				return nil, e
			}
			if path == i.current && ob["kind"] != "absent" && ob["kind"] != "symlink" {
				return nil, errors.New("unsafe pointer observation")
			}
			if path == i.statePath && ob["kind"] != "absent" && ob["kind"] != "file" {
				return nil, errors.New("unsafe state observation")
			}
			if path == i.agents && ob["kind"] != "absent" && ob["kind"] != "file" {
				return nil, errors.New("unsafe global observation")
			}
			if filepath.Dir(path) == i.skills && ob["kind"] != "absent" && ob["kind"] != "symlink" {
				return nil, errors.New("unsafe registration observation")
			}
			if path == i.current && ob["kind"] == "symlink" {
				release := text(ob["target"])
				if filepath.Dir(release) != filepath.Join(i.State, "releases") || !commitSHA.MatchString(filepath.Base(release)) {
					return nil, errors.New("unsafe release pointer in journal")
				}
				if _, _, e = i.release(filepath.Base(release)); e != nil {
					return nil, e
				}
			}
			if path == i.statePath && ob["kind"] == "file" {
				data, e := decode(ob["data"])
				if e != nil {
					return nil, e
				}
				var s Object
				if e = json.Unmarshal(data, &s); e != nil {
					return nil, e
				}
				if e = verifySeal(s); e != nil {
					return nil, e
				}
				if e = i.validateStateStructure(s); e != nil {
					return nil, e
				}
				if s["home"] != i.Home || s["codex_home"] != i.Codex || s["state_dir"] != i.State {
					return nil, errors.New("journal state roots mismatch")
				}
				if meta := object(s["model_config"]); meta != nil {
					if e = ModelValidateMetadata(meta); e != nil {
						return nil, e
					}
				}
				if _, _, e = i.release(text(s["release"])); e != nil {
					return nil, e
				}
				for _, raw := range sequence(s["history"]) {
					if _, _, e = i.release(text(object(raw)["release"])); e != nil {
						return nil, e
					}
				}
			}
		}
	}
	return j, nil
}
func (i *Installer) restoreRemovedGlobal(op, current Object) (Object, error) {
	if current["kind"] != "absent" && current["kind"] != "file" {
		return nil, errors.New("AGENTS.md ownership changed during removal recovery")
	}
	contents := []byte{}
	var e error
	if current["kind"] == "file" {
		contents, e = decode(current["data"])
		if e != nil {
			return nil, e
		}
	}
	if hasMarkers(contents) {
		return nil, errors.New("unexpected managed markers during removal recovery")
	}
	segment, e := decode(op["before_segment"])
	if e != nil {
		return nil, e
	}
	original, e := decode(object(op["before"])["data"])
	if e != nil {
		return nil, e
	}
	start, _, e := locateBlock(original)
	if e != nil {
		return nil, e
	}
	leading := len(segment) - len(bytes.TrimLeft(segment, "\n"))
	if start < leading {
		return nil, errors.New("invalid global segment")
	}
	prefix := original[:start-leading]
	pos := 0
	if bytes.HasPrefix(contents, prefix) {
		pos = len(prefix)
	}
	out := append(bytes.Clone(contents[:pos]), segment...)
	contents = append(out, contents[pos:]...)
	mode := current["mode"]
	if mode == nil {
		mode = object(op["before"])["mode"]
	}
	if mode == nil {
		mode = 0600
	}
	return Object{"kind": "file", "data": encode(contents), "mode": mode}, nil
}
func (i *Installer) recoveryPlan(j Object) ([]Object, error) {
	ops := sequence(j["operations"])
	reversals := []Object{}
	for index := len(ops) - 1; index >= 0; index-- {
		op := object(ops[index])
		path := text(op["path"])
		switch op["kind"] {
		case "personal_skill":
			change, err := i.personalReversal(op)
			if err != nil {
				return nil, err
			}
			if change != nil {
				reversals = append(reversals, change)
			}
			continue
		case "command_profile":
			change, err := i.profileReversal(op)
			if err != nil {
				return nil, err
			}
			if change != nil {
				reversals = append(reversals, change)
			}
			continue
		case "model_config":
			rev, e := ModelReversal(path, op)
			if e != nil {
				return nil, e
			}
			if rev != nil {
				reversals = append(reversals, rev)
			}
			continue
		case sharedConfigOperationKind:
			rev, err := ConfigReversal(path, op)
			if err != nil {
				return nil, err
			}
			if rev != nil {
				reversals = append(reversals, rev)
			}
			continue
		}
		current, e := observe(path)
		if e != nil {
			return nil, e
		}
		if equal(current, op["before"]) {
			continue
		}
		if op["kind"] == "global" && op["after_segment"] == nil {
			beforeSegment, e := decode(op["before_segment"])
			if e != nil {
				return nil, e
			}
			if current["kind"] == "file" {
				contents, e := decode(current["data"])
				if e != nil {
					return nil, e
				}
				if hasMarkers(contents) {
					if _, e = replaceOwned(contents, beforeSegment, nil); e != nil {
						return nil, e
					}
					continue
				}
			}
			target, e := i.restoreRemovedGlobal(op, current)
			if e != nil {
				return nil, e
			}
			rev := pathOperation(path, current, target, "recover-global")
			rev["kind"] = "global_recovery"
			rev["source_segment"] = nil
			rev["target_segment"] = op["before_segment"]
			rev["restoration"] = op
			reversals = append(reversals, rev)
			continue
		}
		if op["kind"] == "global" && op["after_segment"] != nil && current["kind"] == "file" {
			afterSegment, e := decode(op["after_segment"])
			if e != nil {
				return nil, e
			}
			var beforeSegment []byte
			if op["before_segment"] != nil {
				beforeSegment, e = decode(op["before_segment"])
				if e != nil {
					return nil, e
				}
			}
			contents, e := decode(current["data"])
			if e != nil {
				return nil, e
			}
			restored, e := replaceOwned(contents, afterSegment, beforeSegment)
			if e != nil {
				if beforeSegment != nil {
					if _, beforeErr := replaceOwned(contents, beforeSegment, nil); beforeErr == nil {
						continue
					}
				}
				return nil, e
			}
			mode := object(op["before"])["mode"]
			if mode == nil {
				mode = current["mode"]
			}
			target := Object{"kind": "file", "data": encode(restored), "mode": mode}
			if len(restored) == 0 && object(op["before"])["kind"] == "absent" {
				target = Object{"kind": "absent"}
			}
			rev := pathOperation(path, current, target, "recover-global")
			rev["kind"] = "global_recovery"
			rev["source_segment"] = op["after_segment"]
			rev["target_segment"] = op["before_segment"]
			rev["restoration"] = op
			reversals = append(reversals, rev)
		} else if equal(current, op["after"]) {
			reversal := pathOperation(path, current, object(op["before"]), "recover-"+text(op["label"]))
			if op["kind"] == "manager_path" {
				reversal["kind"] = "manager_path"
			}
			reversals = append(reversals, reversal)
		} else {
			return nil, fmt.Errorf("ownership changed at %s; recovery refuses overwrite", path)
		}
	}
	return reversals, nil
}
func (i *Installer) pendingRecovery(j Object) ([]Object, error) {
	if j["recovery_operations"] == nil {
		return i.recoveryPlan(j)
	}
	pending := []Object{}
	for _, raw := range sequence(j["recovery_operations"]) {
		op := object(raw)
		path := text(op["path"])
		switch op["kind"] {
		case "personal_skill":
			change, err := i.personalPending(op)
			if err != nil {
				return nil, err
			}
			if change != nil {
				pending = append(pending, change)
			}
			continue
		case "command_profile":
			change, err := i.profilePending(op)
			if err != nil {
				return nil, err
			}
			if change != nil {
				pending = append(pending, change)
			}
			continue
		case "model_config":
			change, e := ModelPending(path, op)
			if e != nil {
				return nil, e
			}
			if change != nil {
				pending = append(pending, change)
			}
			continue
		case sharedConfigOperationKind:
			change, err := ConfigPending(path, op)
			if err != nil {
				return nil, err
			}
			if change != nil {
				pending = append(pending, change)
			}
			continue
		}
		current, e := observe(path)
		if e != nil {
			return nil, e
		}
		switch op["kind"] {
		case "global_recovery":
			if current["kind"] != "file" && current["kind"] != "absent" {
				return nil, errors.New("AGENTS.md ownership changed during interrupted recovery")
			}
			contents := []byte{}
			if current["kind"] == "file" {
				contents, e = decode(current["data"])
				if e != nil {
					return nil, e
				}
			}
			var source, target []byte
			if op["source_segment"] != nil {
				source, e = decode(op["source_segment"])
				if e != nil {
					return nil, e
				}
			}
			if op["target_segment"] != nil {
				target, e = decode(op["target_segment"])
				if e != nil {
					return nil, e
				}
			}
			var after Object
			if hasMarkers(contents) {
				if target != nil {
					if _, check := replaceOwned(contents, target, nil); check == nil {
						continue
					}
				}
				if source == nil {
					return nil, errors.New("owned block changed during interrupted recovery")
				}
				result, e := replaceOwned(contents, source, target)
				if e != nil {
					return nil, e
				}
				after = Object{"kind": "file", "data": encode(result), "mode": current["mode"]}
				if len(result) == 0 && object(op["after"])["kind"] == "absent" {
					after = Object{"kind": "absent"}
				}
			} else if target == nil {
				continue
			} else if source == nil {
				after, e = i.restoreRemovedGlobal(object(op["restoration"]), current)
				if e != nil {
					return nil, e
				}
			} else {
				return nil, errors.New("owned block disappeared during interrupted recovery")
			}
			change := clone(op)
			change["before"] = current
			change["after"] = after
			pending = append(pending, change)
		default:
			if equal(current, op["before"]) {
				pending = append(pending, op)
			} else if !equal(current, op["after"]) {
				return nil, fmt.Errorf("ownership changed during interrupted recovery: %s", path)
			}
		}
	}
	return pending, nil
}
func (i *Installer) Recover() (Object, error) {
	if err := i.Context.Err(); err != nil {
		return nil, err
	}
	if !exists(i.journalPath) {
		return Object{"command": "recover", "pending": false, "dry_run": !i.Apply}, nil
	}
	j, e := i.loadJournal()
	if e != nil {
		return nil, e
	}
	reversals, e := i.pendingRecovery(j)
	if e != nil {
		return nil, e
	}
	if !i.Apply {
		return Object{"command": "recover", "pending": true, "dry_run": true, "reversals": len(reversals)}, nil
	}
	unlock, e := lockRoot(i.State, true)
	if e != nil {
		return nil, e
	}
	defer unlock()
	j, e = i.loadJournal()
	if e != nil {
		return nil, e
	}
	reversals, e = i.pendingRecovery(j)
	if e != nil {
		return nil, e
	}
	if j["recovery_operations"] == nil {
		j["recovery_operations"] = reversals
		if e = atomicWrite(i.journalPath, legacyJSON(seal(j)), 0600); e != nil {
			return nil, e
		}
	}
	for _, op := range reversals {
		if e = i.perform(op); e != nil {
			return nil, e
		}
		if e = fault("after-" + text(op["label"])); e != nil {
			return nil, e
		}
	}
	if e = os.Remove(i.journalPath); e != nil {
		return nil, e
	}
	if e = syncDir(i.State); e != nil {
		return nil, e
	}
	return Object{"command": "recover", "pending": false, "dry_run": false, "recovered": j["command"]}, nil
}
