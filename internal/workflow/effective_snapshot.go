package workflow

import (
	json "encoding/json/v2"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"github.com/belevtsev/agent-workflows/internal/bundled"
	"github.com/pelletier/go-toml/v2"
	"gopkg.in/yaml.v3"
)

var digestPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
var effectivePattern = regexp.MustCompile(`^effective-[0-9a-f]{64}$`)
var standaloneRuntimePattern = regexp.MustCompile(`^[0-9a-f]{40}-[0-9a-f]{64}$`)

// validatedSnapshots remembers immutable snapshots that passed suite
// validation, keyed by root and the digest of the inventory the caller just
// compared with the snapshot's receipt. A command reads the same snapshots
// several times, and suite validation is the costliest part of each read.
// Any changed entry, mode or link target changes the digest, so changed bytes
// are always validated again. Failures, and validations during which the
// snapshot changed, are never remembered.
var validatedSnapshots = struct {
	sync.Mutex
	manifests map[string]Object
}{manifests: map[string]Object{}}

// validatedSnapshotLimit bounds the cache. cw runs one command per process, so
// the limit only matters to long test processes; past it the cache restarts.
const validatedSnapshotLimit = 1024

// validatedSnapshot returns the manifest of a snapshot whose entries equal
// contents, validating the suite unless these exact entries at this root
// already passed. The caller must have computed contents from root itself.
func validatedSnapshot(root string, contents Object) (Object, error) {
	key := root + "\x00" + hash(legacyJSON(contents))
	validatedSnapshots.Lock()
	cached, ok := validatedSnapshots.manifests[key]
	validatedSnapshots.Unlock()
	if ok {
		return copyObject(cached), nil
	}
	manifest, err := LoadManifestValidated(root)
	if err != nil {
		return nil, err
	}
	// Remember the result only if the snapshot did not change while it was
	// being validated; otherwise this read stands alone, as without the cache.
	after, err := inventory(root)
	if err != nil || !equal(after, contents) {
		return manifest, nil
	}
	validatedSnapshots.Lock()
	if len(validatedSnapshots.manifests) >= validatedSnapshotLimit {
		clear(validatedSnapshots.manifests)
	}
	validatedSnapshots.manifests[key] = copyObject(manifest)
	validatedSnapshots.Unlock()
	return manifest, nil
}

// copyObject deep-copies decoded JSON so no caller shares nested maps or
// slices with the cache.
func copyObject(value Object) Object {
	out := make(Object, len(value))
	for key, item := range value {
		out[key] = copyValue(item)
	}
	return out
}

func copyValue(value any) any {
	switch typed := value.(type) {
	case Object:
		return copyObject(typed)
	case []any:
		out := make([]any, len(typed))
		for index, item := range typed {
			out[index] = copyValue(item)
		}
		return out
	default:
		return typed
	}
}

type snapshotReceipt struct {
	Version           int              `json:"version"`
	Base              bundled.Identity `json:"base"`
	PreferencesSHA256 string           `json:"preferences_sha256"`
	EffectiveDigest   string           `json:"effective_digest"`
	Inventory         Object           `json:"inventory"`
	Integrity         string           `json:"integrity_sha256"`
}

func validBaseIdentity(id bundled.Identity) bool {
	return commitSHA.MatchString(id.Revision) && digestPattern.MatchString(id.Digest) && (id.OS == "darwin" || id.OS == "linux") && (id.Arch == "arm64" || id.Arch == "amd64")
}

func copySnapshot(source, destination string) error {
	if err := realDirectory(source, false); err != nil {
		return err
	}
	directories := map[string]os.FileMode{}
	err := filepath.WalkDir(source, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		target := filepath.Join(destination, relative)
		if info.IsDir() {
			if err := realDirectory(target, true); err != nil {
				return err
			}
			directories[target] = info.Mode().Perm()
			return nil
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("snapshot contains an unsupported path: %s", relative)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return atomicWrite(target, data, info.Mode().Perm())
	})
	if err != nil {
		return err
	}
	for path, mode := range directories {
		if err := os.Chmod(path, mode); err != nil {
			return err
		}
		if err := syncDir(path); err != nil {
			return err
		}
	}
	return nil
}

func (i *Installer) baseRelease(id bundled.Identity) (string, Object, error) {
	if !validBaseIdentity(id) {
		return "", nil, errors.New("invalid immutable base identity")
	}
	root := filepath.Join(i.State, "bases", id.Digest)
	if err := realDirectory(root, false); err != nil {
		return "", nil, err
	}
	receipt, err := readJSON(root + ".receipt.json")
	if err != nil {
		return "", nil, err
	}
	if !exactFields(receipt, "version", "base", "inventory", "integrity_sha256") || integer(receipt["version"]) != 1 || verifySeal(receipt) != nil || !equal(receipt["base"], asObject(id)) {
		return "", nil, errors.New("invalid immutable base receipt")
	}
	actual, err := inventory(root)
	if err != nil {
		return "", nil, err
	}
	if !equal(actual, receipt["inventory"]) {
		return "", nil, errors.New("immutable base was modified")
	}
	m, err := validatedSnapshot(root, actual)
	return root, m, err
}

func (i *Installer) stageBase(root string, id bundled.Identity) error {
	parent := filepath.Join(i.State, "bases")
	if err := durableDirectory(parent, syncDir); err != nil {
		return err
	}
	destination := filepath.Join(parent, id.Digest)
	if exists(destination) && !exists(destination+".receipt.json") {
		// A crash can publish an unreferenced cache directory before its receipt.
		// Repair only from the caller's already verified exact base bytes.
		expected, err := inventory(root)
		if err != nil {
			return err
		}
		actual, err := inventory(destination)
		if err != nil {
			return err
		}
		if !equal(expected, actual) {
			return errors.New("unreceipted base differs from its verified source")
		}
		if err = atomicWrite(destination+".receipt.json", legacyJSON(seal(Object{"version": 1, "base": asObject(id), "inventory": expected})), 0600); err != nil {
			return err
		}
	}
	if exists(destination) || exists(destination+".receipt.json") {
		_, _, err := i.baseRelease(id)
		return err
	}
	tmp, err := os.MkdirTemp(parent, ".base-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	if err = copySnapshot(root, tmp); err != nil {
		return err
	}
	contents, err := inventory(tmp)
	if err != nil {
		return err
	}
	if err = os.Rename(tmp, destination); err != nil {
		return err
	}
	if err = syncDir(parent); err != nil {
		return err
	}
	if err = atomicWrite(destination+".receipt.json", legacyJSON(seal(Object{"version": 1, "base": asObject(id), "inventory": contents})), 0600); err != nil {
		return err
	}
	return i.fault("after-base-publication")
}

func compileEffective(root string, manifest Object, view ConfigurationView, prefs Preferences) error {
	for _, host := range view.Hosts {
		hp, exists := prefs.Hosts[host.ID]
		if !exists {
			continue
		}
		key := "model_policy"
		if host.ID == hostClaude {
			key = "claude_model_policy"
		}
		path := filepath.Join(root, text(manifest[key]))
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		value, err := suiteYAML(data)
		if err != nil {
			return err
		}
		policy := object(value)
		profiles, roles := object(policy["profiles"]), object(policy["roles"])
		set := func(role RoleConfiguration, override ModelOverride, fallback bool) error {
			selected, err := selectedModel(role, override)
			if err != nil {
				return err
			}
			prefix := "device_role_"
			if fallback {
				prefix = "device_fallback_"
			}
			name := prefix + hash([]byte(role.Role))
			for profiles[name] != nil {
				name += "_"
			}
			profile := Object{"model": selected.Model, "scope": role.Scope}
			if host.ID == hostCodex {
				profile["reasoning_effort"] = selected.Effort
				requested := []any{}
				for _, effort := range policyModelEfforts[selected.Model] {
					if effort != selected.Effort {
						requested = append(requested, effort)
					}
				}
				profile["user_requested_efforts"] = requested
			} else {
				profile["effort"] = selected.Effort
			}
			profiles[name] = profile
			if fallback {
				policy["unknown_role_profile"] = name
				if host.ID == hostCodex {
					object(policy["fallbacks"])["luna_unavailable"] = name
				}
			} else {
				roles[role.Role] = name
			}
			return nil
		}
		for _, role := range host.Roles {
			if override, ok := hp.Roles[role.Role]; ok {
				if err := set(role, override, false); err != nil {
					return err
				}
			}
		}
		if hp.Fallback != nil {
			if err := set(host.Fallback, *hp.Fallback, true); err != nil {
				return err
			}
		}
		if host.ID == hostCodex {
			effort := object(policy["effort_policy"])
			effort["default_substantive"] = object(profiles[text(roles["coordinator"])])["reasoning_effort"]
			maxProfiles := []string{}
			for _, name := range keys(profiles) {
				if object(profiles[name])["reasoning_effort"] == "max" {
					maxProfiles = append(maxProfiles, name)
				}
			}
			effort["configured_max_profiles"] = maxProfiles
			effort["max_for_sol"] = "configured_or_explicit_user_request"
		}
		data, err = yaml.Marshal(policy)
		if err != nil {
			return err
		}
		if err = atomicWrite(path, data, 0644); err != nil {
			return err
		}
	}
	if values := sharedPreferenceValues(prefs.Shared); len(values) != 0 {
		path := filepath.Join(root, sharedConfigResource)
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		var defaults Object
		if err = toml.Unmarshal(data, &defaults); err != nil {
			return err
		}
		for key, value := range values {
			if group, name, nested := strings.Cut(key, "."); nested {
				object(defaults[group])[name] = value
			} else {
				defaults[key] = value
			}
		}
		data, err = toml.Marshal(defaults)
		if err != nil {
			return err
		}
		if err = atomicWrite(path, data, 0644); err != nil {
			return err
		}
	}
	_, err := LoadManifestValidated(root)
	return err
}

func effectiveReceipt(root string, id bundled.Identity, prefs Preferences) (string, Object, error) {
	contents, err := inventory(root)
	if err != nil {
		return "", nil, err
	}
	prefsDigest := hash(legacyJSON(prefs))
	digest := hash(legacyJSON(Object{"base": asObject(id), "preferences_sha256": prefsDigest, "inventory": contents}))
	receipt := seal(Object{"version": 1, "base": asObject(id), "preferences_sha256": prefsDigest, "effective_digest": digest, "inventory": contents})
	return "effective-" + digest, receipt, nil
}

func (i *Installer) effectiveRelease(name string) (string, Object, error) {
	if !effectivePattern.MatchString(name) {
		return "", nil, errors.New("invalid effective snapshot identity")
	}
	root := filepath.Join(i.State, "releases", name)
	if err := realDirectory(root, false); err != nil {
		return "", nil, err
	}
	data, err := readRecordBytes(root + ".receipt.json")
	if err != nil {
		return "", nil, err
	}
	var receipt snapshotReceipt
	if err = json.Unmarshal(data, &receipt, json.RejectUnknownMembers(true)); err != nil {
		return "", nil, err
	}
	if receipt.Version != 1 || !validBaseIdentity(receipt.Base) || !digestPattern.MatchString(receipt.PreferencesSHA256) || name != "effective-"+receipt.EffectiveDigest || VerifyLocalSeal(data) != nil {
		return "", nil, errors.New("invalid effective snapshot receipt")
	}
	contents, err := inventory(root)
	if err != nil {
		return "", nil, err
	}
	digest := hash(legacyJSON(Object{"base": asObject(receipt.Base), "preferences_sha256": receipt.PreferencesSHA256, "inventory": contents}))
	if !equal(contents, receipt.Inventory) || digest != receipt.EffectiveDigest {
		return "", nil, errors.New("immutable effective snapshot was modified")
	}
	m, err := validatedSnapshot(root, contents)
	return root, m, err
}

func (i *Installer) stageEffective(root, name string, receipt Object) (string, Object, error) {
	parent := filepath.Join(i.State, "releases")
	if err := durableDirectory(parent, syncDir); err != nil {
		return "", nil, err
	}
	destination := filepath.Join(parent, name)
	if exists(destination) && !exists(destination+".receipt.json") {
		actual, err := inventory(destination)
		if err != nil {
			return "", nil, err
		}
		if !equal(actual, receipt["inventory"]) {
			return "", nil, errors.New("unreceipted effective snapshot differs from its compiled source")
		}
		if err = atomicWrite(destination+".receipt.json", legacyJSON(receipt), 0600); err != nil {
			return "", nil, err
		}
	}
	if exists(destination) || exists(destination+".receipt.json") {
		return i.effectiveRelease(name)
	}
	tmp, err := os.MkdirTemp(parent, ".effective-")
	if err != nil {
		return "", nil, err
	}
	defer os.RemoveAll(tmp)
	if err = copySnapshot(root, tmp); err != nil {
		return "", nil, err
	}
	actual, err := inventory(tmp)
	if err != nil || !equal(actual, receipt["inventory"]) {
		return "", nil, errors.Join(errors.New("effective staging content changed"), err)
	}
	if err = os.Rename(tmp, destination); err != nil {
		return "", nil, err
	}
	if err = syncDir(parent); err != nil {
		return "", nil, err
	}
	if err = atomicWrite(destination+".receipt.json", legacyJSON(receipt), 0600); err != nil {
		return "", nil, err
	}
	if err = i.fault("after-effective-publication"); err != nil {
		return "", nil, err
	}
	return i.effectiveRelease(name)
}

func (i *Installer) verifySnapshotBinding(state Object) error {
	name := text(state["release"])
	if _, _, err := i.effectiveRelease(name); err != nil {
		return err
	}
	receipt, err := readJSON(filepath.Join(i.State, "releases", name) + ".receipt.json")
	if err != nil {
		return err
	}
	if !equal(receipt["base"], state["base"]) || receipt["preferences_sha256"] != state["preferences_sha256"] {
		return errors.New("effective snapshot differs from ownership identity")
	}
	var base bundled.Identity
	if err := json.Unmarshal(legacyJSON(state["base"]), &base, json.RejectUnknownMembers(true)); err != nil {
		return err
	}
	if _, _, err := i.baseRelease(base); err != nil {
		return err
	}
	for _, raw := range sequence(state["history"]) {
		previous := object(raw)
		var id bundled.Identity
		if err := json.Unmarshal(legacyJSON(previous["base"]), &id, json.RejectUnknownMembers(true)); err != nil {
			return err
		}
		if _, _, err := i.baseRelease(id); err != nil {
			return err
		}
		if _, _, err := i.effectiveRelease(text(previous["release"])); err != nil {
			return err
		}
		previousReceipt, err := readJSON(filepath.Join(i.State, "releases", text(previous["release"])) + ".receipt.json")
		if err != nil {
			return err
		}
		if !equal(previousReceipt["base"], previous["base"]) {
			return errors.New("rollback base differs from its retained effective snapshot")
		}
	}
	return nil
}

func validReleaseKey(key string) bool {
	return effectivePattern.MatchString(key)
}

func runtimeKeyRevision(key string) string {
	if standaloneRuntimePattern.MatchString(key) {
		return key[:40]
	}
	return ""
}

func (i *Installer) candidateRuntimeKey() string {
	return i.RuntimeIdentity.Revision + "-" + i.bundleIdentity.Digest
}

func managerRuntimeKey(manager ManagerRecord) string { return manager.RuntimeKey }

func supportedRuntimeKey(key string) bool { return runtimeKeyRevision(key) != "" }

func readableReceiptFields(value Object, version int) bool {
	return version == 3 && exactFields(value, "version", "identity", "sha256", "locator_sha256", "manager_protocol", "integrity_sha256", "base_digest")
}
