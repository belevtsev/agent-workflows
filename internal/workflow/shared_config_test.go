package workflow

import (
	"bytes"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func sharedTestDefaults() Object {
	return Object{
		"features.memories": true, "memories.generate_memories": true, "memories.use_memories": true,
		"memories.extract_model": "gpt-6.1-sol", "memories.consolidation_model": "gpt-6.1-sol",
		"model_context_window": int64(512000),
	}
}

func configTestApply(t *testing.T, path string, operation Object) {
	t.Helper()
	if operation == nil {
		return
	}
	data, mode, remove, err := ConfigRender(path, operation)
	if err != nil {
		t.Fatal(err)
	}
	if remove {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
		return
	}
	modelTestWrite(t, path, data, mode)
}

func TestSharedConfigPreservesDeviceBytesAndRestoresTokens(t *testing.T) {
	for name, original := range map[string]string{
		"tables and quoted keys":  "# device header\nmodel = 'personal' # model note\nmodel_reasoning_effort = '''high'''\n\n[features]\n\"memories\" = false # feature note\nother = false\n\n[\"memories\"]\nextract_model = '''personal\nmodel''' # extraction note\nconsolidation_model = ''\nuse_memories = false\n\n[projects.\"/local/device\"]\ntrust_level = 'trusted'\n[profiles.personal]\nmodel = 'profile'\n",
		"dotted and literal keys": "\"features.memories\" = 'literal'\n\"memories.extract_model\" = 'literal'\nfeatures . memories = false\nmemories . \"extract_model\" = 'old'\n[desktop]\npath = '/local/device'\n",
		"CRLF":                    "# device\r\n[features]\r\nother = true\r\n[memories]\r\nuse_memories = false # keep\r\n[projects.device]\r\ntrust_level = 'trusted'\r\n",
		"root without newline":    "device_path = '/local/device'",
		"comment without newline": "# a user's comment",
		"whitespace":              "\n  \n\t\n",
		"only nested lookalikes":  "[profiles.private.features]\nmemories = false\n[profiles.private.memories]\nextract_model = 'nested'\n",
		"implicit parent tables":  "[memories.local]\npath = '/device'\n[features.local]\nvalue = true\n",
		"device context window":   "model_context_window = 128000 # device window\n[features]\nother = true\n",
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.toml")
			modelTestWrite(t, path, []byte(original), 0o640)
			state, operation, err := configPrepare(path, modelTestDefaults, sharedTestDefaults(), Object{})
			if err != nil {
				t.Fatal(err)
			}
			configTestApply(t, path, operation)
			if err := SharedConfigVerify(path, state["shared_config"].(Object)); err != nil {
				t.Fatal(err)
			}
			if err := ModelVerify(path, state["model_config"].(Object)); err != nil {
				t.Fatal(err)
			}
			installed := modelTestRead(t, path)
			later := []byte("\n# later device comment\n")
			modelTestWrite(t, path, append(installed, later...), 0o604)
			unchanged, change, err := configPrepare(path, modelTestDefaults, sharedTestDefaults(), state)
			if err != nil || change != nil || !reflect.DeepEqual(unchanged, state) {
				t.Fatalf("repeat changed managed configuration: %v, %v", change, err)
			}
			removal, err := ConfigRemoval(path, state)
			if err != nil {
				t.Fatal(err)
			}
			configTestApply(t, path, removal)
			want := append([]byte(original), later...)
			if got := modelTestRead(t, path); !bytes.Equal(got, want) {
				t.Fatalf("device bytes changed: got %q, want %q", got, want)
			}
			info, err := os.Stat(path)
			if err != nil || info.Mode().Perm() != 0o604 {
				t.Fatalf("device permissions changed: %v, %v", info, err)
			}
		})
	}
}

func TestSharedConfigLegacyAbsentOriginSurvivesAcquisitionAndRelease(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	legacy, op, err := ModelPrepare(path, modelTestDefaults, nil)
	if err != nil {
		t.Fatal(err)
	}
	modelTestApply(t, path, op)
	state, op, err := configPrepare(path, modelTestDefaults, sharedTestDefaults(), Object{"model_config": legacy})
	if err != nil {
		t.Fatal(err)
	}
	if state["config_original_exists"] != false || !reflect.DeepEqual(state["model_config"], legacy) || state["shared_config"].(Object)["original_exists"] != true {
		t.Fatalf("legacy origin or frozen metadata changed: %v", state)
	}
	configTestApply(t, path, op)
	released, op, err := configPrepare(path, modelTestDefaults, nil, state)
	if err != nil {
		t.Fatal(err)
	}
	configTestApply(t, path, op)
	if released["shared_config"] != nil || released["config_original_exists"] != false {
		t.Fatalf("shared ownership retained or earliest origin lost: %v", released)
	}
	removal, err := ConfigRemoval(path, released)
	if err != nil {
		t.Fatal(err)
	}
	configTestApply(t, path, removal)
	if exists(path) {
		t.Fatalf("originally absent config retained after uninstall: %q", modelTestRead(t, path))
	}
}

func TestSharedConfigRefusesUnsafeLocalShapesBeforeChanges(t *testing.T) {
	for _, original := range []string{
		"features = { memories = false, local = true }\n",
		"memories = { extract_model = 'old' }\n",
		"features = []\n", "memories = 'local'\n", "[memories]\nextract_model = false\n",
		"[features]\nmemories = 'true'\n", "[memories.extract_model]\nvalue = 'model'\n",
		"model = true\n", "memories.use_memories = 1\n", "[memories]",
		"memories.extract_model = 'unterminated\n",
	} {
		t.Run(original, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.toml")
			modelTestWrite(t, path, []byte(original), 0o640)
			_, _, err := configPrepare(path, modelTestDefaults, sharedTestDefaults(), Object{})
			if err == nil {
				t.Fatal("unsafe local config accepted")
			}
			if got := string(modelTestRead(t, path)); got != original {
				t.Fatalf("failed preflight changed config: %q", got)
			}
		})
	}
}

func TestSharedConfigOperationRefusesUnownedKeysAndInjectedValues(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	_, operation, err := configPrepare(path, modelTestDefaults, sharedTestDefaults(), Object{})
	if err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(Object){
		func(op Object) {
			op["after_keys"].(Object)["memories.extract_model"].(Object)["representation"] = "\"model\"\nother = 'injected'"
		},
		func(op Object) {
			op["after_keys"].(Object)["memories.extract_model"].(Object)["representation"] = "\"model\" # injected comment"
		},
		func(op Object) {
			op["after_keys"].(Object)["memories.extract_model"].(Object)["representation"] = "true"
		},
		func(op Object) { op["after_keys"].(Object)["features.memories"].(Object)["representation"] = "'true'" },
		func(op Object) { delete(op["before_keys"].(Object), "model") },
		func(op Object) { op["before_keys"].(Object)["project_path"] = Object{"present": false} },
		func(op Object) { op["after_exists"] = false },
	} {
		invalid := clone(operation)
		mutate(invalid)
		if err := ConfigValidateOperation(invalid); err == nil {
			t.Fatalf("unsafe operation accepted: %v", invalid)
		}
	}
}

func TestSharedConfigRecoveryPreservesUnownedChangesAndRejectsDrift(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	original := "# local\n[memories]\nextract_model = 'personal' # note\n[desktop]\npath = '/device'\n"
	modelTestWrite(t, path, []byte(original), 0o640)
	_, op, err := configPrepare(path, modelTestDefaults, sharedTestDefaults(), Object{})
	if err != nil {
		t.Fatal(err)
	}
	configTestApply(t, path, op)
	installed := modelTestRead(t, path)
	later := []byte("# independent edit\n")
	modelTestWrite(t, path, append(installed, later...), 0o604)
	reversal, err := ConfigReversal(path, op)
	if err != nil {
		t.Fatal(err)
	}
	configTestApply(t, path, reversal)
	if got := modelTestRead(t, path); !bytes.Equal(got, append([]byte(original), later...)) {
		t.Fatalf("recovery overwrote independent edit: %q", got)
	}
	if pending, err := ConfigPending(path, reversal); err != nil || pending != nil {
		t.Fatalf("completed recovery was repeated: %v, %v", pending, err)
	}
	configTestApply(t, path, op)
	drifted := bytes.Replace(modelTestRead(t, path), []byte(`extract_model = "gpt-6.1-sol"`), []byte(`extract_model = "user-edit"`), 1)
	modelTestWrite(t, path, drifted, 0o604)
	if _, err := ConfigReversal(path, op); err == nil {
		t.Fatal("owned drift was overwritten by recovery")
	}
}

func TestSharedConfigResourceAllowlistAndHistoricalAbsence(t *testing.T) {
	root := t.TempDir()
	if defaults, err := SharedConfigDefaults(root); err != nil || defaults != nil {
		t.Fatalf("historical source was changed: %v, %v", defaults, err)
	}
	if err := os.Mkdir(filepath.Join(root, "global"), 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, filepath.FromSlash(sharedConfigResource))
	valid := "version=1\nmodel_context_window=512000\n[features]\nmemories=true\n[memories]\ngenerate_memories=true\nuse_memories=true\nextract_model='gpt-6.1-sol'\nconsolidation_model='gpt-6.1-sol'\n"
	modelTestWrite(t, path, []byte(valid), 0o644)
	if got, err := SharedConfigDefaults(root); err != nil || !reflect.DeepEqual(got, sharedTestDefaults()) {
		t.Fatalf("valid defaults rejected: %v, %v", got, err)
	}
	for _, invalid := range []string{
		strings.Replace(valid, "version=1", "version=2", 1),
		strings.Replace(valid, "version=1", "version=1.0", 1),
		strings.Replace(valid, "memories=true", "memories='true'", 1),
		strings.Replace(valid, "extract_model='gpt-6.1-sol'", "extract_model=''", 1),
		strings.Replace(valid, "version=1", "version=1\nmodel='duplicated'", 1),
		valid + "device_path='/private/device'\n",
		strings.Replace(valid, "use_memories=true\n", "", 1),
		strings.Replace(valid, "model_context_window=512000\n", "", 1),
		strings.Replace(valid, "model_context_window=512000", "model_context_window=-1", 1),
		strings.Replace(valid, "model_context_window=512000", "model_context_window=512000.0", 1),
		strings.Replace(valid, "model_context_window=512000", "model_context_window='512000'", 1),
	} {
		modelTestWrite(t, path, []byte(invalid), 0o644)
		if _, err := SharedConfigDefaults(root); err == nil {
			t.Fatalf("unportable defaults accepted: %q", invalid)
		}
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("elsewhere", path); err != nil {
		t.Fatal(err)
	}
	if _, err := SharedConfigDefaults(root); err == nil {
		t.Fatal("symlinked resource accepted")
	}
}

func TestSharedConfigOnlyDoesNotAcquireMainModel(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	original := "model = 1\nmodel_reasoning_effort = false\n[profiles.local]\nmodel = 'personal'\n"
	modelTestWrite(t, path, []byte(original), 0o640)
	state, op, err := configPrepare(path, nil, sharedTestDefaults(), Object{})
	if err != nil {
		t.Fatal(err)
	}
	configTestApply(t, path, op)
	if state["model_config"] != nil {
		t.Fatal("shared defaults acquired unrequested model ownership")
	}
	defaults := maps.Clone(sharedTestDefaults())
	defaults["memories.extract_model"] = "gpt-6-sol"
	updated, change, err := configPrepare(path, nil, defaults, state)
	if err != nil {
		t.Fatal(err)
	}
	configTestApply(t, path, change)
	removal, err := ConfigRemoval(path, updated)
	if err != nil {
		t.Fatal(err)
	}
	configTestApply(t, path, removal)
	if got := string(modelTestRead(t, path)); got != original {
		t.Fatalf("shared-only delivery changed main model fields: %q", got)
	}
}
