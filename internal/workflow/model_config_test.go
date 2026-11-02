package workflow

import (
	"bytes"
	"encoding/json/v2"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var modelTestDefaults = map[string]string{"model": "gpt-6.1-sol", "model_reasoning_effort": "max"}

func modelTestWrite(t *testing.T, path string, data []byte, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, data, mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}

func modelTestApply(t *testing.T, path string, operation Object) {
	t.Helper()
	if operation == nil {
		return
	}
	data, mode, remove, err := ModelRender(path, operation)
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

func modelTestRead(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestModelOriginalRepresentationCommentsModeAndTables(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "config.toml")
	original := []byte("# User header\nmodel = 'original' # keep inline\nmodel_reasoning_effort=\"high\"\n\n[profiles.personal]\nmodel = 'profile'\n")
	modelTestWrite(t, path, original, 0o640)
	metadata, operation, err := ModelPrepare(path, modelTestDefaults, nil)
	if err != nil {
		t.Fatal(err)
	}
	modelTestApply(t, path, operation)
	installed := modelTestRead(t, path)
	for _, preserved := range []string{"# keep inline", "model_reasoning_effort=\"max\"", "model = 'profile'"} {
		if !bytes.Contains(installed, []byte(preserved)) {
			t.Fatalf("missing %q in %s", preserved, installed)
		}
	}
	modelTestWrite(t, path, append(installed, []byte("other = 'later'\n")...), 0o604)
	removal, err := ModelRemoval(path, metadata)
	if err != nil {
		t.Fatal(err)
	}
	modelTestApply(t, path, removal)
	want := append(bytes.Clone(original), []byte("other = 'later'\n")...)
	if got := modelTestRead(t, path); !bytes.Equal(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o604 {
		t.Fatalf("mode = %o", info.Mode().Perm())
	}
}

func TestModelMissingKeysPreserveEveryUnrelatedByte(t *testing.T) {
	t.Parallel()
	for _, original := range []string{"# heading\n[profile]\nmodel = 'profile'\n", "other='value'", "# heading without final newline", "\n  \n\t\n", "# CRLF\r\n[profile]\r\nvalue = 'other'\r\n", "other = [\n 'model', # untouched\n 'next',\n]\n"} {
		t.Run(original, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.toml")
			modelTestWrite(t, path, []byte(original), 0o640)
			metadata, operation, err := ModelPrepare(path, modelTestDefaults, nil)
			if err != nil {
				t.Fatal(err)
			}
			modelTestApply(t, path, operation)
			removal, err := ModelRemoval(path, metadata)
			if err != nil {
				t.Fatal(err)
			}
			modelTestApply(t, path, removal)
			if got := string(modelTestRead(t, path)); got != original {
				t.Fatalf("got %q, want %q", got, original)
			}
		})
	}
}

func TestModelRemovalRetainsAttachedComment(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "config.toml")
	metadata, operation, err := ModelPrepare(path, modelTestDefaults, nil)
	if err != nil {
		t.Fatal(err)
	}
	modelTestApply(t, path, operation)
	data := bytes.Replace(modelTestRead(t, path), []byte(`model = "gpt-6.1-sol"`), []byte(`  model = "gpt-6.1-sol"  ###   User heading`), 1)
	modelTestWrite(t, path, data, 0o600)
	removal, err := ModelRemoval(path, metadata)
	if err != nil {
		t.Fatal(err)
	}
	modelTestApply(t, path, removal)
	if got := string(modelTestRead(t, path)); got != "  ###   User heading\n" {
		t.Fatalf("got %q", got)
	}
}

func TestModelAbsentFileRemovalAndWhitespace(t *testing.T) {
	t.Parallel()
	for _, later := range []string{"", "# later user note\n", "\n  \n\t\n"} {
		t.Run(later, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.toml")
			metadata, operation, err := ModelPrepare(path, modelTestDefaults, nil)
			if err != nil {
				t.Fatal(err)
			}
			modelTestApply(t, path, operation)
			if later != "" {
				modelTestWrite(t, path, append(modelTestRead(t, path), []byte(later)...), 0o600)
			}
			removal, err := ModelRemoval(path, metadata)
			if err != nil {
				t.Fatal(err)
			}
			modelTestApply(t, path, removal)
			if later == "" {
				if _, err := os.Stat(path); !os.IsNotExist(err) {
					t.Fatalf("file was not removed: %v", err)
				}
			} else if got := string(modelTestRead(t, path)); got != later {
				t.Fatalf("got %q, want %q", got, later)
			}
		})
	}
}

func TestModelConcurrentEditBetweenPreparationAndRender(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "config.toml")
	modelTestWrite(t, path, []byte("other = 'before'\n"), 0o600)
	_, operation, err := ModelPrepare(path, modelTestDefaults, nil)
	if err != nil {
		t.Fatal(err)
	}
	modelTestWrite(t, path, []byte("other = 'changed'\n# added\n"), 0o600)
	modelTestApply(t, path, operation)
	if !bytes.Contains(modelTestRead(t, path), []byte("other = 'changed'\n# added\n")) {
		t.Fatal("concurrent user edit lost")
	}
}

func TestModelRecoveryAndRetryPreserveIndependentEdits(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "config.toml")
	modelTestWrite(t, path, []byte("model = 'original'\n"), 0o640)
	_, operation, err := ModelPrepare(path, modelTestDefaults, nil)
	if err != nil {
		t.Fatal(err)
	}
	if reverse, err := ModelReversal(path, operation); err != nil || reverse != nil {
		t.Fatalf("before apply: %v, %v", reverse, err)
	}
	modelTestApply(t, path, operation)
	modelTestWrite(t, path, append(modelTestRead(t, path), []byte("first = 1\n")...), 0o604)
	reverse, err := ModelReversal(path, operation)
	if err != nil {
		t.Fatal(err)
	}
	modelTestWrite(t, path, append(modelTestRead(t, path), []byte("second = 2\n")...), 0o604)
	pending, err := ModelPending(path, reverse)
	if err != nil {
		t.Fatal(err)
	}
	modelTestApply(t, path, pending)
	modelTestWrite(t, path, append(modelTestRead(t, path), []byte("third = 3\n")...), 0o604)
	if pending, err := ModelPending(path, reverse); err != nil || pending != nil {
		t.Fatalf("retry: %v, %v", pending, err)
	}
	if got := string(modelTestRead(t, path)); got != "model = 'original'\nfirst = 1\nsecond = 2\nthird = 3\n" {
		t.Fatalf("got %q", got)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o604 {
		t.Fatalf("later mode lost: %o", info.Mode().Perm())
	}
}

func TestModelRemovalRecoveryPreservesLaterContent(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "config.toml")
	metadata, install, err := ModelPrepare(path, modelTestDefaults, nil)
	if err != nil {
		t.Fatal(err)
	}
	modelTestApply(t, path, install)
	removal, err := ModelRemoval(path, metadata)
	if err != nil {
		t.Fatal(err)
	}
	modelTestApply(t, path, removal)
	modelTestWrite(t, path, []byte("other = 'later'\n"), 0o600)
	reverse, err := ModelReversal(path, removal)
	if err != nil {
		t.Fatal(err)
	}
	modelTestApply(t, path, reverse)
	if err := ModelVerify(path, metadata); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(modelTestRead(t, path), []byte("other = 'later'")) {
		t.Fatal("later bytes lost")
	}
}

func TestModelRejectsMalformedUnsafeAndChangedOwnedInput(t *testing.T) {
	t.Parallel()
	for _, content := range []string{"x =", "model = 'a'\nmodel = 'b'\n", "\xff", "model = 42\n", "model_reasoning_effort = true\n", "[model]\nx=1\n"} {
		t.Run(content, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.toml")
			modelTestWrite(t, path, []byte(content), 0o600)
			if _, _, err := ModelPrepare(path, modelTestDefaults, nil); err == nil {
				t.Fatal("unsafe TOML accepted")
			}
			if got := string(modelTestRead(t, path)); got != content {
				t.Fatal("input mutated")
			}
		})
	}
	t.Run("symlink", func(t *testing.T) {
		base := t.TempDir()
		target := filepath.Join(base, "other.toml")
		modelTestWrite(t, target, []byte("# other\n"), 0o600)
		path := filepath.Join(base, "config.toml")
		if err := os.Symlink(target, path); err != nil {
			t.Fatal(err)
		}
		if _, _, err := ModelPrepare(path, modelTestDefaults, nil); err == nil || !strings.Contains(err.Error(), "symlinked") {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("changed owned value", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "config.toml")
		metadata, operation, err := ModelPrepare(path, modelTestDefaults, nil)
		if err != nil {
			t.Fatal(err)
		}
		modelTestApply(t, path, operation)
		data := bytes.ReplaceAll(modelTestRead(t, path), []byte(`"max"`), []byte(`"high"`))
		modelTestWrite(t, path, data, 0o600)
		if _, err := ModelRemoval(path, metadata); err == nil {
			t.Fatal("changed ownership accepted")
		}
		if _, err := ModelReversal(path, operation); err == nil {
			t.Fatal("changed recovery ownership accepted")
		}
		if !bytes.Equal(modelTestRead(t, path), data) {
			t.Fatal("changed input mutated")
		}
	})
}

func TestModelMetadataOperationJSONCompatibilityAndCorruption(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "config.toml")
	metadata, operation, err := ModelPrepare(path, modelTestDefaults, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []Object{metadata, operation} {
		data, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		var decoded Object
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatal(err)
		}
		if value == nil {
			t.Fatal("missing fixture")
		}
		if decoded["kind"] == "model_config" {
			if err := ModelValidateOperation(decoded); err != nil {
				t.Fatal(err)
			}
		} else if err := ModelValidateMetadata(decoded); err != nil {
			t.Fatal(err)
		}
	}
	for _, value := range []Object{nil, {}, maps.Clone(metadata)} {
		if len(value) > 0 {
			value["original_exists"] = "false"
		}
		if err := ModelValidateMetadata(value); err == nil {
			t.Fatal("corrupt metadata accepted")
		}
	}
	operation["after_keys"].(Object)["other"] = Object{"present": false}
	if _, _, _, err := ModelRender(path, operation); err == nil {
		t.Fatal("nonowned key accepted")
	}
	metadata["original"].(Object)["model"] = Object{"present": true, "representation": "'x'\nother='unsafe'"}
	if err := ModelValidateMetadata(metadata); err == nil {
		t.Fatal("injected representation accepted")
	}
}

func TestModelQuotedAndMultilineRepresentationsRoundTrip(t *testing.T) {
	t.Parallel()
	for _, content := range []string{"\"model\" = '''\nold # literal\nmodel = misleading\n''' # keep\n'model_reasoning_effort'=\"hi\\u0067h\"\n", "model = \"\"\"old \\\n text\"\"\"\nmodel_reasoning_effort = 'high'"} {
		t.Run(content, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.toml")
			modelTestWrite(t, path, []byte(content), 0o600)
			metadata, operation, err := ModelPrepare(path, modelTestDefaults, nil)
			if err != nil {
				t.Fatal(err)
			}
			modelTestApply(t, path, operation)
			removal, err := ModelRemoval(path, metadata)
			if err != nil {
				t.Fatal(err)
			}
			modelTestApply(t, path, removal)
			if got := string(modelTestRead(t, path)); got != content {
				t.Fatalf("got %q, want %q", got, content)
			}
		})
	}
}
