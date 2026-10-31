package workflow

import (
	"bytes"
	"context"
	"encoding/json/jsontext"
	json "encoding/json/v2"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const profileStart = "# >>> agent-workflows PATH >>>"
const profileEnd = "# <<< agent-workflows PATH <<<"

// CodexDefaultsCapability identifies native support for shared config ownership.
const CodexDefaultsCapability = "cw-codex-defaults-v1"

// OpenAIDecisionsCapability identifies the optional adviser policy extension.
const OpenAIDecisionsCapability = "cw-openai-decisions-v1"

// ThreeAdviserCapability identifies the required three-provider policy contract.
const ThreeAdviserCapability = "cw-three-adviser-v1"

type ProfileRecord struct {
	Shell          string `json:"shell"`
	Path           string `json:"path"`
	Segment        string `json:"segment"`
	OriginalExists bool   `json:"original_exists"`
	OriginalMode   int    `json:"original_mode"`
}

type ManagerRecord struct {
	Version       int             `json:"version"`
	Source        string          `json:"source"`
	Runtime       RuntimeIdentity `json:"runtime"`
	CommandPath   string          `json:"command_path"`
	CommandTarget string          `json:"command_target"`
	Profile       *ProfileRecord  `json:"profile,omitempty"`
}

type RuntimeLocator struct {
	Version     int    `json:"version,omitzero"`
	Source      string `json:"source"`
	Home        string `json:"home"`
	Codex       string `json:"codex"`
	State       string `json:"state"`
	CommandPath string `json:"command_path,omitempty"`
	Integrity   string `json:"integrity_sha256,omitempty"`
}

type runtimeReceipt struct {
	Version         int             `json:"version"`
	Identity        RuntimeIdentity `json:"identity"`
	SHA256          string          `json:"sha256"`
	LocatorSHA256   string          `json:"locator_sha256"`
	ManagerProtocol string          `json:"manager_protocol,omitempty"`
	Integrity       string          `json:"integrity_sha256,omitempty"`
}

var runtimeVersion = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9.+_-]*$`)

func (i *Installer) commandPath() string    { return filepath.Join(i.Home, ".local", "bin", "cw") }
func (i *Installer) runtimeCurrent() string { return filepath.Join(i.State, "runtime", "current") }
func (i *Installer) locatorPath() string    { return filepath.Join(i.State, "runtime", "locator.json") }
func (i *Installer) runtimeDir(revision string) string {
	return filepath.Join(i.State, "runtime", "releases", revision)
}

func validRuntimeIdentity(identity RuntimeIdentity) bool {
	return commitSHA.MatchString(identity.Revision) && runtimeVersion.MatchString(identity.Version) &&
		(identity.OS == "darwin" || identity.OS == "linux") && (identity.Arch == "amd64" || identity.Arch == "arm64")
}

func (i *Installer) nativeRequested() bool {
	return i.RuntimeCandidate != "" || i.PrepareRuntime != nil
}

func (i *Installer) prepareRuntimeFor(sha string) error {
	if !i.nativeRequested() || !i.Apply {
		return nil
	}
	if i.RuntimeCandidate == "" || i.RuntimeIdentity.Revision != sha {
		if i.PrepareRuntime == nil {
			return errors.New("an exact-revision native runtime candidate is required")
		}
		candidate, err := i.PrepareRuntime(i.Context, i.Source, sha, i.State)
		if err != nil {
			return err
		}
		i.RuntimeCandidate, i.RuntimeIdentity = candidate.Path, candidate.Identity
	}
	if !validRuntimeIdentity(i.RuntimeIdentity) || i.RuntimeIdentity.Revision != sha {
		return errors.New("native runtime candidate does not match the selected revision")
	}
	protocol, err := i.inspectRuntimeProtocol(i.RuntimeCandidate)
	if err != nil {
		return err
	}
	i.runtimeProtocol = protocol
	if i.AdoptPersonalSkills && protocol != "cw-manager-v6" {
		return errors.New("personal skill adoption requires a verified native v6 runtime")
	}
	return nil
}

func (i *Installer) inspectRuntimeProtocol(binary string) (string, error) {
	return i.inspectRuntimeCapability(binary, "--manager-protocol")
}

func (i *Installer) inspectRuntimeCapability(binary, flag string) (string, error) {
	if err := realDirectory(filepath.Dir(binary), false); err != nil {
		return "", err
	}
	info, err := os.Lstat(binary)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
		return "", errors.New("native runtime candidate must be a regular executable")
	}
	ctx, cancel := context.WithTimeout(i.Context, 5*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, binary, flag)
	var output bytes.Buffer
	command.Stdout = &boundedProtocolWriter{buffer: &output}
	if err := command.Run(); err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", fmt.Errorf("cannot inspect native runtime capability: %w", err)
	}
	return strings.TrimSpace(output.String()), nil
}

func (i *Installer) verifySharedConfigRuntime(binary string) error {
	protocol, err := i.inspectRuntimeProtocol(binary)
	if err != nil {
		return err
	}
	if protocol != "cw-manager-v6" {
		return errors.New("shared Codex config requires a verified native v6 runtime")
	}
	capability, err := i.inspectRuntimeCapability(binary, "--codex-defaults-capability")
	if err != nil {
		return fmt.Errorf("shared Codex config requires native capability %s: %w", CodexDefaultsCapability, err)
	}
	if capability != CodexDefaultsCapability {
		return fmt.Errorf("shared Codex config requires native capability %s", CodexDefaultsCapability)
	}
	return nil
}

func (i *Installer) verifyPolicyRuntime(binary, release string, manifest Object) error {
	release, err := suiteRoot(release, false)
	if err != nil {
		return err
	}
	policyPath, err := suiteRelativePath(release, manifest["model_policy"], "model_policy")
	if err != nil {
		return err
	}
	policy, err := suiteModelPolicy(policyPath)
	if err != nil {
		return err
	}
	consultation := object(policy["consultation"])
	_, optional := consultation["optional_providers"]
	_, required := object(consultation["providers"])["openai_decisions"]
	if !optional && !required {
		return nil
	}
	name, flag, expected := "optional OpenAI adviser policy", "--openai-decisions-capability", OpenAIDecisionsCapability
	if required {
		name, flag, expected = "required three-adviser policy", "--three-adviser-capability", ThreeAdviserCapability
	}
	protocol, err := i.inspectRuntimeProtocol(binary)
	if err != nil {
		return err
	}
	if protocol != "cw-manager-v6" {
		return fmt.Errorf("%s requires a verified native v6 runtime", name)
	}
	capability, err := i.inspectRuntimeCapability(binary, flag)
	if err != nil {
		return fmt.Errorf("%s requires native capability %s: %w", name, expected, err)
	}
	if capability != expected {
		return fmt.Errorf("%s requires native capability %s", name, expected)
	}
	return nil
}

type boundedProtocolWriter struct{ buffer *bytes.Buffer }

func (w *boundedProtocolWriter) Write(data []byte) (int, error) {
	if w.buffer.Len()+len(data) > 4096 {
		return 0, errors.New("native runtime capability output exceeds limit")
	}
	return w.buffer.Write(data)
}

func (i *Installer) runtimeLocatorBytes() []byte {
	locator := RuntimeLocator{Source: i.Source, Home: i.Home, Codex: i.Codex, State: i.State, CommandPath: i.commandPath()}
	if i.runtimeProtocol == "cw-manager-v6" {
		locator.Version = 2
		return legacyJSON(seal(asObject(locator)))
	}
	return legacyJSON(locator)
}

func asObject(value any) Object {
	data, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	var result Object
	if err = json.Unmarshal(data, &result); err != nil {
		panic(err)
	}
	return result
}

func (i *Installer) decodeRuntimeLocator(data []byte) (RuntimeLocator, error) {
	var locator RuntimeLocator
	if err := json.Unmarshal(data, &locator, json.RejectUnknownMembers(true)); err != nil {
		return RuntimeLocator{}, err
	}
	if locator.Version == 2 {
		if err := VerifyLocalSeal(data); err != nil {
			return RuntimeLocator{}, err
		}
	} else if locator.Version != 0 || locator.Integrity != "" {
		return RuntimeLocator{}, errors.New("unsupported native locator version")
	}
	if locator.Home != i.Home || locator.Codex != i.Codex || locator.State != i.State || locator.Source == "" || Normalize(locator.Source) != locator.Source {
		return RuntimeLocator{}, errors.New("unsafe native locator roots")
	}
	var fields map[string]jsontext.Value
	if err := json.Unmarshal(data, &fields); err != nil {
		return RuntimeLocator{}, err
	}
	if locator.Version == 0 {
		if _, present := fields["version"]; present {
			return RuntimeLocator{}, errors.New("legacy locator must not declare a version")
		}
		if _, present := fields["integrity_sha256"]; present {
			return RuntimeLocator{}, errors.New("legacy locator must not declare an integrity seal")
		}
	} else if !aliasFields(asObject(locator), "version", "source", "home", "codex", "state", "command_path", "integrity_sha256") {
		return RuntimeLocator{}, errors.New("version-two locator requires every sealed root")
	}
	if _, present := fields["command_path"]; present && locator.CommandPath != i.commandPath() {
		return RuntimeLocator{}, errors.New("unsafe native locator command path")
	}
	return locator, nil
}

func (i *Installer) runtimeRelease(revision string) (RuntimeIdentity, error) {
	dir := i.runtimeDir(revision)
	if !commitSHA.MatchString(revision) {
		return RuntimeIdentity{}, errors.New("invalid runtime revision")
	}
	if err := realDirectory(dir, false); err != nil {
		return RuntimeIdentity{}, err
	}
	data, err := readJSON(filepath.Join(dir, "receipt.json"))
	if err != nil {
		return RuntimeIdentity{}, err
	}
	var receipt runtimeReceipt
	if err = json.Unmarshal(legacyJSON(data), &receipt, json.RejectUnknownMembers(true)); err != nil {
		return RuntimeIdentity{}, err
	}
	if (receipt.Version != 1 && receipt.Version != 2) || !validRuntimeIdentity(receipt.Identity) || receipt.Identity.Revision != revision {
		return RuntimeIdentity{}, errors.New("invalid native runtime receipt")
	}
	if receipt.Version == 2 {
		if err := verifySeal(data); err != nil || receipt.ManagerProtocol != "cw-manager-v6" {
			return RuntimeIdentity{}, errors.New("invalid v6 runtime receipt seal or capability")
		}
	} else {
		if _, present := data["manager_protocol"]; present {
			return RuntimeIdentity{}, errors.New("legacy runtime receipt has unexpected capability fields")
		}
		if _, present := data["integrity_sha256"]; present {
			return RuntimeIdentity{}, errors.New("legacy runtime receipt has unexpected integrity seal")
		}
	}
	info, err := os.Lstat(filepath.Join(dir, "cw"))
	if err != nil {
		return RuntimeIdentity{}, err
	}
	if !info.Mode().IsRegular() || unixMode(info.Mode()) != 0o755 {
		return RuntimeIdentity{}, errors.New("native runtime is not a regular executable")
	}
	binary, err := os.ReadFile(filepath.Join(dir, "cw"))
	if err != nil {
		return RuntimeIdentity{}, err
	}
	if hash(binary) != receipt.SHA256 {
		return RuntimeIdentity{}, errors.New("native runtime content differs from receipt")
	}
	locatorBytes, err := readRecordBytes(filepath.Join(dir, "locator.json"))
	if err != nil {
		return RuntimeIdentity{}, err
	}
	locatorInfo, err := os.Lstat(filepath.Join(dir, "locator.json"))
	if err != nil || unixMode(locatorInfo.Mode()) != 0o600 {
		return RuntimeIdentity{}, errors.New("native runtime locator mode changed")
	}
	if _, err = i.decodeRuntimeLocator(locatorBytes); err != nil {
		return RuntimeIdentity{}, err
	}
	if hash(locatorBytes) != receipt.LocatorSHA256 {
		return RuntimeIdentity{}, errors.New("native runtime locator differs from its immutable receipt or roots")
	}
	if receipt.Version == 2 {
		var locator RuntimeLocator
		if err := json.Unmarshal(locatorBytes, &locator); err != nil || locator.Version != 2 {
			return RuntimeIdentity{}, errors.New("v6 runtime receipt requires a sealed version-two locator")
		}
		entries, err := os.ReadDir(dir)
		if err != nil || len(entries) != 3 {
			return RuntimeIdentity{}, errors.New("v6 runtime release contains unexpected files")
		}
		for _, entry := range entries {
			if entry.Name() != "cw" && entry.Name() != "locator.json" && entry.Name() != "receipt.json" {
				return RuntimeIdentity{}, errors.New("v6 runtime release contains unexpected files")
			}
		}
		info, err := os.Lstat(filepath.Join(dir, "receipt.json"))
		if err != nil || unixMode(info.Mode()) != 0o600 {
			return RuntimeIdentity{}, errors.New("v6 runtime receipt mode changed")
		}
	}
	return receipt.Identity, nil
}

func (i *Installer) stageRuntime() error {
	if err := i.Context.Err(); err != nil {
		return err
	}
	identity := i.RuntimeIdentity
	if !validRuntimeIdentity(identity) {
		return errors.New("invalid native runtime identity")
	}
	protocol, err := i.inspectRuntimeProtocol(i.RuntimeCandidate)
	if err != nil {
		return err
	}
	i.runtimeProtocol = protocol
	if i.AdoptPersonalSkills && protocol != "cw-manager-v6" {
		return errors.New("personal skill adoption requires a verified native v6 runtime")
	}
	info, err := os.Lstat(i.RuntimeCandidate)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
		return errors.New("native runtime candidate must be a regular executable")
	}
	binary, err := os.ReadFile(i.RuntimeCandidate)
	if err != nil {
		return err
	}
	dir := i.runtimeDir(identity.Revision)
	if exists(dir) {
		owned, err := i.runtimeRelease(identity.Revision)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(filepath.Join(dir, "cw"))
		if err != nil {
			return err
		}
		if owned != identity || !bytes.Equal(data, binary) {
			return errors.New("native runtime revision already has different immutable content")
		}
		return nil
	}
	parent := filepath.Dir(dir)
	if err = durableDirectory(parent, syncDir); err != nil {
		return err
	}
	temporary, err := os.MkdirTemp(parent, ".runtime-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(temporary)
	if err = atomicWrite(filepath.Join(temporary, "cw"), binary, 0o755); err != nil {
		return err
	}
	locator := i.runtimeLocatorBytes()
	if err = atomicWrite(filepath.Join(temporary, "locator.json"), locator, 0o600); err != nil {
		return err
	}
	receipt := runtimeReceipt{Version: 1, Identity: identity, SHA256: hash(binary), LocatorSHA256: hash(locator)}
	receiptBytes := legacyJSON(receipt)
	if protocol == "cw-manager-v6" {
		receipt.Version, receipt.ManagerProtocol = 2, protocol
		receiptBytes = legacyJSON(seal(asObject(receipt)))
	}
	if err = atomicWrite(filepath.Join(temporary, "receipt.json"), receiptBytes, 0o600); err != nil {
		return err
	}
	if err = i.Context.Err(); err != nil {
		return err
	}
	if err = os.Rename(temporary, dir); err != nil {
		return err
	}
	return syncDir(parent)
}

func pathSegment(paths Paths, leading string) string {
	bin := filepath.Join(paths.Home, ".local", "bin")
	return leading + profileStart + "\ncase \":$PATH:\" in\n  *" + aliasQuote(":"+bin+":") + "*) ;;\n  *) export PATH=" + aliasQuote(bin) + ":\"$PATH\" ;;\nesac\n" + profileEnd + "\n"
}

func (i *Installer) validateManager(value Object) error {
	var manager ManagerRecord
	if err := json.Unmarshal(legacyJSON(value), &manager, json.RejectUnknownMembers(true)); err != nil {
		return err
	}
	if manager.Version != 1 || manager.Source == "" || Normalize(manager.Source) != manager.Source || !validRuntimeIdentity(manager.Runtime) || manager.CommandPath != i.commandPath() || manager.CommandTarget != filepath.Join(i.runtimeCurrent(), "cw") {
		return errors.New("invalid native manager ownership")
	}
	if manager.Profile != nil {
		if !aliasFields(object(value["profile"]), "shell", "path", "segment", "original_exists", "original_mode") {
			return errors.New("invalid PATH profile fields")
		}
		profile := manager.Profile
		path, err := aliasRCPath(i.Paths, profile.Shell)
		if err != nil {
			return err
		}
		if profile.Path != path || profile.OriginalMode < 0 || profile.OriginalMode > 0o7777 || (profile.Segment != pathSegment(i.Paths, "") && profile.Segment != pathSegment(i.Paths, "\n")) {
			return errors.New("invalid PATH profile ownership")
		}
	}
	if raw, present := value["profile"]; present && raw == nil {
		return errors.New("null PATH profile ownership")
	}
	return nil
}

func (i *Installer) verifyManager(value Object) error {
	if err := i.validateManager(value); err != nil {
		return err
	}
	var manager ManagerRecord
	if err := json.Unmarshal(legacyJSON(value), &manager); err != nil {
		return err
	}
	identity, err := i.runtimeRelease(manager.Runtime.Revision)
	if err != nil {
		return err
	}
	if identity != manager.Runtime {
		return errors.New("native runtime identity differs from ownership")
	}
	for path, target := range map[string]string{i.commandPath(): manager.CommandTarget, i.runtimeCurrent(): i.runtimeDir(manager.Runtime.Revision)} {
		if err := realDirectory(filepath.Dir(path), false); err != nil {
			return err
		}
		actual, err := observe(path)
		if err != nil {
			return err
		}
		if !equal(actual, Object{"kind": "symlink", "target": target}) {
			return fmt.Errorf("owned native command changed at %s", path)
		}
	}
	locatorBytes, err := readRecordBytes(i.locatorPath())
	if err != nil {
		return err
	}
	locator, err := i.decodeRuntimeLocator(locatorBytes)
	if err != nil {
		return err
	}
	if locator.Source != manager.Source {
		return errors.New("native locator roots changed")
	}
	if manager.Profile != nil {
		data, exists, _, err := aliasRead(manager.Profile.Path)
		if err != nil {
			return err
		}
		if !exists {
			return errors.New("owned PATH profile disappeared")
		}
		_, _, err = profileLocate(data, manager.Profile.Segment)
		return err
	}
	return nil
}

func profileLocate(data []byte, expected string) (int, int, error) {
	if strings.Contains(expected, aliasStart) {
		start, end, err := aliasLocate(data, expected)
		if err != nil {
			return 0, 0, err
		}
		remainder := append(bytes.Clone(data[:start]), data[end:]...)
		if bytes.Contains(remainder, []byte("agent-workflows PATH")) {
			return 0, 0, errors.New("unidentified PATH markers beside legacy wrapper")
		}
		return start, end, nil
	}
	if bytes.Count(data, []byte(profileStart)) != 1 || bytes.Count(data, []byte(profileEnd)) != 1 {
		return 0, 0, errors.New("startup file must contain one owned PATH marker pair")
	}
	start := bytes.Index(data, []byte(profileStart))
	end := bytes.Index(data, []byte(profileEnd)) + len(profileEnd) + 1
	begin := start - (len(expected) - len(strings.TrimLeft(expected, "\n")))
	if begin < 0 || end > len(data) || (start > 0 && data[start-1] != '\n') || string(data[begin:end]) != expected {
		return 0, 0, errors.New("owned PATH profile changed")
	}
	remainder := append(bytes.Clone(data[:begin]), data[end:]...)
	if bytes.Contains(remainder, []byte("agent-workflows PATH")) {
		return 0, 0, errors.New("unidentified PATH profile markers")
	}
	if err := aliasUnowned(remainder); err != nil {
		return 0, 0, err
	}
	return begin, end, nil
}

func (i *Installer) managerPlan(state Object) (Object, []Object, error) {
	if i.RuntimeCandidate == "" {
		return object(state["manager"]), nil, nil
	}
	if !validRuntimeIdentity(i.RuntimeIdentity) {
		return nil, nil, errors.New("invalid native runtime identity")
	}
	old := object(state["manager"])
	if old != nil {
		if err := i.verifyManager(old); err != nil {
			return nil, nil, err
		}
		if old["source"] != i.Source {
			return nil, nil, errors.New("native manager belongs to another source checkout")
		}
	}
	if sharedConfigStateRequired(state) {
		if err := i.verifySharedConfigRuntime(i.RuntimeCandidate); err != nil {
			return nil, nil, err
		}
	}
	if legacy := object(state["command_alias"]); legacy != nil && legacy["source"] != i.Source {
		return nil, nil, errors.New("cw belongs to another source checkout; use the enrolled checkout")
	}
	if old == nil {
		if command, err := exec.LookPath("cw"); command != "" && (err == nil || errors.Is(err, exec.ErrDot)) && Normalize(command) != i.commandPath() {
			return nil, nil, fmt.Errorf("another cw command is on PATH: %s", command)
		}
	}
	manager := ManagerRecord{Version: 1, Source: i.Source, Runtime: i.RuntimeIdentity, CommandPath: i.commandPath(), CommandTarget: filepath.Join(i.runtimeCurrent(), "cw")}
	ops := []Object{}
	for _, path := range []string{i.runtimeCurrent(), i.commandPath()} {
		if err := realDirectory(filepath.Dir(path), false); err != nil {
			return nil, nil, err
		}
		target := manager.CommandTarget
		if path == i.runtimeCurrent() {
			target = i.runtimeDir(i.RuntimeIdentity.Revision)
		}
		before, err := observe(path)
		if err != nil {
			return nil, nil, err
		}
		if old == nil && before["kind"] != "absent" {
			return nil, nil, fmt.Errorf("native command path is occupied: %s", path)
		}
		after := Object{"kind": "symlink", "target": target}
		if !equal(before, after) {
			ops = append(ops, managerPathOperation(path, before, after, i.managerPathLabel(path)))
		}
	}
	locatorBefore, err := observe(i.locatorPath())
	if err != nil {
		return nil, nil, err
	}
	if old == nil && locatorBefore["kind"] != "absent" {
		return nil, nil, errors.New("native locator exists without ownership")
	}
	locatorAfter := Object{"kind": "file", "data": encode(i.runtimeLocatorBytes()), "mode": 0o600}
	if !equal(locatorBefore, locatorAfter) {
		ops = append([]Object{managerPathOperation(i.locatorPath(), locatorBefore, locatorAfter, "manager-locator")}, ops...)
	}
	if old != nil {
		if raw := object(old["profile"]); raw != nil {
			if err := json.Unmarshal(legacyJSON(raw), &manager.Profile); err != nil {
				return nil, nil, err
			}
		}
		if i.Shell != "auto" && (manager.Profile == nil || manager.Profile.Shell != i.Shell) {
			if manager.Profile != nil {
				removal, err := i.profileRemoval(manager.Profile, text(old["source"]))
				if err != nil {
					return nil, nil, err
				}
				ops = append(ops, removal)
			}
			profile, changes, err := i.prepareProfile(nil)
			if err != nil {
				return nil, nil, err
			}
			manager.Profile = profile
			ops = append(ops, changes...)
		}
	} else {
		profile, changes, err := i.prepareProfile(object(state["command_alias"]))
		if err != nil {
			return nil, nil, err
		}
		manager.Profile = profile
		ops = append(ops, changes...)
	}
	return asObject(manager), ops, nil
}

func managerPathOperation(path string, before, after Object, label string) Object {
	op := pathOperation(path, before, after, label)
	op["kind"] = "manager_path"
	return op
}

func (i *Installer) managerPathLabel(path string) string {
	switch path {
	case i.runtimeCurrent():
		return "manager-runtime"
	case i.commandPath():
		return "manager-command"
	default:
		return "manager-locator"
	}
}

func (i *Installer) prepareProfile(legacy Object) (*ProfileRecord, []Object, error) {
	if legacy == nil || i.Shell == "auto" || legacy["shell"] == i.Shell {
		profile, op, err := i.prepareSingleProfile(legacy)
		if err != nil {
			return nil, nil, err
		}
		if op == nil {
			return profile, nil, nil
		}
		return profile, []Object{op}, nil
	}
	if err := AliasVerify(legacy, i.Paths); err != nil {
		return nil, nil, err
	}
	originalMode, _ := aliasInteger(legacy["original_mode"])
	record := &ProfileRecord{Shell: text(legacy["shell"]), Path: text(legacy["path"]), Segment: text(legacy["segment"]), OriginalExists: legacy["original_exists"].(bool), OriginalMode: originalMode}
	removal, err := i.profileRemoval(record, text(legacy["source"]))
	if err != nil {
		return nil, nil, err
	}
	profile, op, err := i.prepareSingleProfile(nil)
	if err != nil {
		return nil, nil, err
	}
	changes := []Object{removal}
	if op != nil {
		changes = append(changes, op)
	}
	return profile, changes, nil
}

func (i *Installer) prepareSingleProfile(legacy Object) (*ProfileRecord, Object, error) {
	var shell, path string
	var beforeSegment any
	var originalExists bool
	var originalMode int
	if legacy != nil {
		if err := AliasVerify(legacy, i.Paths); err != nil {
			return nil, nil, err
		}
		shell, path = text(legacy["shell"]), text(legacy["path"])
		beforeSegment = legacy["segment"]
		originalExists, _ = legacy["original_exists"].(bool)
		originalMode, _ = aliasInteger(legacy["original_mode"])
	} else {
		shell, _ = aliasSelectedShell(i.Shell, i.Paths, map[string]string{"SHELL": os.Getenv("SHELL"), "ZDOTDIR": os.Getenv("ZDOTDIR")})
		if shell == "" {
			return nil, nil, nil
		}
		var err error
		path, err = aliasRCPath(i.Paths, shell)
		if err != nil {
			return nil, nil, err
		}
	}
	data, exists, mode, err := aliasRead(path)
	if err != nil {
		return nil, nil, err
	}
	leading := ""
	if beforeSegment != nil {
		if _, _, err = profileLocate(data, beforeSegment.(string)); err != nil {
			return nil, nil, err
		}
		if strings.HasPrefix(beforeSegment.(string), "\n") {
			leading = "\n"
		}
	} else {
		if err = aliasUnowned(data); err != nil {
			return nil, nil, err
		}
		if bytes.Contains(data, []byte("agent-workflows PATH")) {
			return nil, nil, errors.New("preexisting PATH markers need ownership state")
		}
		originalExists, originalMode = exists, mode
		if len(data) > 0 && !bytes.HasSuffix(data, []byte("\n")) {
			leading = "\n"
		}
	}
	if pathIncludes(os.Getenv("PATH"), filepath.Join(i.Home, ".local", "bin")) {
		if legacy == nil {
			return nil, nil, nil
		}
		return nil, Object{"kind": "command_profile", "path": path, "shell": shell, "source": legacy["source"], "before_segment": beforeSegment, "after_segment": nil, "before_exists": exists, "after_exists": originalExists, "before_mode": mode, "after_mode": originalMode, "label": "command-profile"}, nil
	}
	profile := &ProfileRecord{Shell: shell, Path: path, Segment: pathSegment(i.Paths, leading), OriginalExists: originalExists, OriginalMode: originalMode}
	op := Object{"kind": "command_profile", "path": path, "shell": shell, "source": i.Source, "before_segment": beforeSegment, "after_segment": profile.Segment, "before_exists": exists, "after_exists": true, "before_mode": mode, "after_mode": mode, "label": "command-profile"}
	if legacy != nil {
		op["source"] = legacy["source"]
	}
	return profile, op, nil
}

func pathIncludes(value, directory string) bool {
	for _, candidate := range filepath.SplitList(value) {
		if Normalize(candidate) == directory {
			return true
		}
	}
	return false
}

func (i *Installer) validateManagerPath(op Object) error {
	if !aliasFields(op, "kind", "path", "before", "after", "label") || op["kind"] != "manager_path" {
		return errors.New("invalid native manager operation")
	}
	path := text(op["path"])
	if path != i.commandPath() && path != i.runtimeCurrent() && path != i.locatorPath() {
		return errors.New("unsafe native manager operation path")
	}
	if err := realDirectory(filepath.Dir(path), false); err != nil {
		return err
	}
	for _, side := range []string{"before", "after"} {
		ob := object(op[side])
		if err := validateObservation(ob, false); err != nil {
			return err
		}
		if ob["kind"] == "absent" {
			continue
		}
		switch path {
		case i.commandPath():
			if ob["kind"] != "symlink" || ob["target"] != filepath.Join(i.runtimeCurrent(), "cw") {
				return errors.New("unsafe native command target")
			}
		case i.runtimeCurrent():
			target := text(ob["target"])
			if ob["kind"] != "symlink" || filepath.Dir(target) != filepath.Join(i.State, "runtime", "releases") {
				return errors.New("unsafe native runtime pointer")
			}
			if _, err := i.runtimeRelease(filepath.Base(target)); err != nil {
				return err
			}
		case i.locatorPath():
			if ob["kind"] != "file" {
				return errors.New("unsafe native locator observation")
			}
			data, err := decode(ob["data"])
			if err != nil {
				return err
			}
			if _, err = i.decodeRuntimeLocator(data); err != nil {
				return err
			}
		}
	}
	return nil
}

func (i *Installer) validateProfileOperation(op Object) error {
	if !aliasFields(op, "kind", "path", "shell", "source", "before_segment", "after_segment", "before_exists", "after_exists", "before_mode", "after_mode", "label") || op["kind"] != "command_profile" {
		return errors.New("invalid PATH profile operation")
	}
	path, err := aliasRCPath(i.Paths, op["shell"])
	if err != nil {
		return err
	}
	if op["path"] != path {
		return errors.New("unsafe PATH profile operation path")
	}
	source := text(op["source"])
	if source == "" || Normalize(source) != source {
		return errors.New("unsafe PATH profile source")
	}
	paths := i.Paths
	paths.Source = source
	for _, side := range []string{"before", "after"} {
		if _, ok := op[side+"_exists"].(bool); !ok {
			return errors.New("invalid PATH profile existence")
		}
		if _, ok := aliasInteger(op[side+"_mode"]); !ok {
			return errors.New("invalid PATH profile mode")
		}
		if raw := op[side+"_segment"]; raw != nil {
			segment, ok := raw.(string)
			if !ok || (segment != pathSegment(paths, "") && segment != pathSegment(paths, "\n") && segment != aliasSegment(paths, "") && segment != aliasSegment(paths, "\n")) {
				return errors.New("invalid PATH profile segment")
			}
			if !op[side+"_exists"].(bool) {
				return errors.New("absent PATH profile has a segment")
			}
		}
	}
	if op["before_segment"] == op["after_segment"] {
		return errors.New("unchanged PATH profile operation")
	}
	_, _, _, err = aliasRead(path)
	return err
}

func profileCurrent(data []byte, op Object) (any, error) {
	if !bytes.Contains(data, []byte(aliasMarker)) && !bytes.Contains(data, []byte("agent-workflows PATH")) {
		return nil, aliasUnowned(data)
	}
	for _, side := range []string{"before_segment", "after_segment"} {
		if segment := op[side]; segment != nil {
			if _, _, err := profileLocate(data, segment.(string)); err == nil {
				return segment, nil
			}
		}
	}
	return nil, errors.New("owned command profile changed")
}

func (i *Installer) renderProfile(op Object) ([]byte, os.FileMode, bool, error) {
	if err := i.validateProfileOperation(op); err != nil {
		return nil, 0, false, err
	}
	data, exists, mode, err := aliasRead(text(op["path"]))
	if err != nil {
		return nil, 0, false, err
	}
	current, err := profileCurrent(data, op)
	if err != nil {
		return nil, 0, false, err
	}
	if current != op["before_segment"] {
		return nil, 0, false, errors.New("owned command profile changed during mutation")
	}
	if op["label"] == "command-profile" && current == nil && exists != op["before_exists"] {
		return nil, 0, false, errors.New("startup file existence changed during enrollment")
	}
	var target []byte
	if op["after_segment"] != nil {
		target = []byte(op["after_segment"].(string))
	}
	var result []byte
	if current == nil {
		if len(data) == 0 || bytes.HasSuffix(data, []byte("\n")) || bytes.HasPrefix(target, []byte("\n")) {
			result = append(bytes.Clone(data), target...)
		} else {
			result = append(bytes.Clone(target), data...)
		}
	} else {
		start, end, err := profileLocate(data, current.(string))
		if err != nil {
			return nil, 0, false, err
		}
		result = append(append(bytes.Clone(data[:start]), target...), data[end:]...)
	}
	remove := !op["after_exists"].(bool) && len(result) == 0
	beforeMode, _ := aliasInteger(op["before_mode"])
	if remove && exists && mode != beforeMode {
		return nil, 0, false, errors.New("startup mode changed before removal")
	}
	if !exists {
		mode, _ = aliasInteger(op["after_mode"])
	}
	return result, aliasFileMode(mode), remove, nil
}

func (i *Installer) profileReversal(op Object) (Object, error) {
	if err := i.validateProfileOperation(op); err != nil {
		return nil, err
	}
	data, exists, mode, err := aliasRead(text(op["path"]))
	if err != nil {
		return nil, err
	}
	current, err := profileCurrent(data, op)
	if err != nil {
		return nil, err
	}
	if current == op["before_segment"] {
		return nil, nil
	}
	if current != op["after_segment"] {
		return nil, errors.New("command profile changed; refusing recovery")
	}
	reversal := clone(op)
	reversal["before_segment"], reversal["after_segment"] = op["after_segment"], op["before_segment"]
	reversal["before_exists"], reversal["after_exists"] = exists, op["before_exists"]
	reversal["before_mode"], reversal["after_mode"] = mode, op["before_mode"]
	reversal["label"] = "recover-command-profile"
	return reversal, nil
}

func (i *Installer) profilePending(op Object) (Object, error) {
	if err := i.validateProfileOperation(op); err != nil {
		return nil, err
	}
	data, _, _, err := aliasRead(text(op["path"]))
	if err != nil {
		return nil, err
	}
	current, err := profileCurrent(data, op)
	if err != nil {
		return nil, err
	}
	if current == op["after_segment"] {
		return nil, nil
	}
	if current != op["before_segment"] {
		return nil, errors.New("command profile changed during interrupted recovery")
	}
	return op, nil
}

func (i *Installer) managerRemoval(value Object) ([]Object, error) {
	if err := i.verifyManager(value); err != nil {
		return nil, err
	}
	var manager ManagerRecord
	if err := json.Unmarshal(legacyJSON(value), &manager); err != nil {
		return nil, err
	}
	ops := []Object{}
	if profile := manager.Profile; profile != nil {
		removal, err := i.profileRemoval(profile, manager.Source)
		if err != nil {
			return nil, err
		}
		ops = append(ops, removal)
	}
	for _, path := range []string{i.commandPath(), i.runtimeCurrent(), i.locatorPath()} {
		before, err := observe(path)
		if err != nil {
			return nil, err
		}
		ops = append(ops, managerPathOperation(path, before, Object{"kind": "absent"}, i.managerPathLabel(path)))
	}
	return ops, nil
}

func (i *Installer) profileRemoval(profile *ProfileRecord, source string) (Object, error) {
	data, exists, mode, err := aliasRead(profile.Path)
	if err != nil {
		return nil, err
	}
	if _, _, err = profileLocate(data, profile.Segment); err != nil {
		return nil, err
	}
	return Object{"kind": "command_profile", "path": profile.Path, "shell": profile.Shell, "source": source, "before_segment": profile.Segment, "after_segment": nil, "before_exists": exists, "after_exists": profile.OriginalExists, "before_mode": mode, "after_mode": profile.OriginalMode, "label": "command-profile"}, nil
}
