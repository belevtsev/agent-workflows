package workflow

import (
	"bytes"
	"encoding/json/jsontext"
	json "encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"reflect"
)

const claudeSettingsOperationKind = "claude_settings"

var claudeSettingsKeys = [...]string{"model", "effortLevel"}

// settingsMember holds byte offsets so edits leave every other key, its order and
// its formatting untouched.
type settingsMember struct{ start, valueStart, end int }

type settingsDocument struct {
	data        []byte
	members     map[string]settingsMember
	order       []string
	open, close int
}

func settingsParse(data []byte) (settingsDocument, error) {
	doc := settingsDocument{data: data, members: map[string]settingsMember{}, open: -1, close: -1}
	if len(bytes.TrimSpace(data)) == 0 {
		return doc, nil
	}
	decoder := jsontext.NewDecoder(bytes.NewReader(data))
	token, err := decoder.ReadToken()
	if err != nil {
		return doc, fmt.Errorf("Claude settings are not valid JSON: %w", err)
	}
	if token.Kind() != '{' {
		return doc, errors.New("Claude settings must be a JSON object")
	}
	doc.open = int(decoder.InputOffset()) - 1
	for decoder.PeekKind() != '}' {
		previous := int(decoder.InputOffset())
		token, err := decoder.ReadToken()
		if err != nil {
			return doc, fmt.Errorf("Claude settings are not valid JSON: %w", err)
		}
		name := token.String()
		value, err := decoder.ReadValue()
		if err != nil {
			return doc, fmt.Errorf("Claude settings are not valid JSON: %w", err)
		}
		end := int(decoder.InputOffset())
		start := previous + bytes.IndexByte(data[previous:], '"')
		doc.members[name] = settingsMember{start: start, valueStart: end - len(value), end: end}
		doc.order = append(doc.order, name)
	}
	if _, err = decoder.ReadToken(); err != nil {
		return doc, fmt.Errorf("Claude settings are not valid JSON: %w", err)
	}
	doc.close = int(decoder.InputOffset()) - 1
	if _, err = decoder.ReadToken(); !errors.Is(err, io.EOF) {
		return doc, errors.New("Claude settings contain trailing data")
	}
	return doc, nil
}

func settingsRead(path string) (settingsDocument, bool, int, error) {
	if err := realDirectory(filepath.Dir(path), false); err != nil {
		return settingsDocument{}, false, 0, err
	}
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		doc, err := settingsParse(nil)
		return doc, false, 0o600, err
	}
	if err != nil {
		return settingsDocument{}, false, 0, err
	}
	if !info.Mode().IsRegular() {
		return settingsDocument{}, false, 0, errors.New("Claude settings path is occupied or symlinked")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return settingsDocument{}, false, 0, err
	}
	doc, err := settingsParse(data)
	return doc, true, specialFileMode(info), err
}

func settingsSnapshot(doc settingsDocument) Object {
	snapshot := Object{}
	for _, key := range claudeSettingsKeys {
		if member, present := doc.members[key]; present {
			snapshot[key] = Object{"present": true, "representation": string(doc.data[member.valueStart:member.end])}
		} else {
			snapshot[key] = Object{"present": false}
		}
	}
	return snapshot
}

func settingsValidateSnapshot(raw any) (Object, error) {
	snapshot, ok := raw.(Object)
	if !ok || len(snapshot) != len(claudeSettingsKeys) {
		return nil, errors.New("corrupt Claude settings snapshot")
	}
	for _, key := range claudeSettingsKeys {
		item, ok := snapshot[key].(Object)
		if !ok {
			return nil, errors.New("corrupt Claude settings snapshot")
		}
		present, ok := item["present"].(bool)
		if !ok {
			return nil, errors.New("corrupt Claude settings snapshot")
		}
		if !present {
			if len(item) != 1 {
				return nil, errors.New("corrupt absent Claude setting")
			}
			continue
		}
		representation, ok := item["representation"].(string)
		if !ok || len(item) != 2 || !jsontext.Value(representation).IsValid() {
			return nil, errors.New("corrupt Claude setting value")
		}
	}
	return snapshot, nil
}

func settingsApply(data []byte, key string, target Object) ([]byte, error) {
	doc, err := settingsParse(data)
	if err != nil {
		return nil, err
	}
	member, exists := doc.members[key]
	if target["present"] != true {
		if !exists {
			return data, nil
		}
		position := indexOf(doc.order, key)
		if len(doc.order) == 1 {
			return append(bytes.Clone(data[:doc.open+1]), data[doc.close:]...), nil
		}
		if position == 0 {
			next := doc.members[doc.order[1]]
			return append(bytes.Clone(data[:member.start]), data[next.start:]...), nil
		}
		previous := doc.members[doc.order[position-1]]
		return append(bytes.Clone(data[:previous.end]), data[member.end:]...), nil
	}
	value := []byte(text(target["representation"]))
	if exists {
		return append(append(bytes.Clone(data[:member.valueStart]), value...), data[member.end:]...), nil
	}
	name, err := json.Marshal(key)
	if err != nil {
		return nil, err
	}
	entry := append(append(name, ": "...), value...)
	if doc.open < 0 {
		return append(append([]byte("{\n  "), entry...), "\n}\n"...), nil
	}
	if len(doc.order) == 0 {
		out := append(bytes.Clone(data[:doc.open+1]), "\n  "...)
		out = append(append(out, entry...), '\n')
		return append(out, data[doc.close:]...), nil
	}
	separator := data[doc.open+1 : doc.members[doc.order[0]].start]
	last := doc.members[doc.order[len(doc.order)-1]]
	out := append(bytes.Clone(data[:last.end]), ',')
	out = append(append(out, separator...), entry...)
	return append(out, data[last.end:]...), nil
}

func indexOf(values []string, value string) int {
	for index, candidate := range values {
		if candidate == value {
			return index
		}
	}
	return -1
}

func ClaudeSettingsValidateMetadata(metadata Object) error {
	if !exactFields(metadata, "original", "expected", "original_exists") {
		return errors.New("invalid Claude settings metadata fields")
	}
	if _, ok := metadata["original_exists"].(bool); !ok {
		return errors.New("invalid Claude settings origin")
	}
	for _, field := range []string{"original", "expected"} {
		if _, err := settingsValidateSnapshot(metadata[field]); err != nil {
			return err
		}
	}
	return nil
}

func ClaudeSettingsValidateOperation(op Object) error {
	if !exactFields(op, "kind", "path", "before_keys", "after_keys", "before_exists", "after_exists", "before_mode", "after_mode", "label") {
		return errors.New("invalid Claude settings operation fields")
	}
	for _, side := range []string{"before", "after"} {
		if _, err := settingsValidateSnapshot(op[side+"_keys"]); err != nil {
			return err
		}
		if _, ok := op[side+"_exists"].(bool); !ok {
			return errors.New("invalid Claude settings existence")
		}
		if _, ok := modeInteger(op[side+"_mode"]); !ok {
			return errors.New("invalid Claude settings mode")
		}
	}
	return nil
}

func settingsOperation(path string, before, after Object, beforeExists, afterExists bool, beforeMode, afterMode int, label string) Object {
	return Object{"kind": claudeSettingsOperationKind, "path": path, "before_keys": before, "after_keys": after, "before_exists": beforeExists, "after_exists": afterExists, "before_mode": beforeMode, "after_mode": afterMode, "label": label}
}

// ClaudeSettingsPrepare owns the two default keys. A key the user has changed
// since the last activation stays theirs, including across release updates.
func ClaudeSettingsPrepare(path string, defaults map[string]string, metadata Object) (Object, Object, error) {
	doc, exists, mode, err := settingsRead(path)
	if err != nil {
		return nil, nil, err
	}
	before := settingsSnapshot(doc)
	expected := Object{}
	for _, key := range claudeSettingsKeys {
		value, ok := defaults[key]
		if !ok {
			return nil, nil, fmt.Errorf("missing Claude model default %s", key)
		}
		representation, err := json.Marshal(value)
		if err != nil {
			return nil, nil, err
		}
		expected[key] = Object{"present": true, "representation": string(representation)}
	}
	owned := Object{"original": before, "expected": expected, "original_exists": exists}
	target := maps.Clone(expected)
	if metadata != nil {
		if err = ClaudeSettingsValidateMetadata(metadata); err != nil {
			return nil, nil, err
		}
		owned = maps.Clone(metadata)
		previous := metadata["expected"].(Object)
		kept := maps.Clone(expected)
		for _, key := range claudeSettingsKeys {
			if !reflect.DeepEqual(before[key], previous[key]) {
				target[key], kept[key] = before[key], previous[key]
			}
		}
		owned["expected"] = kept
	}
	if reflect.DeepEqual(before, target) {
		return owned, nil, nil
	}
	return owned, settingsOperation(path, before, target, exists, true, mode, mode, "claude-settings"), nil
}

// ClaudeSettingsDrift names the owned keys that no longer hold the managed value.
func ClaudeSettingsDrift(path string, metadata Object) ([]string, error) {
	drift := []string{}
	if metadata == nil {
		return drift, nil
	}
	doc, _, _, err := settingsRead(path)
	if err != nil {
		return nil, err
	}
	current := settingsSnapshot(doc)
	expected := object(metadata["expected"])
	for _, key := range claudeSettingsKeys {
		if !reflect.DeepEqual(current[key], expected[key]) {
			drift = append(drift, key)
		}
	}
	return drift, nil
}

// ClaudeSettingsRemoval restores only keys still holding the managed value.
func ClaudeSettingsRemoval(path string, metadata Object) (Object, error) {
	doc, exists, mode, err := settingsRead(path)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, nil
	}
	before := settingsSnapshot(doc)
	target := maps.Clone(before)
	expected, original := object(metadata["expected"]), object(metadata["original"])
	for _, key := range claudeSettingsKeys {
		if reflect.DeepEqual(before[key], expected[key]) {
			target[key] = original[key]
		}
	}
	if reflect.DeepEqual(before, target) {
		return nil, nil
	}
	return settingsOperation(path, before, target, exists, metadata["original_exists"].(bool), mode, mode, "claude-settings"), nil
}

// ClaudeSettingsRender changes only keys that still hold their before value, so
// a concurrent edit by Claude Code survives instead of stranding the journal.
func ClaudeSettingsRender(path string, op Object) ([]byte, os.FileMode, bool, error) {
	if err := ClaudeSettingsValidateOperation(op); err != nil {
		return nil, 0, false, err
	}
	doc, exists, mode, err := settingsRead(path)
	if err != nil {
		return nil, 0, false, err
	}
	if !exists {
		mode, _ = modeInteger(op["after_mode"])
	}
	current := settingsSnapshot(doc)
	before, after := object(op["before_keys"]), object(op["after_keys"])
	data := doc.data
	for _, key := range claudeSettingsKeys {
		if reflect.DeepEqual(current[key], before[key]) {
			if data, err = settingsApply(data, key, object(after[key])); err != nil {
				return nil, 0, false, err
			}
		}
	}
	result, err := settingsParse(data)
	if err != nil {
		return nil, 0, false, err
	}
	remove := op["after_exists"] == false && len(result.order) == 0
	return data, fileModeBits(mode), remove, nil
}

func settingsChanged(current, from, to Object) bool {
	for _, key := range claudeSettingsKeys {
		if !reflect.DeepEqual(from[key], to[key]) && reflect.DeepEqual(current[key], from[key]) {
			return true
		}
	}
	return false
}

// ClaudeSettingsReversal undoes keys the forward operation applied, leaving any
// key that has since moved to a third value.
func ClaudeSettingsReversal(path string, op Object) (Object, error) {
	if err := ClaudeSettingsValidateOperation(op); err != nil {
		return nil, err
	}
	doc, exists, mode, err := settingsRead(path)
	if err != nil {
		return nil, err
	}
	before, after := object(op["before_keys"]), object(op["after_keys"])
	if !settingsChanged(settingsSnapshot(doc), after, before) {
		return nil, nil
	}
	restored, _ := modeInteger(op["before_mode"])
	return settingsOperation(path, after, before, exists, op["before_exists"].(bool), mode, restored, "recover-claude-settings"), nil
}

func ClaudeSettingsPending(path string, op Object) (Object, error) {
	if err := ClaudeSettingsValidateOperation(op); err != nil {
		return nil, err
	}
	doc, _, _, err := settingsRead(path)
	if err != nil {
		return nil, err
	}
	if !settingsChanged(settingsSnapshot(doc), object(op["before_keys"]), object(op["after_keys"])) {
		return nil, nil
	}
	return op, nil
}
