package advisers

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func isolatePersonalConfig(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("APPDATA", dir)
	t.Setenv("HOME", dir)
	configDir, err := os.UserConfigDir()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(configDir, "kitsu", "advisers.toml")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

func writeAdviserTestFile(t *testing.T, path, data string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestLoadHasNoAdvisersOrAccessByDefault(t *testing.T) {
	personal := isolatePersonalConfig(t)
	t.Setenv("TYPESAFE_API_KEY", "present-but-not-enabled")
	t.Setenv("SUBQ_API_KEY", "present-but-not-enabled")
	if len(Providers()) == 0 {
		t.Fatal("expected an available provider catalog")
	}
	root := t.TempDir()
	got, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Advisers) != 0 || len(got.Access) != 0 || len(got.Checkpoints) != 0 {
		t.Fatalf("default configuration = %#v, want no advisers or policy", got)
	}
	if Allowed(got.Access, "coordinator", "anything") {
		t.Fatal("an absent policy granted access")
	}
	for _, path := range []string{personal, filepath.Join(root, FileName)} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("default discovery created configuration at %s: %v", path, err)
		}
	}
}

func TestLoadReplacesWholeDefinitionsAndValidatesMergedReferences(t *testing.T) {
	personal := isolatePersonalConfig(t)
	writeAdviserTestFile(t, personal, `
version = 1
[advisers.local]
model = "personal"
command = ["personal-cli", "{model}", "--old-flag"]
timeout = "11s"
[advisers.private]
model = "private-model"
command = ["private-cli", "{model}"]
`)
	root := t.TempDir()
	repository := filepath.Join(root, FileName)
	writeAdviserTestFile(t, repository, `
version = 1
[advisers.local]
model = "repository"
command = ["repository-cli", "{model}"]
[access]
default = ["local", "private"]
developer = []
[[checkpoints]]
id = "design-advice"
stage = "design"
role = "architect"
advisers = ["local", "private"]
`)
	got, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	local := got.Advisers["local"]
	if local.Model != "repository" || local.Timeout != DefaultTimeout || local.Source != repository {
		t.Fatalf("repository definition = %#v, want a complete replacement with the default timeout", local)
	}
	if !reflect.DeepEqual(local.Command, []string{"repository-cli", "{model}"}) {
		t.Fatalf("repository command = %q, inherited personal arguments", local.Command)
	}
	if got.Advisers["private"].Source != personal {
		t.Fatalf("personal source = %q, want %q", got.Advisers["private"].Source, personal)
	}
	if !Allowed(got.Access, "architect", "private") || Allowed(got.Access, "developer", "local") {
		t.Fatal("role fallback or explicit empty deny list was lost")
	}
	if len(got.Checkpoints) != 1 || len(got.Checkpoints[0].Advisers) != 2 {
		t.Fatalf("checkpoints = %#v", got.Checkpoints)
	}
}

func TestParseStrictConfiguration(t *testing.T) {
	valid := "version = 1\n[advisers.local]\nmodel = \"model\"\ncommand = [\"cli\", \"{model}\"]\n"
	tests := []struct {
		name     string
		text     string
		personal bool
	}{
		{name: "missing version", text: strings.TrimPrefix(valid, "version = 1\n")},
		{name: "future version", text: strings.Replace(valid, "version = 1", "version = 3", 1)},
		{name: "wrong version type", text: "version = \"1\"\n"},
		{name: "unknown root field", text: "version = 1\nkey = true\n"},
		{name: "unknown adviser field", text: valid + "key = true\n"},
		{name: "duplicate TOML field", text: valid + "model = \"other\"\n"},
		{name: "invalid name", text: strings.Replace(valid, "advisers.local", "advisers.Local", 1)},
		{name: "empty model", text: strings.Replace(valid, "\"model\"", "\" \"", 1)},
		{name: "no model placeholder", text: strings.Replace(valid, "\"{model}\"", "\"model\"", 1)},
		{name: "repeated model placeholder", text: strings.Replace(valid, "\"{model}\"", "\"{model}\", \"{model}\"", 1)},
		{name: "embedded model placeholder", text: strings.Replace(valid, "\"{model}\"", "\"--model={model}\"", 1)},
		{name: "model placeholder is program", text: strings.Replace(valid, "\"cli\", \"{model}\"", "\"{model}\", \"cli\"", 1)},
		{name: "repeated answer placeholder", text: strings.Replace(valid, "\"{model}\"", "\"{model}\", \"{answer_file}\", \"{answer_file}\"", 1)},
		{name: "embedded answer placeholder", text: strings.Replace(valid, "\"{model}\"", "\"{model}\", \"--out={answer_file}\"", 1)},
		{name: "empty timeout", text: valid + "timeout = \"\"\n"},
		{name: "invalid timeout", text: valid + "timeout = \"soon\"\n"},
		{name: "timeout too short", text: valid + "timeout = \"999ms\"\n"},
		{name: "timeout too long", text: valid + "timeout = \"1h1s\"\n"},
		{name: "personal access", text: "version = 1\n[access]\n", personal: true},
		{name: "personal empty access", text: "version = 1\naccess = {}\n", personal: true},
		{name: "personal empty checkpoints", text: "version = 1\ncheckpoints = []\n", personal: true},
		{name: "unknown role", text: valid + "[access]\nadmin = [\"local\"]\n"},
		{name: "duplicate access adviser", text: valid + "[access]\ndefault = [\"local\", \"local\"]\n"},
		{name: "invalid UTF-8", text: valid + "# \xff"},
		{name: "oversize", text: strings.Repeat(" ", MaxInput+1)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := Parse([]byte(test.text), test.personal); err == nil {
				t.Fatal("invalid configuration was accepted")
			}
		})
	}
	for _, timeout := range []string{"1s", "1h"} {
		t.Run("boundary timeout "+timeout, func(t *testing.T) {
			if _, err := Parse([]byte(valid+"timeout = \""+timeout+"\"\n"), false); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestValidateConfigReferencesAndCheckpoints(t *testing.T) {
	definitions := `version = 1
[advisers.local]
model = "model"
command = ["cli", "{model}"]
`
	checkpoint := `[[checkpoints]]
id = "checkpoint"
stage = "design"
role = "architect"
advisers = ["local"]
`
	tests := []struct {
		name   string
		policy string
	}{
		{name: "undefined access", policy: "[access]\ndefault = [\"missing\"]\n"},
		{name: "undefined checkpoint", policy: strings.Replace(checkpoint, `advisers = ["local"]`, `advisers = ["missing"]`, 1)},
		{name: "denied checkpoint", policy: checkpoint},
		{name: "explicitly denied checkpoint", policy: "[access]\ndefault = [\"local\"]\narchitect = []\n" + checkpoint},
		{name: "unknown checkpoint role", policy: strings.Replace(checkpoint, "architect", "admin", 1)},
		{name: "unknown checkpoint stage", policy: strings.Replace(checkpoint, `stage = "design"`, `stage = "implementation"`, 1)},
		{name: "empty checkpoint advisers", policy: strings.Replace(checkpoint, `advisers = ["local"]`, "advisers = []", 1)},
		{name: "duplicate checkpoint id", policy: checkpoint + checkpoint},
		{name: "unknown checkpoint field", policy: checkpoint + "required = true\n"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cfg, err := Parse([]byte(definitions+test.policy), false)
			if err == nil {
				err = ValidateConfig(cfg)
			}
			if err == nil {
				t.Fatal("invalid merged configuration was accepted")
			}
		})
	}
	cfg, err := Parse([]byte("version = 1\n[access]\ndefault = [\"personal-only\"]\n"), false)
	if err != nil {
		t.Fatalf("reference was checked before merging: %v", err)
	}
	if err := ValidateConfig(cfg); err == nil {
		t.Fatal("undefined merged reference was accepted")
	}
}

func TestLoadReportsInvalidRepositoryAndPersonalConfiguration(t *testing.T) {
	personal := isolatePersonalConfig(t)
	root := t.TempDir()
	writeAdviserTestFile(t, personal, "version = 3\n")
	if _, err := Load(root); err == nil || !strings.Contains(err.Error(), personal) {
		t.Fatalf("personal load error = %v, want its path", err)
	}
	writeAdviserTestFile(t, personal, "version = 1\n")
	writeAdviserTestFile(t, filepath.Join(root, FileName), "version = 1\n[access]\ndefault = [\"undefined\"]\n")
	if _, err := Load(root); err == nil || !strings.Contains(err.Error(), "undefined") {
		t.Fatalf("repository load error = %v, want an undefined reference", err)
	}
}

func TestAllowedAndDefinitionFingerprint(t *testing.T) {
	access := map[string][]string{"default": {"local"}, "developer": {}}
	if !Allowed(access, "coordinator", "local") || !Allowed(access, "reviewer", "local") {
		t.Fatal("default policy was not applied to known roles")
	}
	if Allowed(access, "developer", "local") || Allowed(access, "unknown", "local") {
		t.Fatal("explicit deny or unknown role granted access")
	}
	definition := Definition{Model: "model", Command: []string{"cli", "{model}"}, Timeout: time.Minute}
	first := Fingerprint(definition)
	definition.Source = "/another/machine/personal.toml"
	if Fingerprint(definition) != first || len(first) != 64 {
		t.Fatal("fingerprint depends on a personal path or is not SHA-256")
	}
	for _, changed := range []Definition{
		{Model: "other", Command: definition.Command, Timeout: definition.Timeout},
		{Model: definition.Model, Command: []string{"other-cli", "{model}"}, Timeout: definition.Timeout},
		{Model: definition.Model, Command: definition.Command, Timeout: time.Second},
	} {
		if Fingerprint(changed) == first {
			t.Fatal("model, command or timeout drift did not change its fingerprint")
		}
	}
}
