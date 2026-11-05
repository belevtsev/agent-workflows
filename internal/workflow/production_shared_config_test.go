package workflow

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/pelletier/go-toml/v2"
)

const productionSharedFiveResource = `version = 1

[features]
memories = true

[memories]
generate_memories = true
use_memories = true
extract_model = 'gpt-6.1-sol'
consolidation_model = 'gpt-6.1-sol'
`

const productionSharedPersonalDocument = `# independent personal comment
model = 'device-model'
model_reasoning_effort = 'high'

[profiles.personal]
model = 'profile-model'

[features]
memories = false
device_flag = 'keep-feature'

[memories]
use_memories = false
extract_model = 'personal-extract'
consolidation_model = '''personal consolidation'''
device_setting = 'keep-memory'

[mcp_servers.personal]
command = 'personal-command'
`

func productionSharedDefaults(withWindow bool) Object {
	defaults := Object{
		"features.memories":            true,
		"memories.generate_memories":   true,
		"memories.use_memories":        true,
		"memories.extract_model":       "gpt-6.1-sol",
		"memories.consolidation_model": "gpt-6.1-sol",
	}
	if withWindow {
		defaults["model_context_window"] = int64(512000)
	}
	return defaults
}

func productionSharedRecord(representation string) Object {
	if representation == "" {
		return Object{"present": false}
	}
	return Object{"present": true, "representation": representation}
}

func productionSharedOriginal(existed bool) Object {
	if !existed {
		return Object{
			"features.memories":            Object{"present": false},
			"memories.generate_memories":   Object{"present": false},
			"memories.use_memories":        Object{"present": false},
			"memories.extract_model":       Object{"present": false},
			"memories.consolidation_model": Object{"present": false},
		}
	}
	return Object{
		"features.memories":            Object{"present": true, "representation": "false"},
		"memories.generate_memories":   Object{"present": false},
		"memories.use_memories":        Object{"present": true, "representation": "false"},
		"memories.extract_model":       Object{"present": true, "representation": "'personal-extract'"},
		"memories.consolidation_model": Object{"present": true, "representation": "'''personal consolidation'''"},
	}
}

func productionSharedFrozenExpected() Object {
	return Object{
		"features.memories":            Object{"present": true, "representation": "true"},
		"memories.generate_memories":   Object{"present": true, "representation": "true"},
		"memories.use_memories":        Object{"present": true, "representation": "true"},
		"memories.extract_model":       Object{"present": true, "representation": `"legacy-extract"`},
		"memories.consolidation_model": Object{"present": true, "representation": `"legacy-consolidation"`},
	}
}

func productionSharedCurrentExpected(withWindow bool) Object {
	expected := Object{
		"features.memories":            Object{"present": true, "representation": "true"},
		"memories.generate_memories":   Object{"present": true, "representation": "true"},
		"memories.use_memories":        Object{"present": true, "representation": "true"},
		"memories.extract_model":       Object{"present": true, "representation": `"gpt-6.1-sol"`},
		"memories.consolidation_model": Object{"present": true, "representation": `"gpt-6.1-sol"`},
	}
	if withWindow {
		expected["model_context_window"] = productionSharedRecord("512000")
	}
	return expected
}

func productionSharedFrozenState(existed bool) Object {
	return Object{
		"config_original_exists": existed,
		"shared_config": Object{
			"original":        productionSharedOriginal(existed),
			"expected":        productionSharedFrozenExpected(),
			"original_exists": existed,
		},
	}
}

func productionSharedManagedDocument(window string) []byte {
	prefix := "# independent personal comment\nmodel = 'device-model'\nmodel_reasoning_effort = 'high'\n"
	if window != "" {
		prefix += "model_context_window = " + window + " # personal window\n"
	}
	return []byte(prefix + `
[profiles.personal]
model = 'profile-model'

[features]
memories = true
device_flag = 'keep-feature'

[memories]
generate_memories = true
use_memories = true
extract_model = "legacy-extract"
consolidation_model = "legacy-consolidation"
device_setting = 'keep-memory'

[mcp_servers.personal]
command = 'personal-command'
`)
}

func productionSharedRead(test *testing.T, path string) Object {
	test.Helper()
	var values Object
	if err := toml.Unmarshal(modelTestRead(test, path), &values); err != nil {
		test.Fatalf("rendered config is not TOML: %v", err)
	}
	return values
}

type productionSharedTokenCase struct {
	key            string
	representation string
}

func productionSharedValidTokenCases() []productionSharedTokenCase {
	var cases []productionSharedTokenCase
	for _, key := range []string{"features.memories", "memories.generate_memories", "memories.use_memories"} {
		for _, representation := range []string{"false", "true"} {
			cases = append(cases, productionSharedTokenCase{key, representation})
		}
	}
	for _, key := range []string{"memories.extract_model", "memories.consolidation_model"} {
		for _, representation := range []string{
			"'personal'", `"escaped\ntext"`, "'''line one\nline two'''", "\"\"\"line one\nline two\"\"\"",
			"''", `""`, "'   '", `"\t "`,
		} {
			cases = append(cases, productionSharedTokenCase{key, representation})
		}
	}
	for _, representation := range []string{
		"1", "17", "0x10", "1_000", "512000", "+65_536", "0x1_0000", "0o200_000", "0b1_0000_0000_0000_0000", "+262_144", "9223372036854775807",
	} {
		cases = append(cases, productionSharedTokenCase{"model_context_window", representation})
	}
	return cases
}

func productionSharedInvalidTypedTokenCases() []productionSharedTokenCase {
	var cases []productionSharedTokenCase
	for _, key := range []string{"features.memories", "memories.generate_memories", "memories.use_memories"} {
		for _, representation := range []string{
			"'true'", "17", "-17", "0x10", "1_000", "1.5", "[true, false]", "[1, 'two']", "[1, 'two', { ok = true }]",
			"{ label = 'device', limit = 7 }", "1979-05-27T07:32:00Z", "+inf", "nan",
		} {
			cases = append(cases, productionSharedTokenCase{key, representation})
		}
	}
	for _, key := range []string{"memories.extract_model", "memories.consolidation_model"} {
		for _, representation := range []string{
			"true", "false", "17", "-17", "0x10", "1_000", "1.5", "[true, false]", "[1, 'two']", "[1, 'two', { ok = true }]",
			"{ label = 'device', limit = 7 }", "1979-05-27T07:32:00Z", "+inf", "nan",
		} {
			cases = append(cases, productionSharedTokenCase{key, representation})
		}
	}
	for _, representation := range []string{
		"true", "false", "0", "-1", "-17", "512000.0", "+inf", "nan", "1979-05-27T07:32:00Z", "9223372036854775808",
		"'512000'", "''", "'   '", "'personal-window'", "'latest personal context'", "'personal root window'", "'unowned personal window'",
		"'never owned by the five-key release'", "'legacy personal context'", "'personal context after release'",
		"[]", "[true, false]", "[1, 'two']", "[1, 'two', { ok = true }]", "{ label = 'device', limit = 7 }",
	} {
		cases = append(cases, productionSharedTokenCase{"model_context_window", representation})
	}
	return cases
}

func productionSharedTypedMetadata(key string) Object {
	metadata := object(productionSharedFrozenState(true)["shared_config"])
	if key == "model_context_window" {
		object(metadata["original"])[key] = productionSharedRecord("")
		object(metadata["expected"])[key] = productionSharedRecord("512000")
	}
	return metadata
}

func productionSharedPersonalTokenDocument(key, representation string) []byte {
	prefix := "# independent token comment\n"
	table, field, nested := strings.Cut(key, ".")
	if nested {
		return []byte(prefix + "[" + table + "]\n" + field + " = " + representation + "\n")
	}
	return []byte(prefix + key + " = " + representation + "\n")
}

func productionSharedTokenValue(test *testing.T, representation string) any {
	test.Helper()
	var values Object
	if err := toml.Unmarshal([]byte("value = "+representation+"\n"), &values); err != nil || len(values) != 1 {
		test.Fatalf("snapshot does not contain a single TOML value: %q: %v", representation, err)
	}
	value, present := values["value"]
	if !present {
		test.Fatalf("snapshot representation did not encode a value: %q", representation)
	}
	return value
}

func productionSharedAssertKeyMap(test *testing.T, actual, expected Object) {
	test.Helper()
	if len(actual) != len(expected) {
		test.Fatalf("wrong ownership key set: got=%#v want=%#v", actual, expected)
	}
	for key, raw := range expected {
		actualRaw, present := actual[key]
		if !present {
			test.Fatalf("ownership key missing: %s", key)
		}
		want := object(raw)
		got := object(actualRaw)
		if got["present"] != want["present"] || len(got) != len(want) {
			test.Fatalf("wrong snapshot record for %s: got=%#v want=%#v", key, got, want)
		}
		if want["present"] == true {
			actualValue := productionSharedTokenValue(test, text(got["representation"]))
			expectedValue := productionSharedTokenValue(test, text(want["representation"]))
			if !reflect.DeepEqual(actualValue, expectedValue) {
				test.Fatalf("wrong owned value for %s: got=%#v want=%#v", key, actualValue, expectedValue)
			}
		}
	}
}

func productionSharedAssertValues(test *testing.T, path string, expected Object) {
	test.Helper()
	values := productionSharedRead(test, path)
	for key, want := range expected {
		got, present := sharedInstallerLookup(values, key)
		if !present || !reflect.DeepEqual(got, want) {
			test.Fatalf("config value %s: got=%#v present=%v want=%#v", key, got, present, want)
		}
	}
}

func productionSharedAssertSnapshotOnDisk(test *testing.T, path string, expected Object) {
	test.Helper()
	values := productionSharedRead(test, path)
	for key, raw := range expected {
		record := object(raw)
		got, present := sharedInstallerLookup(values, key)
		if record["present"] == false {
			if present {
				test.Fatalf("released absent key remains: %s=%#v", key, got)
			}
			continue
		}
		want := productionSharedTokenValue(test, text(record["representation"]))
		if !present || !reflect.DeepEqual(got, want) {
			test.Fatalf("personal value not restored for %s: got=%#v present=%v want=%#v", key, got, present, want)
		}
		if !strings.Contains(string(modelTestRead(test, path)), text(record["representation"])) {
			test.Fatalf("personal original token encoding lost for %s", key)
		}
	}
}

func productionSharedAssertPersonal(test *testing.T, path string) {
	test.Helper()
	data := string(modelTestRead(test, path))
	for _, fragment := range []string{
		"# independent personal comment\n",
		"[profiles.personal]\n",
		"model = 'profile-model'\n",
		"device_flag = 'keep-feature'\n",
		"device_setting = 'keep-memory'\n",
		"[mcp_servers.personal]\n",
		"command = 'personal-command'\n",
	} {
		if !strings.Contains(data, fragment) {
			test.Fatalf("unowned personal bytes changed: %q", fragment)
		}
	}
	productionSharedAssertValues(test, path, Object{
		"profiles.personal.model":      "profile-model",
		"features.device_flag":         "keep-feature",
		"memories.device_setting":      "keep-memory",
		"mcp_servers.personal.command": "personal-command",
	})
}

func productionSharedAssertOperation(test *testing.T, path string, operation, before, after Object, label string) {
	test.Helper()
	if err := ConfigValidateOperation(operation); err != nil {
		test.Fatalf("valid keyed operation rejected: %#v: %v", operation, err)
	}
	fields := []string{"kind", "path", "before_keys", "after_keys", "before_exists", "after_exists", "before_mode", "after_mode", "label"}
	if len(operation) != len(fields) {
		test.Fatalf("keyed-operation envelope changed: %#v", operation)
	}
	for _, field := range fields {
		if _, present := operation[field]; !present {
			test.Fatalf("keyed-operation field missing: %s", field)
		}
	}
	if operation["kind"] != "codex_config" || operation["path"] != path || operation["label"] != label {
		test.Fatalf("wrong config operation identity: %#v", operation)
	}
	productionSharedAssertKeyMap(test, object(operation["before_keys"]), before)
	productionSharedAssertKeyMap(test, object(operation["after_keys"]), after)
}

func productionSharedSetWindow(test *testing.T, path, representation string) {
	test.Helper()
	data := modelTestRead(test, path)
	info, err := os.Stat(path)
	if err != nil {
		test.Fatal(err)
	}
	lines := strings.Split(string(data), "\n")
	replacement := "model_context_window = " + representation + " # personal window"
	for index, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") {
			break
		}
		name, _, assignment := strings.Cut(trimmed, "=")
		if assignment && strings.TrimSpace(name) == "model_context_window" {
			lines[index] = replacement
			modelTestWrite(test, path, []byte(strings.Join(lines, "\n")), info.Mode().Perm())
			return
		}
	}
	modelTestWrite(test, path, []byte(replacement+"\n"+string(data)), info.Mode().Perm())
}

func productionSharedLiteralOperation(path string, existed bool) Object {
	beforeMode := 0
	if existed {
		beforeMode = 0o640
	}
	return Object{
		"kind":          "codex_config",
		"path":          path,
		"before_keys":   productionSharedOriginal(existed),
		"after_keys":    productionSharedFrozenExpected(),
		"before_exists": existed,
		"after_exists":  true,
		"before_mode":   beforeMode,
		"after_mode":    0o640,
		"label":         "codex-config",
	}
}

func TestProductionSharedResourceAcceptsExactlyFiveOrSixTypedKeys(test *testing.T) {
	root := test.TempDir()
	path := filepath.Join(root, filepath.FromSlash(sharedConfigResource))
	if got, err := SharedConfigDefaults(root); err != nil || got != nil {
		test.Fatalf("absent optional resource changed historical behavior: %#v, %v", got, err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		test.Fatal(err)
	}
	for _, entry := range []struct {
		name     string
		resource string
		window   int64
	}{
		{"five", productionSharedFiveResource, 0},
		{"six", strings.Replace(productionSharedFiveResource, "version = 1\n", "version = 1\nmodel_context_window = 512000\n", 1), 512000},
		{"minimum", strings.Replace(productionSharedFiveResource, "version = 1\n", "version = 1\nmodel_context_window = 1\n", 1), 1},
		{"maximum-int64", strings.Replace(productionSharedFiveResource, "version = 1\n", "version = 1\nmodel_context_window = 9223372036854775807\n", 1), 9223372036854775807},
	} {
		test.Run(entry.name, func(test *testing.T) {
			modelTestWrite(test, path, []byte(entry.resource), 0o644)
			want := productionSharedDefaults(entry.window != 0)
			if entry.window != 0 {
				want["model_context_window"] = entry.window
			}
			if got, err := SharedConfigDefaults(root); err != nil || !reflect.DeepEqual(got, want) {
				test.Fatalf("allowed resource rejected: got=%#v want=%#v err=%v", got, want, err)
			}
		})
	}
	invalid := []string{
		strings.Replace(productionSharedFiveResource, "version = 1\n", "", 1),
		strings.Replace(productionSharedFiveResource, "version = 1", "version = 2", 1),
		strings.Replace(productionSharedFiveResource, "version = 1", "version = 1.0", 1),
		strings.Replace(productionSharedFiveResource, "version = 1", "version = '1'", 1),
		strings.Replace(productionSharedFiveResource, "version = 1", "version = true", 1),
		strings.Replace(productionSharedFiveResource, "version = 1\n", "version = 1\nmodel = 'unowned'\n", 1),
		strings.Replace(productionSharedFiveResource, "[features]\nmemories = true", "[features]\nmemories = true\nunknown_feature = true", 1),
		productionSharedFiveResource + "unknown_memory = true\n",
		productionSharedFiveResource + "\n[unknown]\nvalue = true\n",
		strings.Replace(productionSharedFiveResource, "[features]\nmemories = true", "features = true", 1),
		strings.Replace(productionSharedFiveResource, "memories = true", "memories = 'true'", 1),
		strings.Replace(productionSharedFiveResource, "generate_memories = true", "generate_memories = 1", 1),
		strings.Replace(productionSharedFiveResource, "use_memories = true", "use_memories = []", 1),
		strings.Replace(productionSharedFiveResource, "extract_model = 'gpt-6.1-sol'", "extract_model = ''", 1),
		strings.Replace(productionSharedFiveResource, "extract_model = 'gpt-6.1-sol'", "extract_model = '   '", 1),
		strings.Replace(productionSharedFiveResource, "extract_model = 'gpt-6.1-sol'", "extract_model = 7", 1),
		strings.Replace(productionSharedFiveResource, "consolidation_model = 'gpt-6.1-sol'", "consolidation_model = ''", 1),
		strings.Replace(productionSharedFiveResource, "consolidation_model = 'gpt-6.1-sol'", "consolidation_model = '   '", 1),
		strings.Replace(productionSharedFiveResource, "consolidation_model = 'gpt-6.1-sol'", "consolidation_model = 7", 1),
	}
	for _, representation := range []string{"0", "-1", "512000.0", "'512000'", "true", "9223372036854775808"} {
		invalid = append(invalid, strings.Replace(productionSharedFiveResource, "version = 1\n", "version = 1\nmodel_context_window = "+representation+"\n", 1))
	}
	for _, required := range []string{"memories = true\n", "generate_memories = true\n", "use_memories = true\n", "extract_model = 'gpt-6.1-sol'\n", "consolidation_model = 'gpt-6.1-sol'\n"} {
		partial := strings.Replace(productionSharedFiveResource, required, "", 1)
		invalid = append(invalid, partial, strings.Replace(partial, "version = 1\n", "version = 1\nmodel_context_window = 512000\n", 1))
	}
	for index, resource := range invalid {
		test.Run(fmt.Sprintf("invalid-%d", index), func(test *testing.T) {
			modelTestWrite(test, path, []byte(resource), 0o644)
			if _, err := SharedConfigDefaults(root); err == nil {
				test.Fatalf("invalid resource accepted: %q", resource)
			}
		})
	}
	if err := os.Remove(path); err != nil {
		test.Fatal(err)
	}
	target := filepath.Join(filepath.Dir(path), "production-valid-resource-target.toml")
	modelTestWrite(test, target, []byte(productionSharedFiveResource), 0o644)
	if err := os.Symlink(filepath.Base(target), path); err != nil {
		test.Fatal(err)
	}
	if _, err := SharedConfigDefaults(root); err == nil {
		test.Fatal("symlink to an otherwise valid resource accepted")
	}
	if err := os.Remove(path); err != nil {
		test.Fatal(err)
	}
	if got, err := SharedConfigDefaults(root); err != nil || got != nil {
		test.Fatalf("optional resource absence changed after removal: %#v, %v", got, err)
	}
	if err := os.Mkdir(path, 0o755); err != nil {
		test.Fatal(err)
	}
	if _, err := SharedConfigDefaults(root); err == nil {
		test.Fatal("directory resource accepted")
	}
}

func TestProductionSharedMetadataAcceptsLiteralHistoricalSetsAndSafeOriginals(test *testing.T) {
	for _, existed := range []bool{false, true} {
		for _, withWindow := range []bool{false, true} {
			test.Run(fmt.Sprintf("origin-%t/window-%t", existed, withWindow), func(test *testing.T) {
				metadata := object(productionSharedFrozenState(existed)["shared_config"])
				if withWindow {
					object(metadata["original"])["model_context_window"] = productionSharedRecord("")
					object(metadata["expected"])["model_context_window"] = productionSharedRecord("512000")
				}
				if err := SharedConfigValidateMetadata(metadata); err != nil {
					test.Fatalf("literal complete historical metadata rejected: %#v: %v", metadata, err)
				}
			})
		}
	}
	for _, entry := range productionSharedValidTokenCases() {
		for _, field := range []string{"original", "expected"} {
			if value, stringValue := productionSharedTokenValue(test, entry.representation).(string); field == "expected" && stringValue && strings.TrimSpace(value) == "" {
				continue
			}
			test.Run(field+"/"+entry.key+"/"+entry.representation, func(test *testing.T) {
				metadata := productionSharedTypedMetadata(entry.key)
				object(metadata[field])[entry.key] = productionSharedRecord(entry.representation)
				before := clone(metadata)
				if err := SharedConfigValidateMetadata(metadata); err != nil {
					test.Fatalf("type-correct token rejected for %s: %q: %v", entry.key, entry.representation, err)
				}
				if !equal(metadata, before) {
					test.Fatal("metadata validation changed a literal token or ownership record")
				}
			})
		}
	}
}

func TestProductionSharedMetadataRejectsNonliteralAndInjectedOwnership(test *testing.T) {
	cases := []struct {
		name   string
		mutate func(Object)
	}{
		{"missing-original", func(metadata Object) { delete(metadata, "original") }},
		{"missing-expected", func(metadata Object) { delete(metadata, "expected") }},
		{"missing-origin", func(metadata Object) { delete(metadata, "original_exists") }},
		{"extra-metadata", func(metadata Object) { metadata["future"] = true }},
		{"nonboolean-origin", func(metadata Object) { metadata["original_exists"] = "true" }},
		{"mismatched-five-six", func(metadata Object) {
			object(metadata["expected"])["model_context_window"] = productionSharedRecord("512000")
		}},
		{"partial-both", func(metadata Object) {
			delete(object(metadata["original"]), "memories.use_memories")
			delete(object(metadata["expected"]), "memories.use_memories")
		}},
		{"unknown-both", func(metadata Object) {
			object(metadata["original"])["memories.future"] = productionSharedRecord("")
			object(metadata["expected"])["memories.future"] = productionSharedRecord("true")
		}},
		{"unknown-in-place-of-known", func(metadata Object) {
			for _, field := range []string{"original", "expected"} {
				snapshot := object(metadata[field])
				snapshot["memories.future"] = snapshot["memories.use_memories"]
				delete(snapshot, "memories.use_memories")
			}
		}},
		{"absent-with-representation", func(metadata Object) {
			object(metadata["original"])["memories.generate_memories"] = Object{"present": false, "representation": "false"}
		}},
		{"present-without-representation", func(metadata Object) {
			object(metadata["original"])["memories.extract_model"] = Object{"present": true}
		}},
		{"nonboolean-present", func(metadata Object) {
			object(metadata["original"])["memories.extract_model"] = Object{"present": "true", "representation": "'personal'"}
		}},
		{"extra-snapshot-field", func(metadata Object) {
			object(metadata["expected"])["features.memories"] = Object{"present": true, "representation": "true", "future": false}
		}},
	}
	for _, entry := range cases {
		test.Run(entry.name, func(test *testing.T) {
			metadata := object(productionSharedFrozenState(true)["shared_config"])
			entry.mutate(metadata)
			if err := SharedConfigValidateMetadata(metadata); err == nil {
				test.Fatalf("corrupt historical ownership accepted: %#v", metadata)
			}
		})
	}
	for _, representation := range []string{"", "true trailing", "[1", "'unterminated", "true\nmodel = 'injected'", "true # comment\n[foreign]", "'safe'\nmemories.extract_model = 'injected'"} {
		for _, field := range []string{"original", "expected"} {
			test.Run(field+"/"+representation, func(test *testing.T) {
				metadata := object(productionSharedFrozenState(true)["shared_config"])
				object(metadata[field])["memories.extract_model"] = Object{"present": true, "representation": representation}
				if err := SharedConfigValidateMetadata(metadata); err == nil {
					test.Fatalf("non-value or injected representation accepted: %q", representation)
				}
			})
		}
	}
}

func TestProductionSharedOriginalTokensSurviveAcquisitionAndRemoval(test *testing.T) {
	for _, entry := range productionSharedValidTokenCases() {
		test.Run(entry.key+"/"+entry.representation, func(test *testing.T) {
			path := filepath.Join(test.TempDir(), "config.toml")
			personal := productionSharedPersonalTokenDocument(entry.key, entry.representation)
			modelTestWrite(test, path, personal, 0o640)
			state, operation, err := configPrepare(path, nil, productionSharedDefaults(true), Object{})
			if err != nil || operation == nil {
				test.Fatalf("personal token acquisition failed: %#v, %v", operation, err)
			}
			if !bytes.Equal(modelTestRead(test, path), personal) {
				test.Fatal("personal token preparation wrote the config")
			}
			metadata := object(state["shared_config"])
			original := object(object(metadata["original"])[entry.key])
			if !reflect.DeepEqual(original, productionSharedRecord(entry.representation)) {
				test.Fatalf("personal token encoding was not captured verbatim for %s: %#v", entry.key, original)
			}
			if err := SharedConfigValidateMetadata(metadata); err != nil {
				test.Fatal(err)
			}
			configTestApply(test, path, operation)
			productionSharedAssertValues(test, path, productionSharedDefaults(true))
			removal, err := ConfigRemoval(path, state)
			if err != nil || removal == nil {
				test.Fatalf("personal token removal failed: %#v, %v", removal, err)
			}
			configTestApply(test, path, removal)
			productionSharedAssertSnapshotOnDisk(test, path, object(metadata["original"]))
			if !strings.Contains(string(modelTestRead(test, path)), "# independent token comment\n") {
				test.Fatal("personal comment was lost during token restoration")
			}
			sharedInstallerAssertMode(test, path, 0o640)
		})
	}
}

func TestProductionSharedMetadataRejectsWrongKeyTypes(test *testing.T) {
	for _, entry := range productionSharedInvalidTypedTokenCases() {
		for _, field := range []string{"original", "expected"} {
			test.Run(field+"/"+entry.key+"/"+entry.representation, func(test *testing.T) {
				metadata := productionSharedTypedMetadata(entry.key)
				object(metadata[field])[entry.key] = productionSharedRecord(entry.representation)
				before := clone(metadata)
				if err := SharedConfigValidateMetadata(metadata); err == nil {
					test.Fatalf("wrong-type or out-of-range token accepted for %s: %q", entry.key, entry.representation)
				}
				if !equal(metadata, before) {
					test.Fatal("typed metadata refusal changed caller ownership")
				}
			})
		}
	}
}

func TestProductionSharedOperationsRejectWrongKeyTypesBeforeMutation(test *testing.T) {
	for _, entry := range productionSharedInvalidTypedTokenCases() {
		for _, field := range []string{"before_keys", "after_keys"} {
			test.Run(field+"/"+entry.key+"/"+entry.representation, func(test *testing.T) {
				path := filepath.Join(test.TempDir(), "config.toml")
				initial := []byte(productionSharedPersonalDocument)
				modelTestWrite(test, path, initial, 0o640)
				operation := productionSharedLiteralOperation(path, true)
				if entry.key == "model_context_window" {
					object(operation["before_keys"])[entry.key] = productionSharedRecord("")
					object(operation["after_keys"])[entry.key] = productionSharedRecord("512000")
				}
				if err := ConfigValidateOperation(operation); err != nil {
					test.Fatalf("literal operation seed rejected before type corruption: %v", err)
				}
				object(operation[field])[entry.key] = productionSharedRecord(entry.representation)
				before := clone(operation)
				if err := ConfigValidateOperation(operation); err == nil {
					test.Fatalf("wrong-key-type operation validated: %s=%q", entry.key, entry.representation)
				}
				if _, _, _, err := ConfigRender(path, operation); err == nil {
					test.Fatal("render accepted wrong-key-type operation")
				}
				if _, err := ConfigPending(path, operation); err == nil {
					test.Fatal("pending accepted wrong-key-type operation")
				}
				if _, err := ConfigReversal(path, operation); err == nil {
					test.Fatal("reversal accepted wrong-key-type operation")
				}
				if !bytes.Equal(modelTestRead(test, path), initial) || !equal(operation, before) {
					test.Fatal("typed operation refusal mutated config or caller operation")
				}
				sharedInstallerAssertMode(test, path, 0o640)
			})
		}
	}
}

func TestProductionSharedAcquisitionRejectsWrongKeyTypesBeforeMutation(test *testing.T) {
	for _, entry := range productionSharedInvalidTypedTokenCases() {
		test.Run(entry.key+"/"+entry.representation, func(test *testing.T) {
			path := filepath.Join(test.TempDir(), "config.toml")
			personal := productionSharedPersonalTokenDocument(entry.key, entry.representation)
			modelTestWrite(test, path, personal, 0o640)
			state := Object{}
			stateBefore := clone(state)
			defaults := productionSharedDefaults(true)
			defaultsBefore := clone(defaults)
			if _, _, err := configPrepare(path, nil, defaults, state); err == nil {
				test.Fatalf("wrong-type or out-of-range personal token acquired: %s=%q", entry.key, entry.representation)
			}
			if !bytes.Equal(modelTestRead(test, path), personal) || !equal(state, stateBefore) || !equal(defaults, defaultsBefore) {
				test.Fatal("typed acquisition refusal changed config or caller inputs")
			}
			sharedInstallerAssertMode(test, path, 0o640)
		})
	}
}

func TestProductionSharedFiveSixReleaseAndReacquisitionUseTrueOwnership(test *testing.T) {
	for _, existed := range []bool{false, true} {
		for _, window := range []struct {
			name  string
			token string
		}{{"absent", ""}, {"personal-decimal", "131_072"}, {"personal-integer-token", "+65_536"}} {
			test.Run(fmt.Sprintf("origin-%t/%s", existed, window.name), func(test *testing.T) {
				path := filepath.Join(test.TempDir(), "config.toml")
				initial := productionSharedManagedDocument(window.token)
				modelTestWrite(test, path, initial, 0o640)
				state := productionSharedFrozenState(existed)
				legacy := object(state["shared_config"])
				if err := SharedConfigValidateMetadata(legacy); err != nil {
					test.Fatalf("frozen five-key ownership is not readable: %v", err)
				}
				if err := SharedConfigVerify(path, legacy); err != nil {
					test.Fatalf("an unowned personal context window was treated as drift: %v", err)
				}
				wantBefore := productionSharedFrozenExpected()
				wantBefore["model_context_window"] = productionSharedRecord(window.token)
				wantAfter := productionSharedCurrentExpected(true)
				next, operation, err := configPrepare(path, nil, productionSharedDefaults(true), state)
				if err != nil || operation == nil {
					test.Fatalf("five-to-six acquisition failed: %#v, %v", operation, err)
				}
				if !bytes.Equal(modelTestRead(test, path), initial) {
					test.Fatal("configPrepare mutated the file")
				}
				productionSharedAssertOperation(test, path, operation, wantBefore, wantAfter, "codex-config")
				if operation["before_exists"] != true || operation["after_exists"] != true || integer(operation["before_mode"]) != 0o640 || integer(operation["after_mode"]) != 0o640 {
					test.Fatalf("acquisition lost current existence or mode: %#v", operation)
				}
				metadata := object(next["shared_config"])
				wantOriginal := productionSharedOriginal(existed)
				wantOriginal["model_context_window"] = productionSharedRecord(window.token)
				if !reflect.DeepEqual(object(metadata["original"]), wantOriginal) {
					test.Fatalf("acquisition rewrote retained originals or missed the personal new key: %#v", metadata)
				}
				productionSharedAssertKeyMap(test, object(metadata["expected"]), wantAfter)
				if next["config_original_exists"] != existed {
					test.Fatal("acquisition changed the earliest outer config origin")
				}
				if window.token != "" && metadata["original_exists"] != true {
					test.Fatal("personal acquisition did not promote the shared group origin")
				}
				if err := SharedConfigValidateMetadata(metadata); err != nil {
					test.Fatalf("acquisition produced unreadable ownership: %v", err)
				}
				if pending, err := ConfigPending(path, operation); err != nil || pending == nil || !equal(pending, operation) {
					test.Fatalf("before snapshot was not pending: %#v, %v", pending, err)
				}
				if reversal, err := ConfigReversal(path, operation); err != nil || reversal != nil {
					test.Fatalf("already-restored before snapshot required reversal: %#v, %v", reversal, err)
				}
				if _, mode, remove, err := ConfigRender(path, operation); err != nil || remove || mode.Perm() != 0o640 {
					test.Fatalf("acquisition render: mode=%o remove=%v err=%v", mode, remove, err)
				}
				if !bytes.Equal(modelTestRead(test, path), initial) {
					test.Fatal("ConfigRender wrote the file")
				}
				configTestApply(test, path, operation)
				productionSharedAssertValues(test, path, productionSharedDefaults(true))
				productionSharedAssertValues(test, path, Object{"model": "device-model", "model_reasoning_effort": "high"})
				productionSharedAssertPersonal(test, path)
				if err := SharedConfigVerify(path, metadata); err != nil {
					test.Fatal(err)
				}
				if repeated, pending, err := configPrepare(path, nil, productionSharedDefaults(true), next); err != nil || pending != nil || !reflect.DeepEqual(object(object(repeated["shared_config"])["original"]), wantOriginal) {
					test.Fatalf("unchanged six-key repeat changed ownership: %#v, %v", pending, err)
				}
				installerAppend(test, path, "\n# personal edit while operation is applied\n")
				if pending, err := ConfigPending(path, operation); err != nil || pending != nil {
					test.Fatalf("applied operation with an unowned edit remained pending: %#v, %v", pending, err)
				}
				reversal, err := ConfigReversal(path, operation)
				if err != nil || reversal == nil {
					test.Fatalf("union-key reversal failed: %#v, %v", reversal, err)
				}
				productionSharedAssertOperation(test, path, reversal, wantAfter, wantBefore, "recover-codex-config")
				configTestApply(test, path, reversal)
				productionSharedAssertSnapshotOnDisk(test, path, wantBefore)
				productionSharedAssertPersonal(test, path)
				if err := SharedConfigVerify(path, legacy); err != nil {
					test.Fatalf("reversal did not restore frozen five-key ownership: %v", err)
				}
				if repeated, err := ConfigReversal(path, operation); err != nil || repeated != nil {
					test.Fatalf("repeated reversal was not a no-op: %#v, %v", repeated, err)
				}
				if pending, err := ConfigPending(path, operation); err != nil || pending == nil {
					test.Fatalf("reversed operation could not be retried: %#v, %v", pending, err)
				}
				configTestApply(test, path, operation)
				released, release, err := configPrepare(path, nil, productionSharedDefaults(false), next)
				if err != nil || release == nil {
					test.Fatalf("six-to-five release failed: %#v, %v", release, err)
				}
				wantReleased := productionSharedCurrentExpected(false)
				wantReleased["model_context_window"] = productionSharedRecord(window.token)
				productionSharedAssertOperation(test, path, release, wantAfter, wantReleased, "codex-config")
				releasedMetadata := object(released["shared_config"])
				if !reflect.DeepEqual(object(releasedMetadata["original"]), productionSharedOriginal(existed)) || released["config_original_exists"] != existed {
					test.Fatalf("release rewrote retained originals or the earliest origin: %#v", released)
				}
				productionSharedAssertKeyMap(test, object(releasedMetadata["expected"]), productionSharedCurrentExpected(false))
				if err := SharedConfigValidateMetadata(releasedMetadata); err != nil {
					test.Fatal(err)
				}
				configTestApply(test, path, release)
				productionSharedAssertValues(test, path, productionSharedDefaults(false))
				productionSharedAssertSnapshotOnDisk(test, path, Object{"model_context_window": productionSharedRecord(window.token)})
				if _, pending, err := configPrepare(path, nil, productionSharedDefaults(false), released); err != nil || pending != nil {
					test.Fatalf("unchanged five-key repeat mutated config: %#v, %v", pending, err)
				}
				const latest = "+262_144"
				productionSharedSetWindow(test, path, latest)
				reacquired, reacquisition, err := configPrepare(path, nil, productionSharedDefaults(true), released)
				if err != nil || reacquisition == nil {
					test.Fatalf("reacquisition rejected a newly personal window: %#v, %v", reacquisition, err)
				}
				wantReacquiredBefore := productionSharedCurrentExpected(false)
				wantReacquiredBefore["model_context_window"] = productionSharedRecord(latest)
				productionSharedAssertOperation(test, path, reacquisition, wantReacquiredBefore, wantAfter, "codex-config")
				wantLatestOriginal := productionSharedOriginal(existed)
				wantLatestOriginal["model_context_window"] = productionSharedRecord(latest)
				reacquiredMetadata := object(reacquired["shared_config"])
				if !reflect.DeepEqual(object(reacquiredMetadata["original"]), wantLatestOriginal) || reacquired["config_original_exists"] != existed {
					test.Fatalf("reacquisition reused a released original: %#v", reacquired)
				}
				if err := SharedConfigValidateMetadata(reacquiredMetadata); err != nil {
					test.Fatal(err)
				}
				configTestApply(test, path, reacquisition)
				removal, err := ConfigRemoval(path, reacquired)
				if err != nil || removal == nil {
					test.Fatalf("removal failed: %#v, %v", removal, err)
				}
				productionSharedAssertOperation(test, path, removal, wantAfter, wantLatestOriginal, "codex-config")
				configTestApply(test, path, removal)
				productionSharedAssertSnapshotOnDisk(test, path, wantLatestOriginal)
				productionSharedAssertPersonal(test, path)
				productionSharedAssertValues(test, path, Object{"model": "device-model", "model_reasoning_effort": "high", "model_context_window": int64(262144)})
				if !strings.Contains(string(modelTestRead(test, path)), "# personal edit while operation is applied\n") {
					test.Fatal("recovery/release/removal discarded an independent edit")
				}
				sharedInstallerAssertMode(test, path, 0o640)
			})
		}
	}
}

func TestProductionSharedRootModelPairIsIncludedOnlyWhenManaged(test *testing.T) {
	path := filepath.Join(test.TempDir(), "config.toml")
	const personalWindow = "0x10000"
	modelTestWrite(test, path, productionSharedManagedDocument(personalWindow), 0o640)
	state := productionSharedFrozenState(true)
	models := map[string]string{"model": "root-next", "model_reasoning_effort": "max"}
	next, operation, err := configPrepare(path, models, productionSharedDefaults(true), state)
	if err != nil || operation == nil {
		test.Fatalf("combined model/shared acquisition failed: %#v, %v", operation, err)
	}
	before := productionSharedFrozenExpected()
	before["model_context_window"] = productionSharedRecord(personalWindow)
	before["model"] = productionSharedRecord("'device-model'")
	before["model_reasoning_effort"] = productionSharedRecord("'high'")
	after := productionSharedCurrentExpected(true)
	after["model"] = productionSharedRecord(`"root-next"`)
	after["model_reasoning_effort"] = productionSharedRecord(`"max"`)
	productionSharedAssertOperation(test, path, operation, before, after, "codex-config")
	configTestApply(test, path, operation)
	productionSharedAssertValues(test, path, Object{"model": "root-next", "model_reasoning_effort": "max"})
	productionSharedAssertValues(test, path, productionSharedDefaults(true))
	productionSharedAssertPersonal(test, path)
	if _, repeated, err := configPrepare(path, models, productionSharedDefaults(true), next); err != nil || repeated != nil {
		test.Fatalf("combined unchanged repeat was not a no-op: %#v, %v", repeated, err)
	}
	removal, err := ConfigRemoval(path, next)
	if err != nil || removal == nil {
		test.Fatalf("combined removal failed: %#v, %v", removal, err)
	}
	original := productionSharedOriginal(true)
	original["model_context_window"] = productionSharedRecord(personalWindow)
	original["model"] = productionSharedRecord("'device-model'")
	original["model_reasoning_effort"] = productionSharedRecord("'high'")
	productionSharedAssertOperation(test, path, removal, after, original, "codex-config")
	configTestApply(test, path, removal)
	productionSharedAssertSnapshotOnDisk(test, path, original)
	productionSharedAssertPersonal(test, path)
	sharedInstallerAssertMode(test, path, 0o640)
}

func TestProductionSharedUnknownAndOwnedDriftErrorsPrecedeMutation(test *testing.T) {
	cases := []struct {
		name           string
		mutateState    func(Object)
		mutateDefaults func(Object)
		drift          bool
	}{
		{"unknown-default", nil, func(defaults Object) { defaults["memories.future"] = true }, false},
		{"partial-default", nil, func(defaults Object) { delete(defaults, "memories.use_memories") }, false},
		{"partial-owned-state", func(state Object) {
			delete(object(object(state["shared_config"])["original"]), "memories.use_memories")
		}, nil, false},
		{"unknown-owned-state", func(state Object) {
			metadata := object(state["shared_config"])
			object(metadata["original"])["memories.future"] = productionSharedRecord("")
			object(metadata["expected"])["memories.future"] = productionSharedRecord("true")
		}, nil, false},
		{"owned-drift", nil, nil, true},
	}
	for _, entry := range cases {
		test.Run(entry.name, func(test *testing.T) {
			path := filepath.Join(test.TempDir(), "config.toml")
			data := productionSharedManagedDocument("131_072")
			if entry.drift {
				data = []byte(strings.Replace(string(data), "use_memories = true\n", "use_memories = false\n", 1))
			}
			modelTestWrite(test, path, data, 0o640)
			state := productionSharedFrozenState(true)
			defaults := productionSharedDefaults(true)
			if entry.mutateState != nil {
				entry.mutateState(state)
			}
			if entry.mutateDefaults != nil {
				entry.mutateDefaults(defaults)
			}
			stateBefore := clone(state)
			if _, _, err := configPrepare(path, nil, defaults, state); err == nil {
				test.Fatal("unknown, partial, or drifted ownership accepted")
			}
			if !bytes.Equal(modelTestRead(test, path), data) || !equal(state, stateBefore) {
				test.Fatal("preparation error mutated config or caller ownership")
			}
			sharedInstallerAssertMode(test, path, 0o640)
		})
	}
}

func TestProductionSharedFrozenFiveKeyOperationsSupportPendingAndReversal(test *testing.T) {
	for _, existed := range []bool{false, true} {
		test.Run(fmt.Sprintf("origin-%t", existed), func(test *testing.T) {
			path := filepath.Join(test.TempDir(), "config.toml")
			if existed {
				modelTestWrite(test, path, []byte(productionSharedPersonalDocument), 0o640)
			}
			operation := productionSharedLiteralOperation(path, existed)
			productionSharedAssertOperation(test, path, operation, productionSharedOriginal(existed), productionSharedFrozenExpected(), "codex-config")
			if pending, err := ConfigPending(path, operation); err != nil || pending == nil {
				test.Fatalf("historical operation not pending at its before snapshot: %#v, %v", pending, err)
			}
			if reversal, err := ConfigReversal(path, operation); err != nil || reversal != nil {
				test.Fatalf("historical operation reversed an unapplied snapshot: %#v, %v", reversal, err)
			}
			if _, mode, remove, err := ConfigRender(path, operation); err != nil || remove || mode.Perm() != 0o640 {
				test.Fatalf("historical render: mode=%o remove=%v err=%v", mode, remove, err)
			}
			configTestApply(test, path, operation)
			productionSharedAssertValues(test, path, Object{
				"features.memories": true, "memories.generate_memories": true, "memories.use_memories": true,
				"memories.extract_model": "legacy-extract", "memories.consolidation_model": "legacy-consolidation",
			})
			installerAppend(test, path, "\n# personal recovery addition\n[profiles.recovery_personal]\nmodel = 'recovery-personal'\n")
			if pending, err := ConfigPending(path, operation); err != nil || pending != nil {
				test.Fatalf("unowned edit invalidated an applied historical operation: %#v, %v", pending, err)
			}
			reversal, err := ConfigReversal(path, operation)
			if err != nil || reversal == nil {
				test.Fatalf("historical reversal failed: %#v, %v", reversal, err)
			}
			productionSharedAssertOperation(test, path, reversal, productionSharedFrozenExpected(), productionSharedOriginal(existed), "recover-codex-config")
			configTestApply(test, path, reversal)
			productionSharedAssertSnapshotOnDisk(test, path, productionSharedOriginal(existed))
			productionSharedAssertValues(test, path, Object{"profiles.recovery_personal.model": "recovery-personal"})
			if !strings.Contains(string(modelTestRead(test, path)), "# personal recovery addition\n") {
				test.Fatal("historical recovery discarded an independent comment")
			}
			sharedInstallerAssertMode(test, path, 0o640)
			if repeated, err := ConfigReversal(path, operation); err != nil || repeated != nil {
				test.Fatalf("repeated historical reversal was not a no-op: %#v, %v", repeated, err)
			}
			if pending, err := ConfigPending(path, operation); err != nil || pending == nil {
				test.Fatalf("historical operation could not retry with preserved personal additions: %#v, %v", pending, err)
			}
		})
	}
}

func TestProductionSharedAbsentOriginReversalRemovesOnlyEmptyManagedFile(test *testing.T) {
	path := filepath.Join(test.TempDir(), "config.toml")
	operation := productionSharedLiteralOperation(path, false)
	configTestApply(test, path, operation)
	reversal, err := ConfigReversal(path, operation)
	if err != nil || reversal == nil {
		test.Fatalf("absent-origin reversal failed: %#v, %v", reversal, err)
	}
	if _, _, remove, err := ConfigRender(path, reversal); err != nil || !remove {
		test.Fatalf("empty originally absent config was not removable: remove=%v err=%v", remove, err)
	}
	configTestApply(test, path, reversal)
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		test.Fatalf("empty originally absent file remains: %v", err)
	}
	if repeated, err := ConfigReversal(path, operation); err != nil || repeated != nil {
		test.Fatalf("already absent origin was not a no-op: %#v, %v", repeated, err)
	}
}

func TestProductionSharedCorruptJournalOperationsRejectBeforeMutation(test *testing.T) {
	cases := []struct {
		name   string
		mutate func(Object)
	}{
		{"missing-envelope-field", func(operation Object) { delete(operation, "before_mode") }},
		{"extra-envelope-field", func(operation Object) { operation["future"] = true }},
		{"wrong-kind", func(operation Object) { operation["kind"] = "other" }},
		{"wrong-label", func(operation Object) { operation["label"] = "other" }},
		{"nonboolean-existence", func(operation Object) { operation["before_exists"] = "true" }},
		{"noninteger-mode", func(operation Object) { operation["after_mode"] = "0640" }},
		{"partial-before", func(operation Object) { delete(object(operation["before_keys"]), "memories.use_memories") }},
		{"partial-both", func(operation Object) {
			delete(object(operation["before_keys"]), "memories.use_memories")
			delete(object(operation["after_keys"]), "memories.use_memories")
		}},
		{"mismatched-five-six", func(operation Object) {
			if _, present := object(operation["before_keys"])["model_context_window"]; present {
				delete(object(operation["after_keys"]), "model_context_window")
			} else {
				object(operation["after_keys"])["model_context_window"] = productionSharedRecord("512000")
			}
		}},
		{"unknown-on-both-sides", func(operation Object) {
			object(operation["before_keys"])["memories.future"] = productionSharedRecord("")
			object(operation["after_keys"])["memories.future"] = productionSharedRecord("true")
		}},
		{"unknown-in-place-of-known", func(operation Object) {
			for _, field := range []string{"before_keys", "after_keys"} {
				keys := object(operation[field])
				keys["memories.future"] = keys["memories.use_memories"]
				delete(keys, "memories.use_memories")
			}
		}},
		{"partial-root-pair", func(operation Object) {
			object(operation["before_keys"])["model"] = productionSharedRecord("'device-model'")
			object(operation["after_keys"])["model"] = productionSharedRecord(`"root-next"`)
		}},
		{"root-pair-only-on-before", func(operation Object) {
			object(operation["before_keys"])["model"] = productionSharedRecord("'device-model'")
			object(operation["before_keys"])["model_reasoning_effort"] = productionSharedRecord("'high'")
		}},
		{"injected-before-token", func(operation Object) {
			object(operation["before_keys"])["memories.extract_model"] = productionSharedRecord("'safe'\nmodel = 'injected'")
		}},
		{"injected-after-token", func(operation Object) {
			object(operation["after_keys"])["memories.extract_model"] = productionSharedRecord("'safe'\n[foreign]")
		}},
		{"absent-with-token", func(operation Object) {
			object(operation["before_keys"])["memories.generate_memories"] = Object{"present": false, "representation": "false"}
		}},
	}
	for _, withWindow := range []bool{false, true} {
		for _, entry := range cases {
			test.Run(fmt.Sprintf("window-%t/%s", withWindow, entry.name), func(test *testing.T) {
				path := filepath.Join(test.TempDir(), "config.toml")
				initial := []byte(productionSharedPersonalDocument)
				modelTestWrite(test, path, initial, 0o640)
				operation := productionSharedLiteralOperation(path, true)
				if withWindow {
					object(operation["before_keys"])["model_context_window"] = productionSharedRecord("")
					object(operation["after_keys"])["model_context_window"] = productionSharedRecord("512000")
				}
				if err := ConfigValidateOperation(operation); err != nil {
					test.Fatalf("literal journal seed rejected before corruption: %v", err)
				}
				entry.mutate(operation)
				if err := ConfigValidateOperation(operation); err == nil {
					test.Fatalf("corrupt journal operation validated: %#v", operation)
				}
				if _, _, _, err := ConfigRender(path, operation); err == nil {
					test.Fatal("render accepted corrupt journal operation")
				}
				if _, err := ConfigPending(path, operation); err == nil {
					test.Fatal("pending accepted corrupt journal operation")
				}
				if _, err := ConfigReversal(path, operation); err == nil {
					test.Fatal("reversal accepted corrupt journal operation")
				}
				if !bytes.Equal(modelTestRead(test, path), initial) {
					test.Fatal("corrupt journal operation mutated config")
				}
				sharedInstallerAssertMode(test, path, 0o640)
			})
		}
	}
}

func TestProductionSharedFrozenFiveKeyRemovalRestoresPersonalOriginals(test *testing.T) {
	for _, existed := range []bool{false, true} {
		test.Run(fmt.Sprintf("origin-%t", existed), func(test *testing.T) {
			path := filepath.Join(test.TempDir(), "config.toml")
			const personalWindow = "131_072"
			modelTestWrite(test, path, productionSharedManagedDocument(personalWindow), 0o640)
			state := productionSharedFrozenState(existed)
			removal, err := ConfigRemoval(path, state)
			if err != nil || removal == nil {
				test.Fatalf("literal five-key removal failed: %#v, %v", removal, err)
			}
			productionSharedAssertOperation(test, path, removal, productionSharedFrozenExpected(), productionSharedOriginal(existed), "codex-config")
			if pending, err := ConfigPending(path, removal); err != nil || pending == nil {
				test.Fatalf("five-key removal did not recognize its before snapshot: %#v, %v", pending, err)
			}
			configTestApply(test, path, removal)
			productionSharedAssertSnapshotOnDisk(test, path, productionSharedOriginal(existed))
			productionSharedAssertSnapshotOnDisk(test, path, Object{"model_context_window": productionSharedRecord(personalWindow)})
			productionSharedAssertValues(test, path, Object{"model": "device-model", "model_reasoning_effort": "high"})
			productionSharedAssertPersonal(test, path)
			sharedInstallerAssertMode(test, path, 0o640)
			if pending, err := ConfigPending(path, removal); err != nil || pending != nil {
				test.Fatalf("completed five-key removal remained pending: %#v, %v", pending, err)
			}
		})
	}
}

func TestProductionSharedRecoveryAndRemovalRefuseOwnedDrift(test *testing.T) {
	path := filepath.Join(test.TempDir(), "config.toml")
	data := []byte(strings.Replace(string(productionSharedManagedDocument("131_072")), "use_memories = true\n", "use_memories = false\n", 1))
	modelTestWrite(test, path, data, 0o640)
	operation := productionSharedLiteralOperation(path, true)
	if _, _, _, err := ConfigRender(path, operation); err == nil {
		test.Fatal("render overwrote owned drift")
	}
	if _, err := ConfigPending(path, operation); err == nil {
		test.Fatal("pending accepted a state matching neither owned snapshot")
	}
	if _, err := ConfigReversal(path, operation); err == nil {
		test.Fatal("reversal overwrote owned drift")
	}
	state := productionSharedFrozenState(true)
	if err := SharedConfigVerify(path, object(state["shared_config"])); err == nil {
		test.Fatal("owned drift was not detected")
	}
	if _, err := ConfigRemoval(path, state); err == nil {
		test.Fatal("removal overwrote owned drift")
	}
	if !bytes.Equal(modelTestRead(test, path), data) {
		test.Fatal("owned-drift rejection changed config bytes")
	}
	sharedInstallerAssertMode(test, path, 0o640)
}

func productionSharedInstallerState(test *testing.T, fixture *installerFixture) (Object, Object) {
	test.Helper()
	state, err := fixture.installer(false).state(true)
	if err != nil {
		test.Fatal(err)
	}
	return state, hostRecord(state, hostCodex)
}

func productionSharedAssertInstallerOwnership(test *testing.T, fixture *installerFixture, withWindow, originalExisted bool) (Object, Object) {
	test.Helper()
	state, host := productionSharedInstallerState(test, fixture)
	metadata := object(host["shared_config"])
	if err := SharedConfigValidateMetadata(metadata); err != nil {
		test.Fatalf("installer produced unreadable shared ownership: %v", err)
	}
	productionSharedAssertKeyMap(test, object(metadata["expected"]), productionSharedCurrentExpected(withWindow))
	if host["config_original_exists"] != originalExisted {
		test.Fatalf("installer changed earliest config origin: %#v", host)
	}
	productionSharedAssertValues(test, sharedInstallerConfigPath(fixture), productionSharedDefaults(withWindow))
	return state, host
}

func productionSharedAssertInstallerJournalUnion(test *testing.T, fixture *installerFixture, personalWindow string) {
	test.Helper()
	journal, err := readJSON(filepath.Join(fixture.paths.State, "journal.json"))
	if err != nil {
		test.Fatal(err)
	}
	if integer(journal["version"]) != 4 {
		test.Fatalf("unexpected native journal version: %#v", journal["version"])
	}
	path := Normalize(sharedInstallerConfigPath(fixture))
	count := 0
	for _, raw := range sequence(journal["operations"]) {
		operation := object(raw)
		if operation["path"] != path {
			continue
		}
		count++
		// Install enrolls the root model defaults as well, so the update's one
		// config operation carries them, unchanged, beside the shared keys.
		before := productionSharedCurrentExpected(false)
		before["model_context_window"] = productionSharedRecord(personalWindow)
		after := productionSharedCurrentExpected(true)
		for key, representation := range map[string]string{"model": `"gpt-6.1-sol"`, "model_reasoning_effort": `"max"`} {
			before[key], after[key] = productionSharedRecord(representation), productionSharedRecord(representation)
		}
		productionSharedAssertOperation(test, path, operation, before, after, "codex-config")
	}
	if count != 1 {
		test.Fatalf("five-to-six update had %d config operations instead of one complete six-key union", count)
	}
}

func TestProductionSharedInstallerFiveSixRollbackRecoveryAndUninstall(test *testing.T) {
	for _, originalExisted := range []bool{false, true} {
		for _, boundary := range []string{"after-journal", "after-codex-config", "after-state"} {
			test.Run(fmt.Sprintf("origin-%t/%s", originalExisted, boundary), func(test *testing.T) {
				fixture := sharedInstallerFixture(test)
				fixture.seedUserFiles(test)
				path := sharedInstallerConfigPath(fixture)
				if originalExisted {
					installerWrite(test, path, productionSharedPersonalDocument, 0o640)
				} else if err := os.Remove(path); err != nil {
					test.Fatal(err)
				}
				legacyRelease := sharedInstallerRelease(test, fixture, productionSharedFiveResource)
				fixture.first = legacyRelease
				sharedLifecycleExecute(test, fixture, "install")
				_, legacyHost := productionSharedAssertInstallerOwnership(test, fixture, false, originalExisted)
				if !reflect.DeepEqual(object(object(legacyHost["shared_config"])["original"]), productionSharedOriginal(originalExisted)) {
					test.Fatalf("five-key install did not snapshot the personal originals: %#v", legacyHost)
				}
				info, err := os.Stat(path)
				if err != nil {
					test.Fatal(err)
				}
				mode := info.Mode().Perm()
				const personalWindow = "131_072"
				productionSharedSetWindow(test, path, personalWindow)
				installerAppend(test, path, "\n# independent lifecycle comment\n[profiles.production_personal]\nmodel = 'do-not-touch'\n")
				currentRelease := sharedInstallerRelease(test, fixture, sharedInstallerResource("gpt-6.1-sol", "gpt-6.1-sol"))
				sharedLifecycleFailAt(test, fixture, "update", boundary)
				productionSharedAssertInstallerJournalUnion(test, fixture, personalWindow)
				installerAppend(test, path, "\n# personal edit after interrupted update\n")
				sharedLifecycleExecute(test, fixture, "recover")
				recoveredState, _ := productionSharedAssertInstallerOwnership(test, fixture, false, originalExisted)
				if stateRevision(recoveredState) != legacyRelease {
					test.Fatalf("recovery retained the interrupted release: %#v", recoveredState["release"])
				}
				productionSharedAssertSnapshotOnDisk(test, path, Object{"model_context_window": productionSharedRecord(personalWindow)})
				productionSharedAssertValues(test, path, Object{"profiles.production_personal.model": "do-not-touch"})
				if !strings.Contains(string(modelTestRead(test, path)), "# personal edit after interrupted update\n") {
					test.Fatal("recovery lost an unowned edit made after interruption")
				}
				sharedInstallerAssertMode(test, path, mode)
				sharedLifecycleExecute(test, fixture, "update")
				updatedState, updatedHost := productionSharedAssertInstallerOwnership(test, fixture, true, originalExisted)
				if stateRevision(updatedState) != currentRelease {
					test.Fatalf("update selected the wrong release: %#v", updatedState["release"])
				}
				original := productionSharedOriginal(originalExisted)
				original["model_context_window"] = productionSharedRecord(personalWindow)
				if !reflect.DeepEqual(object(object(updatedHost["shared_config"])["original"]), original) {
					test.Fatalf("successful update lost a personal original: %#v", updatedHost)
				}
				manager := clone(object(updatedState["manager"]))
				sharedLifecycleExecute(test, fixture, "rollback")
				rolledBack, _ := productionSharedAssertInstallerOwnership(test, fixture, false, originalExisted)
				if stateRevision(rolledBack) != legacyRelease || !equal(rolledBack["manager"], manager) {
					test.Fatal("skill rollback changed the current manager or selected the wrong release")
				}
				productionSharedAssertSnapshotOnDisk(test, path, Object{"model_context_window": productionSharedRecord(personalWindow)})
				const latestWindow = "+262_144"
				productionSharedSetWindow(test, path, latestWindow)
				sharedLifecycleExecute(test, fixture, "update")
				_, reacquiredHost := productionSharedAssertInstallerOwnership(test, fixture, true, originalExisted)
				original["model_context_window"] = productionSharedRecord(latestWindow)
				if !reflect.DeepEqual(object(object(reacquiredHost["shared_config"])["original"]), original) {
					test.Fatal("installer reacquisition reused the released context original")
				}
				sharedLifecycleExecute(test, fixture, "uninstall")
				productionSharedAssertSnapshotOnDisk(test, path, original)
				productionSharedAssertValues(test, path, Object{"model_context_window": int64(262144), "profiles.production_personal.model": "do-not-touch"})
				data := string(modelTestRead(test, path))
				for _, comment := range []string{"# independent lifecycle comment\n", "# personal edit after interrupted update\n"} {
					if !strings.Contains(data, comment) {
						test.Fatalf("uninstall removed an independent comment: %q", comment)
					}
				}
				if originalExisted {
					productionSharedAssertPersonal(test, path)
					productionSharedAssertValues(test, path, Object{"model": "device-model", "model_reasoning_effort": "high"})
				}
				sharedInstallerAssertMode(test, path, mode)
			})
		}
	}
}

func TestProductionSharedInstallerAbsentOriginWithoutPersonalEditsRemovesConfig(test *testing.T) {
	fixture := sharedInstallerFixture(test)
	fixture.seedUserFiles(test)
	path := sharedInstallerConfigPath(fixture)
	if err := os.Remove(path); err != nil {
		test.Fatal(err)
	}
	fixture.first = sharedInstallerRelease(test, fixture, productionSharedFiveResource)
	sharedLifecycleExecute(test, fixture, "install")
	productionSharedAssertInstallerOwnership(test, fixture, false, false)
	sharedInstallerRelease(test, fixture, sharedInstallerResource("gpt-6.1-sol", "gpt-6.1-sol"))
	sharedLifecycleExecute(test, fixture, "update")
	productionSharedAssertInstallerOwnership(test, fixture, true, false)
	sharedLifecycleExecute(test, fixture, "rollback")
	productionSharedAssertInstallerOwnership(test, fixture, false, false)
	sharedLifecycleExecute(test, fixture, "uninstall")
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		test.Fatalf("empty originally absent config remains after uninstall: %v", err)
	}
}

func TestProductionSharedInstallerRecoveryRefusesOwnedDriftWithoutMutation(test *testing.T) {
	fixture := sharedInstallerFixture(test)
	fixture.seedUserFiles(test)
	fixture.first = sharedInstallerRelease(test, fixture, productionSharedFiveResource)
	sharedLifecycleExecute(test, fixture, "install")
	sharedInstallerRelease(test, fixture, sharedInstallerResource("gpt-6.1-sol", "gpt-6.1-sol"))
	sharedLifecycleFailAt(test, fixture, "update", "after-codex-config")
	path := sharedInstallerConfigPath(fixture)
	info, err := os.Stat(path)
	if err != nil {
		test.Fatal(err)
	}
	original := modelTestRead(test, path)
	values := productionSharedRead(test, path)
	if current, present := sharedInstallerLookup(values, "memories.use_memories"); !present || current != true {
		test.Fatal("isolated fixture did not contain the managed use_memories assignment")
	}
	tokenOffset := -1
	var drifted []byte
	for candidateOffset := range original {
		if !bytes.HasPrefix(original[candidateOffset:], []byte("true")) {
			continue
		}
		candidate := []byte(string(original[:candidateOffset]) + "false" + string(original[candidateOffset+len("true"):]))
		var candidateValues Object
		if err := toml.Unmarshal(candidate, &candidateValues); err != nil {
			continue
		}
		if current, present := sharedInstallerLookup(candidateValues, "memories.use_memories"); !present || current != false {
			continue
		}
		if tokenOffset >= 0 {
			test.Fatal("multiple token spans changed the logical memories.use_memories boolean")
		}
		tokenOffset = candidateOffset
		drifted = candidate
	}
	if tokenOffset < 0 {
		test.Fatal("could not locate the logical memories.use_memories boolean token")
	}
	modelTestWrite(test, path, drifted, info.Mode().Perm())
	actual := modelTestRead(test, path)
	if !bytes.Equal(actual, drifted) || !bytes.Equal(actual[:tokenOffset], original[:tokenOffset]) || !bytes.Equal(actual[tokenOffset+len("false"):], original[tokenOffset+len("true"):]) {
		test.Fatal("target-only drift edit changed other bytes or owned token representations")
	}
	sharedInstallerAssertMode(test, path, info.Mode().Perm())
	productionSharedAssertValues(test, path, Object{"memories.use_memories": false})
	const personalComment = "\n# retain on refused recovery\n"
	installerAppend(test, path, personalComment)
	installer := sharedLifecycleInstaller(test, fixture, true)
	homeBefore := installerSnapshot(test, fixture.paths.Home)
	stateBefore := installerSnapshot(test, fixture.paths.State)
	if _, err := installer.Recover(); err == nil {
		test.Fatal("recovery overwrote an owned value matching neither journal side")
	}
	installerUnchanged(test, fixture.paths.Home, homeBefore)
	installerUnchanged(test, fixture.paths.State, stateBefore)
	afterRefusal := modelTestRead(test, path)
	if !bytes.Equal(afterRefusal, []byte(string(drifted)+personalComment)) {
		test.Fatal("refused recovery changed the controlled drift fixture")
	}
	restored := []byte(string(afterRefusal[:tokenOffset]) + "true" + string(afterRefusal[tokenOffset+len("false"):]))
	modelTestWrite(test, path, restored, info.Mode().Perm())
	if !bytes.Equal(modelTestRead(test, path), []byte(string(original)+personalComment)) {
		test.Fatal("restoring the target token changed other bytes or owned representations")
	}
	productionSharedAssertValues(test, path, Object{"memories.use_memories": true})
	if _, err := installer.Recover(); err != nil {
		test.Fatalf("recovery failed after restoring only the target boolean: %v", err)
	}
	if !strings.Contains(string(modelTestRead(test, path)), personalComment) {
		test.Fatal("successful recovery control removed the independent comment")
	}
	sharedInstallerAssertMode(test, path, info.Mode().Perm())
}
