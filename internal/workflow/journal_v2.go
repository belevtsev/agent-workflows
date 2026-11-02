package workflow

import (
	json "encoding/json/v2"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func (i *Installer) mutationPaths(op Object) ([]string, error) {
	if op["kind"] == "personal_skill" {
		record, err := i.validatePersonalOperation(op)
		if err != nil {
			return nil, err
		}
		paths := []string{record.OriginPath, record.BackupPath, record.RegistrationPath}
		for a, left := range paths {
			for b := a + 1; b < len(paths); b++ {
				right := paths[b]
				if a == 0 && b == 2 && left == right {
					continue
				}
				overlap, err := pathsOverlap(left, right)
				if err != nil {
					return nil, err
				}
				if overlap {
					return nil, errors.New("personal operation mutation paths overlap")
				}
			}
		}
		if record.OriginPath == record.RegistrationPath {
			paths = paths[:2]
		}
		return paths, nil
	}
	return []string{text(op["path"])}, nil
}

// A mutation replaces a directory entry, so inspect the leaf with Lstat. In
// particular, two managed symlinks to one release remain separate destinations.
type mutationPathIdentity struct {
	anchor  os.FileInfo
	missing []string
}

type mutationDestination struct {
	path     string
	ancestry []mutationPathIdentity
}

func inspectMutationDestination(path string) (mutationDestination, error) {
	result := mutationDestination{path: path}
	var missing []string
	for {
		info, err := os.Lstat(path)
		if err == nil {
			for len(missing) > 0 {
				result.ancestry = append(result.ancestry, mutationPathIdentity{info, missing})
				missing = missing[:len(missing)-1]
			}
			result.ancestry = append(result.ancestry, mutationPathIdentity{anchor: info})
		} else if os.IsNotExist(err) {
			missing = append([]string{filepath.Base(path)}, missing...)
		} else {
			return result, err
		}
		parent := filepath.Dir(path)
		if parent == path {
			return result, nil
		}
		path = parent
	}
}

func sameMutationIdentity(a, b mutationPathIdentity) (same, undecided bool) {
	if !os.SameFile(a.anchor, b.anchor) || len(a.missing) != len(b.missing) {
		return false, false
	}
	for index, name := range a.missing {
		other := b.missing[index]
		if name == other {
			continue
		}
		if plainASCII(name) && plainASCII(other) && !strings.EqualFold(name, other) {
			return false, false
		}
		undecided = true
	}
	return !undecided, undecided
}

func mutationDestinationsOverlap(a, b mutationDestination) (bool, error) {
	for _, pair := range [][2]mutationDestination{{a, b}, {b, a}} {
		for _, ancestor := range pair[1].ancestry {
			same, undecided := sameMutationIdentity(pair[0].ancestry[0], ancestor)
			if same {
				return true, nil
			}
			if undecided {
				return false, fmt.Errorf("cannot tell whether mutation paths %s and %s overlap until they exist; create their parent directories and run this again", a.path, b.path)
			}
		}
	}
	return false, nil
}

func pathsOverlap(a, b string) (bool, error) {
	left, err := inspectMutationDestination(a)
	if err != nil {
		return false, err
	}
	right, err := inspectMutationDestination(b)
	if err != nil {
		return false, err
	}
	return mutationDestinationsOverlap(left, right)
}

func (i *Installer) validateMutationPaths(ops []any, field string) error {
	var previous []mutationDestination
	for _, raw := range ops {
		paths, err := i.mutationPaths(object(raw))
		if err != nil {
			return err
		}
		for _, target := range paths {
			if target == "" || Normalize(target) != target {
				return errors.New("unsafe journal mutation path")
			}
			if err := realDirectory(filepath.Dir(target), false); err != nil {
				return err
			}
			destination, err := inspectMutationDestination(target)
			if err != nil {
				return err
			}
			for _, other := range previous {
				overlap, err := mutationDestinationsOverlap(destination, other)
				if err != nil {
					return err
				}
				if overlap {
					return fmt.Errorf("overlapping mutation paths in %s: %s", field, target)
				}
			}
			previous = append(previous, destination)
		}
	}
	return nil
}

// preflightMutationPaths also reserves native destinations when a preview has
// not acquired its exact-revision runtime yet. Runtime acquisition and staging
// may write managed roots, so these paths must be checked first.
func (i *Installer) preflightMutationPaths(ops []Object, state Object, manager bool) error {
	operations := make([]any, 0, len(ops)+5)
	paths := map[string]bool{}
	for _, op := range ops {
		operations = append(operations, op)
		paths[text(op["path"])] = true
	}
	if manager && i.nativeRequested() {
		reserved := []string{i.runtimeCurrent(), i.commandPath(), i.locatorPath()}
		if profile := object(object(state["manager"])["profile"]); profile != nil {
			reserved = append(reserved, text(profile["path"]))
		}
		if shell, _ := selectedShell(i.Shell, i.Paths, map[string]string{"SHELL": i.Getenv("SHELL"), "ZDOTDIR": i.Getenv("ZDOTDIR")}); shell != "" && !pathIncludes(i.Getenv("PATH"), filepath.Dir(i.commandPath())) {
			path, err := shellStartupPath(i.Paths, shell)
			if err != nil {
				return err
			}
			reserved = append(reserved, path)
		}
		for _, path := range reserved {
			if !paths[path] {
				operations = append(operations, Object{"path": path})
				paths[path] = true
			}
		}
	}
	return i.validateMutationPaths(operations, "operations")
}

func stateFromObservation(ob Object) (Object, error) {
	if ob["kind"] != "file" {
		return nil, nil
	}
	data, err := decode(ob["data"])
	if err != nil {
		return nil, err
	}
	var state Object
	if err = json.Unmarshal(data, &state); err != nil {
		return nil, err
	}
	return state, nil
}

func (i *Installer) verifyRecoveryRuntime(identity RuntimeIdentity) error {
	receipt, err := i.verifiedRuntimeReceipt(identity.Revision)
	if err != nil {
		return err
	}
	if receipt.Identity != identity {
		return errors.New("recovery runtime identity differs from its immutable receipt")
	}
	protocol, err := i.inspectRuntimeProtocol(filepath.Join(i.runtimeDir(identity.Revision), "cw"))
	if err != nil || protocol != receipt.ManagerProtocol {
		return errors.New("staged recovery runtime does not support the manager protocol")
	}
	return nil
}

// recoveryRuntimeFor names the verified runtime that can recover these
// operations. Transactions before the first manager enrollment have none.
func (i *Installer) recoveryRuntimeFor(ops []Object) (RuntimeIdentity, bool, error) {
	if i.runtimeProtocol == ManagerProtocol && validRuntimeIdentity(i.RuntimeIdentity) {
		return i.RuntimeIdentity, true, i.verifyRecoveryRuntime(i.RuntimeIdentity)
	}
	for _, op := range ops {
		if op["path"] != i.statePath {
			continue
		}
		for _, side := range []string{"after", "before"} {
			state, err := stateFromObservation(object(op[side]))
			if err != nil {
				return RuntimeIdentity{}, false, err
			}
			manager := object(state["manager"])
			if manager == nil {
				continue
			}
			var identity RuntimeIdentity
			if err = json.Unmarshal(legacyJSON(manager["runtime"]), &identity, json.RejectUnknownMembers(true)); err != nil {
				return identity, false, err
			}
			return identity, true, i.verifyRecoveryRuntime(identity)
		}
	}
	return RuntimeIdentity{}, false, nil
}
