package workflow

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const startMarker = "<!-- agent-workflows-start -->"
const endMarker = "<!-- agent-workflows-end -->"

type Options struct {
	Paths
	Context                    context.Context
	RuntimeCandidate           string
	RuntimeIdentity            RuntimeIdentity
	PrepareRuntime             RuntimePreparer
	PrepareConsultationRuntime ConsultationRuntimePreparer
	Apply                      bool
	NoCheckout                 bool
	Shell                      string
	AdoptPersonalSkills        bool
}
type Installer struct {
	Options
	current, statePath, journalPath, agents, config, skills string
	runtimeProtocol                                         string
}

func NewInstaller(options Options) *Installer {
	if options.Context == nil {
		options.Context = context.Background()
	}
	p := options.Paths
	p.Source = Normalize(p.Source)
	p.Home = Normalize(p.Home)
	p.Codex = Normalize(p.Codex)
	p.State = Normalize(p.State)
	options.Paths = p
	return &Installer{Options: options, current: filepath.Join(p.State, "current"), statePath: filepath.Join(p.State, "state.json"), journalPath: filepath.Join(p.State, "journal.json"), agents: filepath.Join(p.Codex, "AGENTS.md"), config: filepath.Join(p.Codex, "config.toml"), skills: filepath.Join(p.Home, ".agents", "skills")}
}
func managedBlock(template []byte, leading []byte) []byte {
	out := append(bytes.Clone(leading), []byte(startMarker+"\n")...)
	out = append(out, template...)
	if !bytes.HasSuffix(template, []byte("\n")) {
		out = append(out, '\n')
	}
	return append(out, []byte(endMarker+"\n")...)
}
func hasMarkers(data []byte) bool {
	return bytes.Contains(data, []byte("agent-workflows-start")) || bytes.Contains(data, []byte("agent-workflows-end"))
}
func locateBlock(data []byte) (int, int, error) {
	if bytes.Count(data, []byte(startMarker)) != 1 || bytes.Count(data, []byte(endMarker)) != 1 {
		return 0, 0, errors.New("AGENTS.md must contain exactly one valid managed marker pair")
	}
	start := bytes.Index(data, []byte(startMarker))
	end := bytes.Index(data, []byte(endMarker))
	tail := end + len(endMarker)
	if end <= start || (start > 0 && data[start-1] != '\n') || start+len(startMarker) >= len(data) || data[start+len(startMarker)] != '\n' || tail >= len(data) || data[tail] != '\n' {
		return 0, 0, errors.New("malformed managed block in AGENTS.md")
	}
	return start, tail + 1, nil
}
func replaceOwned(data, expected, replacement []byte) ([]byte, error) {
	start, end, e := locateBlock(data)
	if e != nil {
		return nil, e
	}
	leading := len(expected) - len(bytes.TrimLeft(expected, "\n"))
	begin := start - leading
	if begin < 0 || !bytes.Equal(data[begin:end], expected) {
		return nil, errors.New("owned AGENTS.md block was modified; refusing overwrite")
	}
	out := append(bytes.Clone(data[:begin]), replacement...)
	return append(out, data[end:]...), nil
}
func (i *Installer) state(required bool) (Object, error) {
	if e := realDirectory(i.State, false); e != nil {
		return nil, e
	}
	if !exists(i.statePath) {
		if required {
			return nil, errors.New("no installation state exists")
		}
		return nil, nil
	}
	s, e := readOwnership(i.statePath)
	if e != nil {
		return nil, e
	}
	if e = verifySeal(s); e != nil {
		return nil, e
	}
	if integer(s["version"]) != 2 || !commitSHA.MatchString(text(s["release"])) || s["home"] != i.Home || s["codex_home"] != i.Codex || s["state_dir"] != i.State || object(s["registrations"]) == nil || object(s["manifest_registrations"]) == nil || sequence(s["history"]) == nil {
		return nil, errors.New("unsupported or corrupt installation ownership state or roots")
	}
	if _, e = decode(s["global_segment"]); e != nil {
		return nil, e
	}
	if e = i.validateStateStructure(s); e != nil {
		return nil, e
	}
	return s, nil
}

func (i *Installer) validateStateStructure(s Object) error {
	if _, err := decodeOwnership(s); err != nil {
		return err
	}
	if integer(s["version"]) != 2 || s["home"] != i.Home || s["codex_home"] != i.Codex || s["state_dir"] != i.State || !commitSHA.MatchString(text(s["release"])) {
		return errors.New("corrupt installation state or roots")
	}
	for _, key := range []string{"model_config", "shared_config"} {
		if value, present := s[key]; present {
			metadata, err := requiredObject(value, key)
			if err != nil {
				return err
			}
			if key == "model_config" {
				err = ModelValidateMetadata(metadata)
			} else {
				err = SharedConfigValidateMetadata(metadata)
			}
			if err != nil {
				return err
			}
		}
	}
	if raw, present := s["config_original_exists"]; present {
		origin, ok := raw.(bool)
		if !ok || (s["model_config"] == nil && s["shared_config"] == nil) {
			return errors.New("corrupt shared Codex config file origin")
		}
		for _, field := range []string{"model_config", "shared_config"} {
			if metadata := object(s[field]); metadata != nil && origin && metadata["original_exists"] == false {
				return errors.New("shared Codex config file origin contradicts ownership")
			}
		}
	}
	if s["shared_config"] != nil {
		if _, ok := s["config_original_exists"].(bool); !ok {
			return errors.New("shared Codex config ownership requires its file origin")
		}
	}
	if raw, present := s["manager"]; present {
		manager, err := requiredObject(raw, "manager")
		if err != nil {
			return err
		}
		if err = i.validateManager(manager); err != nil {
			return err
		}
		if s["source"] != manager["source"] {
			return errors.New("native manager and ownership source differ")
		}
	}
	origin := object(s["global_origin"])
	if origin == nil || (origin["kind"] != "absent" && origin["kind"] != "unmanaged") {
		return errors.New("corrupt global instruction origin")
	}
	if _, err := decode(s["global_segment"]); err != nil {
		return err
	}
	registrations, manifest := object(s["registrations"]), object(s["manifest_registrations"])
	if registrations == nil || manifest == nil || len(registrations) != len(manifest) {
		return errors.New("corrupt registration ownership")
	}
	for name, raw := range registrations {
		item := object(raw)
		if !registrationName(name) || !registrationRoot(text(manifest[name])) || item == nil || item["target"] != i.target(text(manifest[name])) {
			return errors.New("corrupt registration ownership record")
		}
		original := object(item["original"])
		if original == nil {
			return errors.New("corrupt original registration")
		}
		switch original["kind"] {
		case "absent":
			if err := validateObservation(original, false); err != nil {
				return err
			}
		case "symlink":
			if err := validateObservation(original, false); err != nil {
				return err
			}
			if _, ok := original["target"].(string); !ok {
				return errors.New("corrupt original symlink")
			}
		case "personal":
			if _, err := i.validatePersonalOrigin(name, original); err != nil {
				return err
			}
		default:
			return errors.New("unsupported original registration")
		}
	}
	history, ok := s["history"].([]any)
	if !ok {
		return errors.New("corrupt activation history")
	}
	for _, raw := range history {
		previous := object(raw)
		if previous == nil || !commitSHA.MatchString(text(previous["release"])) {
			return errors.New("corrupt prior release")
		}
		if _, err := decode(previous["global_segment"]); err != nil {
			return err
		}
	}
	return nil
}
func (i *Installer) preview(sha string) (string, Object, func(), error) {
	dir, e := os.MkdirTemp("", "agent-workflows-preview-")
	if e != nil {
		return "", nil, nil, e
	}
	cleanup := func() { os.RemoveAll(dir) }
	m, e := exportCommitContext(i.Context, i.Source, sha, dir)
	if e != nil {
		cleanup()
		return "", nil, nil, e
	}
	return dir, m, cleanup, nil
}
func (i *Installer) release(sha string) (string, Object, error) {
	if !commitSHA.MatchString(sha) {
		return "", nil, errors.New("invalid release SHA")
	}
	dir := filepath.Join(i.State, "releases", sha)
	if e := realDirectory(dir, false); e != nil {
		return "", nil, e
	}
	receipt, e := readJSON(dir + ".receipt.json")
	if e != nil {
		return "", nil, e
	}
	contents, e := inventory(dir)
	if e != nil {
		return "", nil, e
	}
	if receipt["sha"] != sha || !equal(receipt["inventory"], contents) {
		return "", nil, fmt.Errorf("missing or modified immutable release: %s", sha)
	}
	m, e := LoadManifestValidated(dir)
	return dir, m, e
}
func (i *Installer) stage(sha string) (string, Object, error) {
	parent := filepath.Join(i.State, "releases")
	if e := durableDirectory(parent, syncDir); e != nil {
		return "", nil, e
	}
	dir := filepath.Join(parent, sha)
	receipt := dir + ".receipt.json"
	if exists(dir) && !exists(receipt) {
		state, e := i.state(false)
		if e != nil {
			return "", nil, e
		}
		referenced := state != nil && state["release"] == sha
		for _, h := range sequence(state["history"]) {
			if object(h)["release"] == sha {
				referenced = true
			}
		}
		pointer, _ := os.Readlink(i.current)
		if referenced || pointer == dir {
			return "", nil, errors.New("missing receipt for a referenced immutable release")
		}
		candidate, m, clean, e := i.preview(sha)
		if e != nil {
			return "", nil, e
		}
		defer clean()
		actual, e := inventory(dir)
		if e != nil {
			return "", nil, e
		}
		expected, e := inventory(candidate)
		if e != nil {
			return "", nil, e
		}
		if !equal(actual, expected) {
			return "", nil, errors.New("unreceipted release differs from its exact Git export")
		}
		if _, e = LoadManifestValidated(dir); e != nil {
			return "", nil, e
		}
		e = atomicWrite(receipt, legacyJSON(Object{"sha": sha, "inventory": expected}), 0600)
		return dir, m, e
	}
	if exists(dir) || exists(receipt) {
		return i.release(sha)
	}
	temporary, e := os.MkdirTemp(parent, ".staging-")
	if e != nil {
		return "", nil, e
	}
	defer os.RemoveAll(temporary)
	m, e := exportCommitContext(i.Context, i.Source, sha, temporary)
	if e != nil {
		return "", nil, e
	}
	contents, e := inventory(temporary)
	if e != nil {
		return "", nil, e
	}
	if e = os.Rename(temporary, dir); e != nil {
		return "", nil, e
	}
	if e = syncDir(parent); e != nil {
		return "", nil, e
	}
	if e = fault("after-release-publication"); e != nil {
		return "", nil, e
	}
	e = atomicWrite(receipt, legacyJSON(Object{"sha": sha, "inventory": contents}), 0600)
	return dir, m, e
}
func registrationName(name string) bool {
	return name != "" && name != "." && name != ".." && !strings.ContainsAny(name, "/\\")
}

func registrationRoot(path string) bool {
	return path != "" && path != "." && path != ".." && !filepath.IsAbs(path) && filepath.Clean(path) == path && !strings.HasPrefix(path, "../") && !strings.ContainsAny(path, "\\\x00")
}
func (i *Installer) target(relative string) string { return filepath.Join(i.current, relative) }
func (i *Installer) readAgents() ([]byte, error) {
	o, e := observe(i.agents)
	if e != nil {
		return nil, e
	}
	if o["kind"] == "absent" {
		return []byte{}, nil
	}
	if o["kind"] != "file" {
		return nil, errors.New("AGENTS.md is occupied or symlinked")
	}
	return decode(o["data"])
}
func (i *Installer) duplicates(registrations Object) error {
	roots, err := i.discoveryRoots()
	if err != nil {
		return err
	}
	for _, root := range roots {
		if root == i.skills {
			continue
		}
		if e := realDirectory(root, false); e != nil {
			return e
		}
		for name := range registrations {
			if !registrationName(name) {
				return errors.New("unsafe registration name")
			}
			path := filepath.Join(root, name)
			if exists(path) {
				return fmt.Errorf("duplicate discovery registration exists: %s", path)
			}
		}
	}
	return nil
}
func (i *Installer) verifyOwned(s Object) error {
	if e := realDirectory(i.skills, false); e != nil {
		return e
	}
	if e := realDirectory(i.Codex, false); e != nil {
		return e
	}
	pointer, e := observe(i.current)
	if e != nil {
		return e
	}
	if !equal(pointer, i.expectedPointer(s)) {
		return errors.New("active release pointer is missing or changed")
	}
	release, m, e := i.release(text(s["release"]))
	if e != nil {
		return e
	}
	if !equal(m["registrations"], s["manifest_registrations"]) {
		return errors.New("release manifest differs from installation state")
	}
	segment, e := decode(s["global_segment"])
	if e != nil {
		return e
	}
	template, e := os.ReadFile(filepath.Join(release, text(m["global_instructions"])))
	if e != nil {
		return e
	}
	leading := segment[:len(segment)-len(bytes.TrimLeft(segment, "\n"))]
	if !bytes.Equal(segment, managedBlock(template, leading)) {
		return errors.New("owned global block differs from its validated release")
	}
	registrations := object(s["registrations"])
	if len(registrations) != len(object(s["manifest_registrations"])) {
		return errors.New("incomplete registration ownership record")
	}
	for name, raw := range registrations {
		item := object(raw)
		if !registrationName(name) || item == nil || item["target"] != i.target(text(object(s["manifest_registrations"])[name])) {
			return errors.New("corrupt registration ownership record")
		}
		actual, e := observe(filepath.Join(i.skills, name))
		if e != nil {
			return e
		}
		if !equal(actual, Object{"kind": "symlink", "target": item["target"]}) {
			return fmt.Errorf("owned registration changed: %s", name)
		}
		original := object(item["original"])
		if original["kind"] == "personal" {
			origin, err := i.validatePersonalOrigin(name, original)
			if err != nil {
				return err
			}
			if matches, err := matchesPersonalDirectory(origin.Backup, origin.Inventory); err != nil || !matches {
				return fmt.Errorf("missing or modified personal adoption backup: %s", name)
			}
			if origin.Path != filepath.Join(i.skills, name) && exists(origin.Path) {
				return fmt.Errorf("personal restore destination is occupied: %s", origin.Path)
			}
		}
	}
	data, e := i.readAgents()
	if e != nil {
		return e
	}
	if _, e = replaceOwned(data, segment, nil); e != nil {
		return e
	}
	if e = i.duplicates(object(s["manifest_registrations"])); e != nil {
		return e
	}
	if m := object(s["model_config"]); m != nil {
		if e = ModelVerify(i.config, m); e != nil {
			return e
		}
	}
	if shared := object(s["shared_config"]); shared != nil {
		if e = SharedConfigVerify(i.config, shared); e != nil {
			return e
		}
	}
	if manager := object(s["manager"]); manager != nil {
		return i.verifyManager(manager)
	}
	return nil
}
func pathOperation(path string, before, after Object, label string) Object {
	return Object{"kind": "path", "path": path, "before": before, "after": after, "label": label}
}
func (i *Installer) globalOperation(before, after, beforeSegment, afterSegment []byte, label string) (Object, error) {
	ob, e := observe(i.agents)
	if e != nil {
		return nil, e
	}
	captured := []byte{}
	if ob["kind"] == "file" {
		captured, e = decode(ob["data"])
		if e != nil {
			return nil, e
		}
	}
	if (ob["kind"] != "file" && ob["kind"] != "absent") || !bytes.Equal(captured, before) {
		return nil, errors.New("AGENTS.md changed while preparing its managed update")
	}
	mode := ob["mode"]
	if mode == nil {
		mode = 0600
	}
	var bs, as any
	if beforeSegment != nil {
		bs = encode(beforeSegment)
	}
	if afterSegment != nil {
		as = encode(afterSegment)
	}
	return Object{"kind": "global", "path": i.agents, "before": ob, "after": Object{"kind": "file", "data": encode(after), "mode": mode}, "before_segment": bs, "after_segment": as, "label": label}, nil
}
func (i *Installer) preflight(release string, m Object) (Object, Object, []byte, []Object, error) {
	s, e := i.state(false)
	if e != nil {
		return nil, nil, nil, nil, e
	}
	if s != nil || exists(i.current) {
		return nil, nil, nil, nil, errors.New("an installation already exists; use status or update")
	}
	if e = realDirectory(i.skills, false); e != nil {
		return nil, nil, nil, nil, e
	}
	if e = realDirectory(i.Codex, false); e != nil {
		return nil, nil, nil, nil, e
	}
	plan, e := i.registrationPlan(nil, m, false, release)
	if e != nil {
		return nil, nil, nil, nil, e
	}
	records, ops := plan.Records, plan.Operations
	before, e := i.readAgents()
	if e != nil {
		return nil, nil, nil, nil, e
	}
	template, e := os.ReadFile(filepath.Join(release, text(m["global_instructions"])))
	if e != nil {
		return nil, nil, nil, nil, e
	}
	ob, e := observe(i.agents)
	if e != nil {
		return nil, nil, nil, nil, e
	}
	mode := ob["mode"]
	if mode == nil {
		mode = 0600
	}
	origin := Object{"kind": "unmanaged", "mode": mode}
	if ob["kind"] == "absent" {
		origin["kind"] = "absent"
	}
	if hasMarkers(before) {
		return nil, nil, nil, nil, errors.New("preexisting managed markers require ownership state")
	}
	leading := []byte{}
	if len(before) > 0 && !bytes.HasSuffix(before, []byte("\n")) {
		leading = []byte("\n")
	}
	segment := managedBlock(template, leading)
	after := append(bytes.Clone(before), segment...)
	op, e := i.globalOperation(before, after, nil, segment, "global")
	if e != nil {
		return nil, nil, nil, nil, e
	}
	ops = append(ops, op)
	return records, origin, segment, ops, nil
}
func (i *Installer) expectedPointer(s Object) Object {
	return Object{"kind": "symlink", "target": filepath.Join(i.State, "releases", text(s["release"]))}
}
func expectedState(s Object) Object {
	return Object{"kind": "file", "data": encode(legacyJSON(s)), "mode": 0600}
}
func (i *Installer) newState(sha string, m, records, origin Object, segment []byte) Object {
	return seal(Object{"version": 2, "release": sha, "source": i.Source, "home": i.Home, "codex_home": i.Codex, "state_dir": i.State, "manifest_registrations": m["registrations"], "registrations": records, "global_origin": origin, "global_segment": encode(segment), "history": []any{}})
}
