package workflow

import (
	"bytes"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/pelletier/go-toml/v2"
	"github.com/pelletier/go-toml/v2/unstable"
)

const sharedConfigOperationKind = "codex_config"
const sharedConfigResource = "global/codex-defaults.toml"

var sharedConfigKeys = [...]string{
	"model_context_window",
	"features.memories", "memories.generate_memories", "memories.use_memories",
	"memories.extract_model", "memories.consolidation_model",
}

// SharedConfigDefaults loads only portable, explicitly supported settings. The
// optional resource leaves historical releases and their exact manifest intact.
func SharedConfigDefaults(root string) (Object, error) {
	path := filepath.Join(root, filepath.FromSlash(sharedConfigResource))
	if err := realDirectory(filepath.Dir(path), false); err != nil {
		return nil, err
	}
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("shared Codex defaults must be a regular, unsymlinked file")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var values Object
	if !utf8.Valid(data) || toml.Unmarshal(data, &values) != nil || !modelFields(values, "version", "model_context_window", "features", "memories") || !suiteInteger(values["version"], 1) {
		return nil, errors.New("shared Codex defaults have invalid fields or version")
	}
	features, memories := object(values["features"]), object(values["memories"])
	if !modelFields(features, "memories") || !modelFields(memories, "generate_memories", "use_memories", "extract_model", "consolidation_model") {
		return nil, errors.New("shared Codex defaults contain unsupported settings")
	}
	defaults := Object{}
	for _, key := range sharedConfigKeys {
		value := values[key]
		if group, name, nested := strings.Cut(key, "."); nested {
			value = object(values[group])[name]
		}
		if err := configValueType(key, value); err != nil {
			return nil, fmt.Errorf("invalid shared Codex default: %w", err)
		}
		if value, ok := value.(string); ok && strings.TrimSpace(value) == "" {
			return nil, fmt.Errorf("shared Codex default %s must be nonblank", key)
		}
		defaults[key] = value
	}
	return defaults, nil
}

func sharedConfigStateRequired(s Object) bool {
	_, shared := s["shared_config"]
	_, origin := s["config_original_exists"]
	return shared || origin
}

func configValueType(key string, value any) error {
	switch key {
	case "features.memories", "memories.generate_memories", "memories.use_memories":
		if _, ok := value.(bool); ok {
			return nil
		}
	case "model", "model_reasoning_effort", "memories.extract_model", "memories.consolidation_model":
		if _, ok := value.(string); ok {
			return nil
		}
	case "model_context_window":
		if tokens, ok := value.(int64); ok && tokens > 0 {
			return nil
		}
	}
	return fmt.Errorf("%s has an unsupported value type", key)
}

type configDocument struct {
	data     []byte
	spans    map[string]modelSpan
	tables   map[string]int
	firstKey int
}

// configParse resolves actual TOML key segments; a quoted literal containing a
// dot is never confused with a nested setting. Only requested keys are inspected.
// Inline parent tables are refused because replacing them would own user bytes.
func configParse(data []byte, owned []string) (configDocument, error) {
	doc := configDocument{data: data, spans: map[string]modelSpan{}, tables: map[string]int{}, firstKey: -1}
	var values Object
	if !utf8.Valid(data) || toml.Unmarshal(data, &values) != nil {
		return doc, errors.New("Codex config.toml is malformed; refusing to change it")
	}
	for _, key := range owned {
		parts := strings.Split(key, ".")
		parent := values
		for index, part := range parts {
			value, found := parent[part]
			if !found {
				break
			}
			if index == len(parts)-1 {
				if err := configValueType(key, value); err != nil {
					return doc, fmt.Errorf("Codex config.toml: %w", err)
				}
			} else {
				parent = object(value)
				if parent == nil {
					return doc, fmt.Errorf("Codex config.toml parent of %s must be a table", key)
				}
			}
		}
	}
	var parser unstable.Parser
	parser.Reset(data)
	var scope []string
	for parser.NextExpression() {
		node := parser.Expression()
		if node.Kind != unstable.KeyValue && node.Kind != unstable.Table && node.Kind != unstable.ArrayTable {
			continue
		}
		var parts []string
		keys := node.Key()
		start := -1
		for keys.Next() {
			part := keys.Node()
			if start == -1 {
				start = int(part.Raw.Offset)
			}
			parts = append(parts, string(part.Data))
		}
		for start > 0 && data[start-1] != '\n' {
			start--
		}
		if doc.firstKey == -1 {
			doc.firstKey = start
		}
		if node.Kind != unstable.KeyValue {
			scope = parts
			if node.Kind == unstable.Table && len(parts) == 1 && (parts[0] == "features" || parts[0] == "memories") {
				end := start
				for end < len(data) && data[end] != '\n' {
					end++
				}
				if end < len(data) {
					end++
				}
				doc.tables[parts[0]] = end
			}
			continue
		}
		full := append(slices.Clone(scope), parts...)
		value := node.Value()
		for _, key := range owned {
			wanted := strings.Split(key, ".")
			if value.Kind == unstable.InlineTable && len(full) < len(wanted) && slices.Equal(full, wanted[:len(full)]) {
				return doc, fmt.Errorf("Codex config.toml uses an inline parent table for %s; use an explicit table or dotted keys before installing shared defaults", key)
			}
			if !slices.Equal(full, wanted) {
				continue
			}
			raw := value.Raw
			// This parser version exposes bool tokens through Data only.
			if value.Kind == unstable.Bool {
				raw = parser.Range(value.Data)
			}
			valueStart := int(raw.Offset)
			valueEnd := valueStart + int(raw.Length)
			end := valueEnd
			for end < len(data) && data[end] != '\n' {
				end++
			}
			if end < len(data) {
				end++
			}
			doc.spans[key] = modelSpan{start: start, end: end, valueStart: valueStart, valueEnd: valueEnd}
		}
	}
	if parser.Error() != nil {
		return doc, errors.New("Codex config.toml is malformed; refusing to change it")
	}
	return doc, nil
}

func configRead(path string, owned []string) (configDocument, bool, int, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		doc, err := configParse(nil, owned)
		return doc, false, 0o600, err
	}
	if err != nil {
		return configDocument{}, false, 0, err
	}
	if !info.Mode().IsRegular() {
		return configDocument{}, false, 0, errors.New("Codex config.toml is occupied or symlinked")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return configDocument{}, false, 0, err
	}
	doc, err := configParse(data, owned)
	return doc, true, modelMode(info), err
}

func configSnapshot(doc configDocument, owned []string) Object {
	snapshot := Object{}
	for _, key := range owned {
		if span, found := doc.spans[key]; found {
			snapshot[key] = Object{"present": true, "representation": string(doc.data[span.valueStart:span.valueEnd])}
		} else {
			snapshot[key] = Object{"present": false}
		}
	}
	return snapshot
}

func configValidateSnapshot(raw any, owned []string) (Object, error) {
	values := object(raw)
	if !modelFields(values, owned...) {
		return nil, errors.New("corrupt shared config ownership keys")
	}
	for _, key := range owned {
		item := object(values[key])
		present, ok := item["present"].(bool)
		if !ok || (!present && !modelFields(item, "present")) {
			return nil, errors.New("corrupt shared config ownership value")
		}
		if !present {
			continue
		}
		representation, ok := item["representation"].(string)
		if !ok || !modelFields(item, "present", "representation") {
			return nil, errors.New("corrupt shared config value representation")
		}
		doc, err := configParse([]byte(key+" = "+representation+"\n"), []string{key})
		if err != nil || len(doc.spans) != 1 || doc.firstKey != 0 {
			return nil, errors.New("corrupt shared config value representation")
		}
		span := doc.spans[key]
		if string(doc.data[span.valueStart:span.valueEnd]) != representation {
			return nil, errors.New("corrupt shared config value representation")
		}
		// Reject injected second assignments, headers, and comments: metadata is a
		// value token, never a TOML document supplied to the renderer.
		var decoded Object
		if toml.Unmarshal(doc.data, &decoded) != nil {
			return nil, errors.New("corrupt shared config value representation")
		}
		parts := strings.Split(key, ".")
		for _, part := range parts[:len(parts)-1] {
			if len(decoded) != 1 {
				return nil, errors.New("corrupt shared config value representation")
			}
			decoded = object(decoded[part])
		}
		if len(decoded) != 1 {
			return nil, errors.New("corrupt shared config value representation")
		}
	}
	return values, nil
}

func SharedConfigValidateMetadata(metadata Object) error {
	if !modelFields(metadata, "original", "expected", "original_exists") {
		return errors.New("corrupt shared config ownership metadata")
	}
	exists, ok := metadata["original_exists"].(bool)
	if !ok {
		return errors.New("corrupt shared config ownership origin")
	}
	original, err := configValidateSnapshot(metadata["original"], sharedConfigKeys[:])
	if err != nil {
		return err
	}
	expected, err := configValidateSnapshot(metadata["expected"], sharedConfigKeys[:])
	if err != nil {
		return err
	}
	for _, key := range sharedConfigKeys {
		if (!exists && original[key].(Object)["present"].(bool)) || !expected[key].(Object)["present"].(bool) {
			return errors.New("corrupt shared config origin or managed defaults")
		}
	}
	return nil
}

func SharedConfigVerify(path string, metadata Object) error {
	if err := SharedConfigValidateMetadata(metadata); err != nil {
		return err
	}
	doc, exists, _, err := configRead(path, sharedConfigKeys[:])
	if err != nil {
		return err
	}
	if !exists || !reflect.DeepEqual(configSnapshot(doc, sharedConfigKeys[:]), metadata["expected"]) {
		return errors.New("owned shared Codex defaults changed; refusing to overwrite them")
	}
	return nil
}

// The new operation uses the same value-token records as the frozen model-only
// operation, with a separate discriminator and complete, allowlisted groups.
func configOperationKeys(snapshot Object) ([]string, error) {
	var owned []string
	for _, group := range [][]string{modelKeys[:], sharedConfigKeys[:]} {
		count := 0
		for _, key := range group {
			if _, ok := snapshot[key]; ok {
				count++
			}
		}
		if count == 0 {
			continue
		}
		if count != len(group) {
			return nil, errors.New("incomplete shared config operation group")
		}
		owned = append(owned, group...)
	}
	if len(owned) == 0 || len(owned) != len(snapshot) {
		return nil, errors.New("unsupported shared config operation keys")
	}
	return owned, nil
}

func ConfigValidateOperation(op Object) error {
	if !modelFields(op, "kind", "path", "before_keys", "after_keys", "before_exists", "after_exists", "before_mode", "after_mode", "label") || op["kind"] != sharedConfigOperationKind {
		return errors.New("corrupt keyed shared config operation")
	}
	if _, ok := op["path"].(string); !ok || (op["label"] != "codex-config" && op["label"] != "recover-codex-config") {
		return errors.New("corrupt shared config operation path or label")
	}
	for _, field := range []string{"before_mode", "after_mode"} {
		if _, ok := modelInteger(op[field]); !ok {
			return errors.New("corrupt shared config operation mode")
		}
	}
	owned, err := configOperationKeys(object(op["before_keys"]))
	if err != nil {
		return err
	}
	for _, side := range []string{"before", "after"} {
		exists, ok := op[side+"_exists"].(bool)
		if !ok {
			return errors.New("corrupt shared config operation origin")
		}
		values, err := configValidateSnapshot(op[side+"_keys"], owned)
		if err != nil {
			return err
		}
		for _, key := range owned {
			if !exists && values[key].(Object)["present"].(bool) {
				return errors.New("corrupt absent shared config operation")
			}
		}
	}
	return nil
}

func configOperation(path string, before, after Object, beforeExists, afterExists bool, beforeMode, afterMode int, label string) Object {
	op := modelOperation(path, before, after, beforeExists, afterExists, beforeMode, afterMode, label)
	op["kind"] = sharedConfigOperationKind
	return op
}

func configReplace(doc configDocument, target Object, owned []string) ([]byte, error) {
	if _, err := configValidateSnapshot(target, owned); err != nil {
		return nil, err
	}
	data := bytes.Clone(doc.data)
	for index := len(owned) - 1; index >= 0; index-- {
		key := owned[index]
		current, err := configParse(data, owned)
		if err != nil {
			return nil, err
		}
		item := target[key].(Object)
		span, present := current.spans[key]
		if item["present"].(bool) {
			representation := item["representation"].(string)
			if present {
				data = append(append(bytes.Clone(data[:span.valueStart]), representation...), data[span.valueEnd:]...)
			} else {
				position := max(current.firstKey, 0)
				assignment := key
				if parent, name, nested := strings.Cut(key, "."); nested {
					if offset, found := current.tables[parent]; found {
						position, assignment = offset, name
					}
				}
				// A table header without a final newline cannot safely receive an
				// appended key without introducing unowned scaffolding.
				if position > 0 && data[position-1] != '\n' {
					return nil, errors.New("Codex table header needs a final newline before shared defaults can be inserted")
				}
				line := []byte(assignment + " = " + representation + "\n")
				data = append(append(bytes.Clone(data[:position]), line...), data[position:]...)
			}
		} else if present {
			var replacement []byte
			if commentAt := bytes.IndexByte(data[span.valueEnd:span.end], '#'); commentAt >= 0 {
				indentEnd := span.start
				for indentEnd < span.valueStart && (data[indentEnd] == ' ' || data[indentEnd] == '\t') {
					indentEnd++
				}
				replacement = append(bytes.Clone(data[span.start:indentEnd]), data[span.valueEnd+commentAt:span.end]...)
			}
			data = append(append(bytes.Clone(data[:span.start]), replacement...), data[span.end:]...)
		}
	}
	if _, err := configParse(data, owned); err != nil {
		return nil, err
	}
	return data, nil
}

func configExpected(defaults Object, owned []string) (Object, error) {
	after := Object{}
	for _, key := range owned {
		value, ok := defaults[key]
		if !ok {
			return nil, fmt.Errorf("missing shared Codex default %s", key)
		}
		if err := configValueType(key, value); err != nil {
			return nil, err
		}
		var representation string
		switch value := value.(type) {
		case string:
			representation = modelQuote(value)
		case int64:
			representation = strconv.FormatInt(value, 10)
		case bool:
			representation = strconv.FormatBool(value)
		}
		after[key] = Object{"present": true, "representation": representation}
	}
	return after, nil
}

func configOrigin(s Object, exists bool) bool {
	if origin, ok := s["config_original_exists"].(bool); ok {
		return origin
	}
	for _, field := range []string{"model_config", "shared_config"} {
		if metadata := object(s[field]); metadata != nil {
			return metadata["original_exists"].(bool)
		}
	}
	return exists
}

// configPrepare updates independently recorded ownership in one file operation.
// A nil shared resource releases that group's ownership during rollback/update.
func configPrepare(path string, models map[string]string, shared Object, s Object) (Object, Object, error) {
	var owned []string
	if models != nil || s["model_config"] != nil {
		owned = append(owned, modelKeys[:]...)
	}
	if shared != nil || s["shared_config"] != nil {
		owned = append(owned, sharedConfigKeys[:]...)
	}
	if len(owned) == 0 {
		return clone(s), nil, nil
	}
	doc, exists, mode, err := configRead(path, owned)
	if err != nil {
		return nil, nil, err
	}
	before, after, state := configSnapshot(doc, owned), Object{}, clone(s)
	origin := configOrigin(s, exists)
	for _, group := range []struct {
		field    string
		keys     []string
		defaults Object
	}{
		{"model_config", modelKeys[:], stringsObject(models)},
		{"shared_config", sharedConfigKeys[:], shared},
	} {
		metadata := object(s[group.field])
		if group.defaults == nil && metadata == nil {
			continue
		}
		if metadata != nil {
			var err error
			if group.field == "model_config" {
				err = ModelValidateMetadata(metadata)
			} else {
				err = SharedConfigValidateMetadata(metadata)
			}
			if err != nil {
				return nil, nil, err
			}
			if !exists || !reflect.DeepEqual(configSnapshot(doc, group.keys), metadata["expected"]) {
				return nil, nil, fmt.Errorf("owned %s defaults changed; refusing to overwrite them", group.field)
			}
		}
		if group.defaults == nil {
			maps.Copy(after, metadata["original"].(Object))
			delete(state, group.field)
			continue
		}
		expected, err := configExpected(group.defaults, group.keys)
		if err != nil {
			return nil, nil, err
		}
		maps.Copy(after, expected)
		if metadata == nil {
			metadata = Object{"original": configSnapshot(doc, group.keys), "original_exists": exists}
		} else {
			metadata = maps.Clone(metadata)
		}
		metadata["expected"] = expected
		state[group.field] = metadata
	}
	if state["model_config"] != nil || state["shared_config"] != nil {
		state["config_original_exists"] = origin
	} else {
		delete(state, "config_original_exists")
	}
	// Render in preflight, before creating a journal or changing another path.
	if _, err := configReplace(doc, after, owned); err != nil {
		return nil, nil, err
	}
	if reflect.DeepEqual(before, after) && exists {
		return state, nil, nil
	}
	afterExists := origin || slices.ContainsFunc(owned, func(key string) bool { return after[key].(Object)["present"].(bool) })
	return state, configOperation(path, before, after, exists, afterExists, mode, mode, "codex-config"), nil
}

func stringsObject(values map[string]string) Object {
	if values == nil {
		return nil
	}
	result := Object{}
	for key, value := range values {
		result[key] = value
	}
	return result
}

func ConfigRemoval(path string, s Object) (Object, error) {
	var owned []string
	after := Object{}
	for _, group := range []struct {
		field string
		keys  []string
	}{
		{"model_config", modelKeys[:]}, {"shared_config", sharedConfigKeys[:]},
	} {
		if metadata := object(s[group.field]); metadata != nil {
			var err error
			if group.field == "model_config" {
				err = ModelVerify(path, metadata)
			} else {
				err = SharedConfigVerify(path, metadata)
			}
			if err != nil {
				return nil, err
			}
			owned = append(owned, group.keys...)
			maps.Copy(after, metadata["original"].(Object))
		}
	}
	doc, exists, mode, err := configRead(path, owned)
	if err != nil {
		return nil, err
	}
	if _, err := configReplace(doc, after, owned); err != nil {
		return nil, err
	}
	afterExists := configOrigin(s, exists) || slices.ContainsFunc(owned, func(key string) bool { return after[key].(Object)["present"].(bool) })
	return configOperation(path, configSnapshot(doc, owned), after, exists, afterExists, mode, mode, "codex-config"), nil
}

func ConfigRender(path string, op Object) ([]byte, os.FileMode, bool, error) {
	if err := ConfigValidateOperation(op); err != nil {
		return nil, 0, false, err
	}
	owned, _ := configOperationKeys(object(op["before_keys"]))
	doc, exists, mode, err := configRead(path, owned)
	if err != nil {
		return nil, 0, false, err
	}
	if !reflect.DeepEqual(configSnapshot(doc, owned), op["before_keys"]) {
		return nil, 0, false, errors.New("owned shared Codex defaults changed during mutation; refusing to overwrite them")
	}
	data, err := configReplace(doc, op["after_keys"].(Object), owned)
	if err != nil {
		return nil, 0, false, err
	}
	remove := !op["after_exists"].(bool) && len(data) == 0
	beforeMode, _ := modelInteger(op["before_mode"])
	if remove && exists && mode != beforeMode {
		return nil, 0, false, errors.New("Codex config.toml mode changed before removal; refusing to delete it")
	}
	if !exists {
		mode, _ = modelInteger(op["after_mode"])
	}
	return data, modelFileMode(mode), remove, nil
}

func ConfigReversal(path string, op Object) (Object, error) {
	if err := ConfigValidateOperation(op); err != nil {
		return nil, err
	}
	owned, _ := configOperationKeys(object(op["before_keys"]))
	doc, exists, mode, err := configRead(path, owned)
	if err != nil {
		return nil, err
	}
	current := configSnapshot(doc, owned)
	if reflect.DeepEqual(current, op["before_keys"]) {
		return nil, nil
	}
	if !reflect.DeepEqual(current, op["after_keys"]) {
		return nil, errors.New("owned shared Codex defaults changed; recovery refuses to overwrite them")
	}
	beforeMode, _ := modelInteger(op["before_mode"])
	return configOperation(path, op["after_keys"].(Object), op["before_keys"].(Object), exists, op["before_exists"].(bool), mode, beforeMode, "recover-codex-config"), nil
}

func ConfigPending(path string, op Object) (Object, error) {
	if err := ConfigValidateOperation(op); err != nil {
		return nil, err
	}
	owned, _ := configOperationKeys(object(op["before_keys"]))
	doc, _, _, err := configRead(path, owned)
	if err != nil {
		return nil, err
	}
	current := configSnapshot(doc, owned)
	if reflect.DeepEqual(current, op["after_keys"]) {
		return nil, nil
	}
	if !reflect.DeepEqual(current, op["before_keys"]) {
		return nil, errors.New("owned shared Codex defaults changed during interrupted recovery")
	}
	return op, nil
}
