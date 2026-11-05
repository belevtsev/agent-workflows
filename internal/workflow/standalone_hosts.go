package workflow

import (
	json "encoding/json/v2"
	"errors"

	"github.com/belevtsev/agent-workflows/internal/bundled"
)

func cachedStandaloneConfiguration(command string, state Object) bool {
	return state != nil && (command == "setup" || command == "install")
}

func (i *Installer) activeStandaloneBase(state Object) (bundled.Identity, string, Object, error) {
	var id bundled.Identity
	if err := json.Unmarshal(legacyJSON(state["base"]), &id, json.RejectUnknownMembers(true)); err != nil {
		return id, "", nil, err
	}
	base, manifest, err := i.baseRelease(id)
	return id, base, manifest, err
}

// Reconfiguration owns host and preference changes. The manager that owns the
// installation remains authoritative even when a different executable runs it.
func (i *Installer) reconfigurationInstaller() *Installer {
	planner := *i
	planner.RuntimeCandidate = ""
	planner.RuntimeIdentity = RuntimeIdentity{}
	planner.bundleIdentity = bundled.Identity{}
	planner.runtimeProtocol = ""
	return &planner
}

// Host enrollment and preference edits do not activate a new base release.
// Keep its activation history and manager exactly, including omitted fields.
func (i *Installer) retainStandaloneActivation(ops []Object, before Object) ([]Object, error) {
	result := make([]Object, 0, len(ops))
	for _, op := range ops {
		if op["path"] != i.statePath {
			result = append(result, op)
			continue
		}
		after, err := stateFromObservation(object(op["after"]))
		if err != nil {
			return nil, err
		}
		if after == nil {
			return nil, errors.New("reconfiguration must retain installation ownership")
		}
		after["base"], after["history"], after["manager"] = before["base"], before["history"], before["manager"]
		if source, present := before["source"]; present {
			after["source"] = source
		} else {
			delete(after, "source")
		}
		after = seal(after)
		if !equal(after, before) {
			op["after"] = expectedState(after)
			result = append(result, op)
		}
	}
	return result, nil
}
