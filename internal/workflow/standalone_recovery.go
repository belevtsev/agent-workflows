package workflow

import (
	"errors"
	"os"
	"path/filepath"
)

func (i *Installer) validatePreferenceObservation(ob Object) error {
	if err := validateObservation(ob, false); err != nil {
		return err
	}
	if ob["kind"] == "absent" {
		return nil
	}
	if ob["kind"] != "file" || integer(ob["mode"]) != 0600 {
		return errors.New("unsafe preference observation in journal")
	}
	data, err := decode(ob["data"])
	if err != nil {
		return err
	}
	_, err = DecodePreferences(data)
	return err
}

func (i *Installer) recoveryRuntimeKey(identity RuntimeIdentity) (string, error) {
	parent := filepath.Join(i.State, "runtime", "releases")
	entries, err := os.ReadDir(parent)
	if err != nil {
		return "", err
	}
	match := ""
	for _, entry := range entries {
		key := entry.Name()
		if runtimeKeyRevision(key) != identity.Revision {
			continue
		}
		raw, err := readJSON(filepath.Join(parent, key, "receipt.json"))
		if err != nil {
			return "", err
		}
		if !equal(raw["identity"], asObject(identity)) {
			continue
		}
		receipt, err := i.verifiedRuntimeReceipt(key)
		if err != nil {
			return "", err
		}
		if receipt.Identity == identity {
			if match != "" {
				return "", errors.New("ambiguous retained recovery runtime")
			}
			match = key
		}
	}
	if match == "" {
		return "", errors.New("missing retained recovery runtime")
	}
	return match, nil
}
