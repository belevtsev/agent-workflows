package workflow

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const aliasStart = "# >>> agent-workflows cw >>>"
const aliasEnd = "# <<< agent-workflows cw <<<"
const aliasMarker = "agent-workflows cw"

var aliasComments = regexp.MustCompile(`(?m)^[ \t]*#.*$`)
var aliasFunctions = regexp.MustCompile(`\b(?:function\s+[^\n{]*\bcw\b|cw\s*\(\s*\)|functions\s*\[\s*["']?cw\b)`)
var aliasDeclarations = regexp.MustCompile(`\balias\s+([^\n;]*)`)
var aliasCWDeclaration = regexp.MustCompile(`(?:^|\s)["']?cw["']?\s*=`)
var aliasSafeQuote = regexp.MustCompile(`^[a-zA-Z0-9_@%+=:,./-]+$`)

func aliasSelectedShell(choice string, paths Paths, environment map[string]string) (string, string) {
	shell := choice
	if shell == "auto" {
		shell = environment["SHELL"]
		if shell == "" {
			shell = "bash"
		}
		shell = filepath.Base(shell)
	}
	if shell == "none" {
		return "", "disabled"
	}
	if shell != "bash" && shell != "zsh" {
		return "", fmt.Sprintf("unsupported shell %s; add cw manually", shell)
	}
	if shell == "zsh" && environment["ZDOTDIR"] != "" {
		location, err := filepath.Abs(environment["ZDOTDIR"])
		if err != nil || location != paths.Home {
			return "", "ZDOTDIR is set; add cw manually or unset ZDOTDIR for home .zshrc enrollment"
		}
	}
	return shell, "enrolled"
}

func aliasRCPath(paths Paths, shell any) (string, error) {
	if shell != "bash" && shell != "zsh" {
		return "", errors.New("Corrupt cw shell selection")
	}
	return filepath.Join(paths.Home, "."+shell.(string)+"rc"), nil
}

func aliasQuote(value string) string {
	if value == "" {
		return "''"
	}
	if aliasSafeQuote.MatchString(value) {
		return value
	}
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func aliasSegment(paths Paths, leading string) string {
	arguments := []string{filepath.Join(paths.Source, "install.sh"), "--home", paths.Home, "--codex-home", paths.Codex, "--state-dir", paths.State}
	for i, argument := range arguments {
		arguments[i] = aliasQuote(argument)
	}
	invocation := strings.Join(arguments, " ") + ` "$@"`
	owner := aliasQuote(invocation)
	script := "if command -v cw >/dev/null 2>&1; then\n" +
		`  if [ "${_AGENT_WORKFLOWS_CW_OWNER-}" != ` + owner + " ]; then\n" +
		"    printf '%s\\n' 'agent-workflows: cw already exists; keeping current command' >&2\n" +
		"  fi\nelse\n  function cw {\n    " + invocation + "\n  }\n" +
		"  typeset +x _AGENT_WORKFLOWS_CW_OWNER=" + owner + "\nfi\n"
	return leading + aliasStart + "\n" + script + aliasEnd + "\n"
}

func aliasRealParent(path string) error {
	parent := filepath.Dir(path)
	var parents []string
	for {
		parents = append(parents, parent)
		ancestor := filepath.Dir(parent)
		if ancestor == parent {
			break
		}
		parent = ancestor
	}
	for i := len(parents) - 1; i >= 0; i-- {
		info, err := os.Lstat(parents[i])
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("Unsafe shell startup directory: %s", parents[i])
		}
	}
	return nil
}

func aliasMode(info os.FileInfo) int {
	mode := int(info.Mode().Perm())
	if info.Mode()&os.ModeSetuid != 0 {
		mode |= 0o4000
	}
	if info.Mode()&os.ModeSetgid != 0 {
		mode |= 0o2000
	}
	if info.Mode()&os.ModeSticky != 0 {
		mode |= 0o1000
	}
	return mode
}

func aliasFileMode(mode int) os.FileMode {
	result := os.FileMode(mode & 0o777)
	if mode&0o4000 != 0 {
		result |= os.ModeSetuid
	}
	if mode&0o2000 != 0 {
		result |= os.ModeSetgid
	}
	if mode&0o1000 != 0 {
		result |= os.ModeSticky
	}
	return result
}

func aliasRead(path string) ([]byte, bool, int, error) {
	if err := aliasRealParent(path); err != nil {
		return nil, false, 0, err
	}
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, 0o600, nil
	}
	if err != nil {
		return nil, false, 0, err
	}
	if !info.Mode().IsRegular() {
		return nil, false, 0, fmt.Errorf("Shell startup path is occupied or symlinked: %s", path)
	}
	data, err := os.ReadFile(path)
	return data, true, aliasMode(info), err
}

func aliasHasDefinition(data []byte) bool {
	text := strings.ToValidUTF8(string(data), "\ufffd")
	text = aliasComments.ReplaceAllString(text, "")
	text = strings.ReplaceAll(text, "\\\n", "")
	if aliasFunctions.MatchString(text) {
		return true
	}
	for _, match := range aliasDeclarations.FindAllStringSubmatch(text, -1) {
		if aliasCWDeclaration.MatchString(match[1]) {
			return true
		}
	}
	return false
}

func aliasLocate(data []byte, expected string) (int, int, error) {
	if bytes.Count(data, []byte(aliasStart)) != 1 || bytes.Count(data, []byte(aliasEnd)) != 1 {
		return 0, 0, errors.New("Shell startup file must contain one owned cw marker pair")
	}
	start := bytes.Index(data, []byte(aliasStart))
	end := bytes.Index(data, []byte(aliasEnd)) + len(aliasEnd) + 1
	leading := len(expected) - len(strings.TrimLeft(expected, "\n"))
	begin := start - leading
	if begin < 0 || end > len(data) || (start > 0 && data[start-1] != '\n') || string(data[begin:end]) != expected {
		return 0, 0, errors.New("Owned cw block changed; refusing to overwrite it")
	}
	remainder := append(bytes.Clone(data[:begin]), data[end:]...)
	if bytes.Contains(remainder, []byte(aliasMarker)) {
		return 0, 0, errors.New("Unidentified cw markers in shell startup file")
	}
	if aliasHasDefinition(remainder) {
		return 0, 0, errors.New("Another cw definition exists in shell startup file")
	}
	return begin, end, nil
}

func aliasUnowned(data []byte) error {
	if bytes.Contains(data, []byte(aliasMarker)) || bytes.Contains(data, []byte("agent-workflows-cw")) {
		return errors.New("Preexisting cw markers require installation ownership state")
	}
	if aliasHasDefinition(data) {
		return errors.New("Existing cw definition in shell startup file; refusing to overwrite it")
	}
	return nil
}

func aliasValidateSegment(value any, source string, paths Paths) error {
	segment, ok := value.(string)
	paths.Source = source
	if !ok || (segment != aliasSegment(paths, "") && segment != aliasSegment(paths, "\n")) {
		return errors.New("Corrupt cw block metadata")
	}
	return nil
}

func aliasValidateSource(value any) error {
	source, ok := value.(string)
	if !ok || !filepath.IsAbs(source) || filepath.Clean(source) != source {
		return errors.New("Corrupt cw source checkout path")
	}
	// Legacy ownership is validated from its exact recorded segment. Recovery
	// and removal must remain possible after the source checkout is unavailable.
	return nil
}

func aliasFields(value Object, fields ...string) bool {
	if len(value) != len(fields) {
		return false
	}
	for _, field := range fields {
		if _, ok := value[field]; !ok {
			return false
		}
	}
	return true
}

func aliasInteger(value any) (int, bool) {
	switch value := value.(type) {
	case int:
		return value, value >= 0 && value <= 0o7777
	case int64:
		return int(value), value >= 0 && value <= 0o7777
	case float64:
		return int(value), value >= 0 && value <= 0o7777 && value == float64(int(value))
	default:
		return 0, false
	}
}

func AliasValidateMetadata(value Object, paths Paths) error {
	if !aliasFields(value, "shell", "path", "source", "segment", "original_exists", "original_mode") {
		return errors.New("Corrupt cw ownership metadata")
	}
	if _, ok := value["original_exists"].(bool); !ok {
		return errors.New("Corrupt cw ownership metadata")
	}
	if _, ok := aliasInteger(value["original_mode"]); !ok {
		return errors.New("Corrupt cw ownership metadata")
	}
	path, err := aliasRCPath(paths, value["shell"])
	if err != nil {
		return err
	}
	if value["path"] != path {
		return errors.New("Unsafe cw startup path in ownership metadata")
	}
	if err := aliasValidateSource(value["source"]); err != nil {
		return err
	}
	return aliasValidateSegment(value["segment"], value["source"].(string), paths)
}

func AliasVerify(value Object, paths Paths) error {
	if err := AliasValidateMetadata(value, paths); err != nil {
		return err
	}
	data, exists, _, err := aliasRead(value["path"].(string))
	if err != nil {
		return err
	}
	if !exists {
		return errors.New("Owned cw block disappeared")
	}
	_, _, err = aliasLocate(data, value["segment"].(string))
	return err
}

func aliasOperation(value Object, beforeSegment, afterSegment any, beforeExists, afterExists bool, beforeMode, afterMode int, label string) Object {
	return Object{"kind": "command_alias", "path": value["path"], "shell": value["shell"], "source": value["source"],
		"before_segment": beforeSegment, "after_segment": afterSegment, "before_exists": beforeExists, "after_exists": afterExists,
		"before_mode": beforeMode, "after_mode": afterMode, "label": label}
}

func AliasPrepare(paths Paths, choice string, metadata Object) (Object, Object, Object, error) {
	if metadata != nil {
		if err := AliasVerify(metadata, paths); err != nil {
			return nil, nil, nil, err
		}
		if metadata["source"] != paths.Source {
			return nil, nil, nil, errors.New("cw belongs to another source checkout; use the enrolled checkout or uninstall before setup here")
		}
		report := AliasReport(metadata)
		report["status"] = "preserved"
		return metadata, nil, report, nil
	}
	shell, status := aliasSelectedShell(choice, paths, map[string]string{"SHELL": os.Getenv("SHELL"), "ZDOTDIR": os.Getenv("ZDOTDIR")})
	if shell == "" {
		return nil, nil, Object{"managed": false, "status": status}, nil
	}
	if err := aliasValidateSource(paths.Source); err != nil {
		return nil, nil, nil, err
	}
	installer := filepath.Join(paths.Source, "install.sh")
	if err := aliasRealParent(installer); err != nil {
		return nil, nil, nil, err
	}
	info, err := os.Lstat(installer)
	if err != nil || !info.Mode().IsRegular() {
		return nil, nil, nil, errors.New("cw source checkout has no regular install.sh")
	}
	path, err := aliasRCPath(paths, shell)
	if err != nil {
		return nil, nil, nil, err
	}
	data, exists, mode, err := aliasRead(path)
	if err != nil {
		return nil, nil, nil, err
	}
	if err := aliasUnowned(data); err != nil {
		return nil, nil, nil, err
	}
	leading := ""
	if len(data) > 0 && !bytes.HasSuffix(data, []byte("\n")) {
		leading = "\n"
	}
	metadata = Object{"shell": shell, "path": path, "source": paths.Source, "segment": aliasSegment(paths, leading), "original_exists": exists, "original_mode": mode}
	change := aliasOperation(metadata, nil, metadata["segment"], exists, true, mode, mode, "command-alias")
	report := AliasReport(metadata)
	report["status"] = status
	return metadata, change, report, nil
}

func AliasReport(metadata Object) Object {
	if metadata == nil {
		return Object{"managed": false}
	}
	return Object{"managed": true, "shell": metadata["shell"], "path": metadata["path"], "status": "enrolled"}
}

func AliasRemoval(metadata Object, paths Paths) (Object, error) {
	if err := AliasVerify(metadata, paths); err != nil {
		return nil, err
	}
	_, exists, mode, err := aliasRead(metadata["path"].(string))
	if err != nil {
		return nil, err
	}
	originalMode, _ := aliasInteger(metadata["original_mode"])
	return aliasOperation(metadata, metadata["segment"], nil, exists, metadata["original_exists"].(bool), mode, originalMode, "command-alias"), nil
}

func AliasValidateOperation(value Object, paths Paths) error {
	if !aliasFields(value, "kind", "path", "shell", "source", "before_segment", "after_segment", "before_exists", "after_exists", "before_mode", "after_mode", "label") || value["kind"] != "command_alias" || (value["label"] != "command-alias" && value["label"] != "recover-command-alias") {
		return errors.New("Corrupt keyed cw operation")
	}
	for _, side := range []string{"before", "after"} {
		if _, ok := value[side+"_exists"].(bool); !ok {
			return errors.New("Corrupt keyed cw operation")
		}
		if _, ok := aliasInteger(value[side+"_mode"]); !ok {
			return errors.New("Corrupt keyed cw operation")
		}
	}
	path, err := aliasRCPath(paths, value["shell"])
	if err != nil {
		return err
	}
	if value["path"] != path {
		return errors.New("Unsafe cw startup path in mutation journal")
	}
	if err := aliasValidateSource(value["source"]); err != nil {
		return err
	}
	for _, side := range []string{"before", "after"} {
		if value[side+"_segment"] == nil {
			continue
		}
		if err := aliasValidateSegment(value[side+"_segment"], value["source"].(string), paths); err != nil {
			return err
		}
		if !value[side+"_exists"].(bool) {
			return errors.New("Corrupt absent cw operation")
		}
	}
	if value["before_segment"] == value["after_segment"] {
		return errors.New("Corrupt cw operation with no block change")
	}
	_, _, _, err = aliasRead(path)
	return err
}

func aliasCurrentSegment(data []byte, value Object) (any, error) {
	if !bytes.Contains(data, []byte(aliasMarker)) && !bytes.Contains(data, []byte("agent-workflows-cw")) {
		return nil, aliasUnowned(data)
	}
	for _, key := range []string{"before_segment", "after_segment"} {
		if value[key] != nil {
			if _, _, err := aliasLocate(data, value[key].(string)); err == nil {
				return value[key], nil
			}
		}
	}
	return nil, errors.New("Owned cw block changed; refusing recovery")
}

func AliasRender(value Object, paths Paths) ([]byte, os.FileMode, bool, error) {
	if err := AliasValidateOperation(value, paths); err != nil {
		return nil, 0, false, err
	}
	data, exists, mode, err := aliasRead(value["path"].(string))
	if err != nil {
		return nil, 0, false, err
	}
	if value["label"] == "command-alias" && value["before_segment"] == nil && exists != value["before_exists"].(bool) {
		return nil, 0, false, errors.New("Shell startup file existence changed during enrollment")
	}
	current, err := aliasCurrentSegment(data, value)
	if err != nil {
		return nil, 0, false, err
	}
	if current != value["before_segment"] {
		return nil, 0, false, errors.New("Owned cw block changed during mutation")
	}
	var result []byte
	if value["before_segment"] == nil {
		target := []byte(value["after_segment"].(string))
		if len(data) == 0 || bytes.HasSuffix(data, []byte("\n")) || bytes.HasPrefix(target, []byte("\n")) {
			result = append(bytes.Clone(data), target...)
		} else {
			result = append(bytes.Clone(target), data...)
		}
	} else {
		start, end, err := aliasLocate(data, value["before_segment"].(string))
		if err != nil {
			return nil, 0, false, err
		}
		var target []byte
		if value["after_segment"] != nil {
			target = []byte(value["after_segment"].(string))
		}
		result = append(append(bytes.Clone(data[:start]), target...), data[end:]...)
	}
	remove := !value["after_exists"].(bool) && len(result) == 0
	beforeMode, _ := aliasInteger(value["before_mode"])
	if remove && exists && mode != beforeMode {
		return nil, 0, false, errors.New("Shell startup file mode changed before removal; refusing to delete it")
	}
	if !exists {
		mode, _ = aliasInteger(value["after_mode"])
	}
	if remove {
		result = nil
	}
	return result, aliasFileMode(mode), remove, nil
}

func AliasReversal(value Object, paths Paths) (Object, error) {
	if err := AliasValidateOperation(value, paths); err != nil {
		return nil, err
	}
	data, exists, mode, err := aliasRead(value["path"].(string))
	if err != nil {
		return nil, err
	}
	current, err := aliasCurrentSegment(data, value)
	if err != nil {
		return nil, err
	}
	if current == value["before_segment"] {
		return nil, nil
	}
	if current != value["after_segment"] {
		return nil, errors.New("Owned cw block changed; recovery refuses to overwrite it")
	}
	beforeMode, _ := aliasInteger(value["before_mode"])
	return aliasOperation(value, value["after_segment"], value["before_segment"], exists, value["before_exists"].(bool), mode, beforeMode, "recover-command-alias"), nil
}

func AliasPending(value Object, paths Paths) (Object, error) {
	if err := AliasValidateOperation(value, paths); err != nil {
		return nil, err
	}
	data, _, _, err := aliasRead(value["path"].(string))
	if err != nil {
		return nil, err
	}
	current, err := aliasCurrentSegment(data, value)
	if err != nil {
		return nil, err
	}
	if current == value["after_segment"] {
		return nil, nil
	}
	if current != value["before_segment"] {
		return nil, errors.New("Owned cw block changed during interrupted recovery")
	}
	return value, nil
}
