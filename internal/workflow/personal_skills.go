package workflow

import (
	json "encoding/json/v2"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"syscall"
)

const personalCatalogName = "personal-skill-origins.json"

// PersonalInventoryEntry records original bytes by hash, including the root's
// mode. These records never retain personal file contents in state or journals.
type PersonalInventoryEntry struct {
	Path   string `json:"path"`
	Kind   string `json:"kind"`
	Mode   int    `json:"mode"`
	SHA256 string `json:"sha256,omitempty"`
}

type PersonalSkillCatalogRecord struct {
	Inventory []PersonalInventoryEntry `json:"inventory"`
}

type PersonalSkillCatalog struct {
	Version int                                   `json:"version"`
	Skills  map[string]PersonalSkillCatalogRecord `json:"skills"`
}

type PersonalSkillOriginRecord struct {
	Kind      string                   `json:"kind"`
	Path      string                   `json:"path"`
	Backup    string                   `json:"backup"`
	Inventory []PersonalInventoryEntry `json:"inventory"`
}

// PersonalSkillOperationRecord owns the directory move and discovery link as
// one operation. "moved" is the only intermediate state: the original is in its
// backup and the registration is absent. The original and registration may be
// the same path; no other operations may share either path.
type PersonalSkillOperationRecord struct {
	Name             string                   `json:"name"`
	OriginPath       string                   `json:"origin_path"`
	BackupPath       string                   `json:"backup_path"`
	RegistrationPath string                   `json:"registration_path"`
	Target           string                   `json:"target"`
	Inventory        []PersonalInventoryEntry `json:"inventory"`
	BeforePhase      string                   `json:"before_phase"`
	AfterPhase       string                   `json:"after_phase"`
}

var personalSHA = regexp.MustCompile(`^[0-9a-f]{64}$`)

func validatePersonalInventory(entries []PersonalInventoryEntry) error {
	if len(entries) < 2 || entries[0].Path != "." || entries[0].Kind != "directory" {
		return errors.New("personal inventory must include its root directory and skill files")
	}
	seen := map[string]string{}
	previous := ""
	for index, item := range entries {
		if item.Mode < 0 || item.Mode > 0o777 || (index > 0 && item.Path <= previous) {
			return errors.New("personal inventory has invalid modes, ordering, or duplicate paths")
		}
		if item.Path != "." && (!registrationRoot(item.Path) || path.Clean(item.Path) != item.Path || suiteDrive.MatchString(item.Path)) {
			return errors.New("personal inventory has an unsafe path")
		}
		switch item.Kind {
		case "directory":
			if item.SHA256 != "" {
				return errors.New("personal directory inventory must not contain a hash")
			}
		case "file":
			if !personalSHA.MatchString(item.SHA256) {
				return errors.New("personal file inventory requires a SHA256 hash")
			}
		default:
			return errors.New("personal inventory supports only directories and regular files")
		}
		if index > 0 && seen[path.Dir(item.Path)] != "directory" {
			return errors.New("personal inventory omits a parent directory")
		}
		seen[item.Path] = item.Kind
		previous = item.Path
	}
	if seen["SKILL.md"] != "file" {
		return errors.New("personal inventory must include SKILL.md")
	}
	return nil
}

func readPersonalCatalog(root string, manifest Object) (PersonalSkillCatalog, error) {
	var result PersonalSkillCatalog
	if integer(manifest["version"]) != 3 {
		return result, nil
	}
	if manifest["adoption_catalog"] != personalCatalogName {
		return result, errors.New("adoption_catalog must be personal-skill-origins.json")
	}
	root, err := suiteRoot(root, false)
	if err != nil {
		return result, err
	}
	target, err := suiteRelativePath(root, manifest["adoption_catalog"], "adoption_catalog")
	if err != nil {
		return result, err
	}
	data, err := readRecordBytes(target)
	if err != nil {
		return result, fmt.Errorf("cannot read personal skill catalog: %w", err)
	}
	if err = json.Unmarshal(data, &result, json.RejectUnknownMembers(true)); err != nil {
		return result, fmt.Errorf("invalid personal skill catalog: %w", err)
	}
	if result.Version != 1 || result.Skills == nil {
		return result, errors.New("unsupported or incomplete personal skill catalog")
	}
	var raw Object
	if err := json.Unmarshal(data, &raw); err != nil {
		return result, err
	}
	for name, skill := range result.Skills {
		if err := validatePersonalInventoryFields(object(object(raw["skills"])[name])["inventory"]); err != nil {
			return result, err
		}
		if !suiteName.MatchString(name) || len(name) > 64 || object(manifest["registrations"])[name] == nil {
			return result, errors.New("personal skill catalog contains an unregistered name")
		}
		if err = validatePersonalInventory(skill.Inventory); err != nil {
			return result, fmt.Errorf("personal skill catalog %s: %w", name, err)
		}
	}
	return result, nil
}

func personalInventory(root string) ([]PersonalInventoryEntry, error) {
	if err := realDirectory(root, false); err != nil {
		return nil, err
	}
	info, err := os.Lstat(root)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("personal skill must be a real directory: %s", root)
	}
	entries := []PersonalInventoryEntry{{Path: ".", Kind: "directory", Mode: unixMode(info.Mode())}}
	err = filepath.WalkDir(root, func(target string, entry os.DirEntry, err error) error {
		if err != nil || target == root {
			return err
		}
		info, err := os.Lstat(target)
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, target)
		if err != nil {
			return err
		}
		item := PersonalInventoryEntry{Path: filepath.ToSlash(relative), Mode: unixMode(info.Mode())}
		switch {
		case info.IsDir():
			item.Kind = "directory"
		case info.Mode().IsRegular():
			item.Kind = "file"
			data, err := os.ReadFile(target)
			if err != nil {
				return err
			}
			item.SHA256 = hash(data)
		default:
			return fmt.Errorf("unsupported personal skill path: %s", target)
		}
		entries = append(entries, item)
		return nil
	})
	if err != nil {
		return nil, err
	}
	slices.SortFunc(entries, func(a, b PersonalInventoryEntry) int { return strings.Compare(a.Path, b.Path) })
	return entries, validatePersonalInventory(entries)
}

func validatePersonalInventoryFields(raw any) error {
	entries, ok := raw.([]any)
	if !ok {
		return errors.New("personal inventory must be an array")
	}
	for _, raw := range entries {
		item := object(raw)
		fields := []string{"path", "kind", "mode"}
		if item["kind"] == "file" {
			fields = append(fields, "sha256")
		}
		if !exactFields(item, fields...) || !validMode(item["mode"]) {
			return errors.New("personal inventory entry has invalid fields or mode")
		}
	}
	return nil
}

func (i *Installer) discoveryRoots() ([]string, error) {
	var roots []string
	for _, root := range []string{i.skills, filepath.Join(i.Codex, "skills"), filepath.Join(i.Home, ".codex", "skills")} {
		root = Normalize(root)
		if slices.Contains(roots, root) {
			continue
		}
		if err := realDirectory(root, false); err != nil {
			return nil, err
		}
		roots = append(roots, root)
	}
	return roots, nil
}

func (i *Installer) personalBackup(name string) string {
	return filepath.Join(i.State, "backups", "personal-skills", name)
}

func deviceOfClosestExisting(target string) (uint64, error) {
	for {
		info, err := os.Lstat(target)
		if err == nil {
			if info.Mode()&os.ModeSymlink != 0 {
				return 0, errors.New("unsafe symlink in personal backup ancestry")
			}
			stat, ok := info.Sys().(*syscall.Stat_t)
			if !ok {
				return 0, errors.New("cannot verify personal backup filesystem")
			}
			return uint64(stat.Dev), nil
		}
		if !os.IsNotExist(err) || target == filepath.Dir(target) {
			return 0, err
		}
		target = filepath.Dir(target)
	}
}

func checkPersonalRenameFilesystem(origin, backup string) error {
	if err := realDirectory(filepath.Dir(backup), false); err != nil {
		return err
	}
	a, err := deviceOfClosestExisting(origin)
	if err != nil {
		return err
	}
	b, err := deviceOfClosestExisting(filepath.Dir(backup))
	if err != nil {
		return err
	}
	if a != b {
		return errors.New("personal adoption backup must be on the same filesystem")
	}
	return nil
}

func (i *Installer) personalAdoptions(additions, manifest Object, root string) (map[string]PersonalSkillOriginRecord, error) {
	result := map[string]PersonalSkillOriginRecord{}
	roots, err := i.discoveryRoots()
	if err != nil {
		return nil, err
	}
	catalog, err := readPersonalCatalog(root, manifest)
	if err != nil {
		return nil, err
	}
	for _, name := range keys(additions) {
		var occupants []string
		for _, discovery := range roots {
			target := filepath.Join(discovery, name)
			_, err := os.Lstat(target)
			if err == nil {
				occupants = append(occupants, target)
			} else if err != nil && !os.IsNotExist(err) {
				return nil, err
			}
		}
		if len(occupants) == 0 {
			continue
		}
		if len(occupants) != 1 {
			return nil, fmt.Errorf("duplicate discovery registration exists: %s", name)
		}
		trusted, ok := catalog.Skills[name]
		if !ok {
			return nil, fmt.Errorf("occupied personal registration has no trusted catalog: %s", occupants[0])
		}
		actual, err := personalInventory(occupants[0])
		if err != nil || !slices.Equal(actual, trusted.Inventory) {
			return nil, fmt.Errorf("personal skill differs from trusted original inventory: %s", occupants[0])
		}
		backup := i.personalBackup(name)
		if _, err := os.Lstat(backup); !os.IsNotExist(err) {
			return nil, fmt.Errorf("personal backup path is occupied or unsafe: %s", backup)
		}
		if err := checkPersonalRenameFilesystem(occupants[0], backup); err != nil {
			return nil, err
		}
		origin := PersonalSkillOriginRecord{Kind: "personal", Path: occupants[0], Backup: backup, Inventory: actual}
		operation := personalOperation(name, origin, filepath.Join(i.skills, name), i.target(text(additions[name])), "personal", "managed", "adopt-personal:"+name)
		if _, err := i.mutationPaths(operation); err != nil {
			return nil, err
		}
		result[name] = origin
	}
	return result, nil
}

func personalOperation(name string, origin PersonalSkillOriginRecord, registration, target, before, after, label string) Object {
	record := PersonalSkillOperationRecord{Name: name, OriginPath: origin.Path, BackupPath: origin.Backup,
		RegistrationPath: registration, Target: target, Inventory: origin.Inventory, BeforePhase: before, AfterPhase: after}
	return asObject(OperationRecord{Kind: "personal_skill", Path: registration, Label: label, Personal: &record})
}

func (i *Installer) validatePersonalOrigin(name string, raw Object) (PersonalSkillOriginRecord, error) {
	var origin PersonalSkillOriginRecord
	if !exactFields(raw, "kind", "path", "backup", "inventory") {
		return origin, errors.New("invalid personal origin fields")
	}
	if err := json.Unmarshal(legacyJSON(raw), &origin, json.RejectUnknownMembers(true)); err != nil {
		return origin, err
	}
	if err := validatePersonalInventoryFields(raw["inventory"]); err != nil {
		return origin, err
	}
	if origin.Kind != "personal" || !suiteName.MatchString(name) || len(name) > 64 || origin.Backup != i.personalBackup(name) {
		return origin, errors.New("unsafe personal origin")
	}
	roots, err := i.discoveryRoots()
	if err != nil {
		return origin, err
	}
	if !slices.ContainsFunc(roots, func(root string) bool { return origin.Path == filepath.Join(root, name) }) {
		return origin, errors.New("personal origin is outside eligible discovery roots")
	}
	return origin, validatePersonalInventory(origin.Inventory)
}

func (i *Installer) validatePersonalOperation(op Object) (PersonalSkillOperationRecord, error) {
	var result PersonalSkillOperationRecord
	if !exactFields(op, "kind", "path", "label", "personal") || op["kind"] != "personal_skill" {
		return result, errors.New("invalid composed personal operation fields")
	}
	if err := json.Unmarshal(legacyJSON(op["personal"]), &result, json.RejectUnknownMembers(true)); err != nil {
		return result, err
	}
	if !exactFields(object(op["personal"]), "name", "origin_path", "backup_path", "registration_path", "target", "inventory", "before_phase", "after_phase") {
		return result, errors.New("invalid composed personal record fields")
	}
	if err := validatePersonalInventoryFields(object(op["personal"])["inventory"]); err != nil {
		return result, err
	}
	if result.RegistrationPath != filepath.Join(i.skills, result.Name) || op["path"] != result.RegistrationPath || result.Target == "" || Normalize(result.Target) != result.Target || !strings.HasPrefix(result.Target, i.current+string(filepath.Separator)) || !registrationRoot(strings.TrimPrefix(result.Target, i.current+string(filepath.Separator))) {
		return result, errors.New("unsafe composed personal registration")
	}
	if result.BeforePhase == result.AfterPhase || !slices.Contains([]string{"personal", "moved", "managed"}, result.BeforePhase) || !slices.Contains([]string{"personal", "managed"}, result.AfterPhase) {
		return result, errors.New("invalid composed personal phases")
	}
	origin := PersonalSkillOriginRecord{Kind: "personal", Path: result.OriginPath, Backup: result.BackupPath, Inventory: result.Inventory}
	_, err := i.validatePersonalOrigin(result.Name, asObject(origin))
	return result, err
}

func matchesPersonalDirectory(target string, expected []PersonalInventoryEntry) (bool, error) {
	info, err := os.Lstat(target)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return false, nil
	}
	actual, err := personalInventory(target)
	return err == nil && slices.Equal(actual, expected), err
}

func (i *Installer) personalPhase(record PersonalSkillOperationRecord) (string, error) {
	for _, target := range []string{record.OriginPath, record.BackupPath, record.RegistrationPath} {
		if err := realDirectory(filepath.Dir(target), false); err != nil {
			return "", err
		}
	}
	original, err := matchesPersonalDirectory(record.OriginPath, record.Inventory)
	if err != nil {
		return "", err
	}
	backup, err := matchesPersonalDirectory(record.BackupPath, record.Inventory)
	if err != nil {
		return "", err
	}
	registration, err := observe(record.RegistrationPath)
	if err != nil {
		return "", err
	}
	originAbsent := !exists(record.OriginPath)
	backupAbsent := !exists(record.BackupPath)
	registrationAbsent := registration["kind"] == "absent"
	managed := equal(registration, Object{"kind": "symlink", "target": record.Target})
	same := record.OriginPath == record.RegistrationPath
	switch {
	case original && backupAbsent && (same || registrationAbsent):
		return "personal", nil
	case originAbsent && backup && registrationAbsent:
		return "moved", nil
	case backup && managed && (same || originAbsent):
		return "managed", nil
	default:
		return "", fmt.Errorf("personal skill ownership changed; refusing overwrite: %s", record.Name)
	}
}

func renamePersonalDirectory(from, to string) error {
	return renamePersonalDirectoryWithSync(from, to, syncDir)
}

func renamePersonalDirectoryWithSync(from, to string, syncDirectory func(string) error) error {
	if err := checkPersonalRenameFilesystem(from, to); err != nil {
		return err
	}
	if err := durableDirectory(filepath.Dir(to), syncDirectory); err != nil {
		return err
	}
	if err := os.Rename(from, to); err != nil {
		return err
	}
	if err := syncDirectory(filepath.Dir(from)); err != nil {
		return err
	}
	return syncDirectory(filepath.Dir(to))
}

func (i *Installer) performPersonal(op Object) error {
	record, err := i.validatePersonalOperation(op)
	if err != nil {
		return err
	}
	phase, err := i.personalPhase(record)
	if err != nil {
		return err
	}
	if phase != record.BeforePhase && phase != "moved" {
		return errors.New("personal adoption phase changed during mutation")
	}
	if record.AfterPhase == "managed" {
		if phase == "personal" {
			if err = renamePersonalDirectory(record.OriginPath, record.BackupPath); err != nil {
				return err
			}
			if err = fault("after-" + text(op["label"]) + ":rename"); err != nil {
				return err
			}
		}
		if err = i.Context.Err(); err != nil {
			return err
		}
		if phase, err = i.personalPhase(record); err != nil || phase != "moved" {
			return errors.New("personal backup or registration changed before link creation")
		}
		if err = writeObservation(record.RegistrationPath, Object{"kind": "symlink", "target": record.Target}); err != nil {
			return err
		}
		return fault("after-" + text(op["label"]) + ":link")
	}
	if phase == "managed" {
		if err = writeObservation(record.RegistrationPath, Object{"kind": "absent"}); err != nil {
			return err
		}
		if err = fault("after-" + text(op["label"]) + ":unlink"); err != nil {
			return err
		}
	}
	if err = i.Context.Err(); err != nil {
		return err
	}
	if phase, err = i.personalPhase(record); err != nil || phase != "moved" {
		return errors.New("personal restore destination or backup changed before rename")
	}
	if err = renamePersonalDirectory(record.BackupPath, record.OriginPath); err != nil {
		return err
	}
	return fault("after-" + text(op["label"]) + ":rename")
}

func (i *Installer) personalReversal(op Object) (Object, error) {
	record, err := i.validatePersonalOperation(op)
	if err != nil {
		return nil, err
	}
	phase, err := i.personalPhase(record)
	if err != nil || phase == record.BeforePhase {
		return nil, err
	}
	record.AfterPhase, record.BeforePhase = record.BeforePhase, phase
	return asObject(OperationRecord{Kind: "personal_skill", Path: record.RegistrationPath, Label: "recover-personal:" + record.Name, Personal: &record}), nil
}

func (i *Installer) personalPending(op Object) (Object, error) {
	record, err := i.validatePersonalOperation(op)
	if err != nil {
		return nil, err
	}
	phase, err := i.personalPhase(record)
	if err != nil || phase == record.AfterPhase {
		return nil, err
	}
	if phase != record.BeforePhase && phase != "moved" {
		return nil, errors.New("personal skill changed during interrupted recovery")
	}
	record.BeforePhase = phase
	return asObject(OperationRecord{Kind: "personal_skill", Path: record.RegistrationPath, Label: text(op["label"]), Personal: &record}), nil
}
