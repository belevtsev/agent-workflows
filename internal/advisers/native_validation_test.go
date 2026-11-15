package advisers

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNativeConfigurationAndLegacyCompatibility(t *testing.T) {
	personal := isolatePersonalConfig(t)
	writeAdviserTestFile(t, personal, "version = 2\n[advisers.jev]\nprovider = 'typesafe'\nmodel = 'personal-model'\n")
	root := t.TempDir()
	writeAdviserTestFile(t, filepath.Join(root, FileName), "version = 2\n[advisers.jev]\nprovider = 'typesafe'\n[advisers.snap]\nprovider = 'subq'\n[advisers.cli]\nmodel = 'legacy'\ncommand = ['tool', '{model}']\n[access]\narchitect = ['jev', 'snap', 'cli']\n")
	cfg, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Advisers["jev"].Model != "jev-1.14.0" || cfg.Advisers["jev"].APIKeyEnv != "TYPESAFE_API_KEY" || cfg.Advisers["snap"].Model != "subq-snap-1.2" || cfg.Advisers["snap"].BaseURL != "https://api.subq.ai/v1" {
		t.Fatalf("presets or whole replacement changed: %+v", cfg)
	}
	legacy, err := Parse([]byte("version = 1\n[advisers.cli]\nmodel = 'legacy'\ncommand = ['tool', '{model}']\n"), false)
	if err != nil {
		t.Fatal(err)
	}
	old, err := Bind(root, "legacy-task", "small", legacy, false)
	if err != nil {
		t.Fatal(err)
	}
	if old.Version != 1 || Fingerprint(cfg.Advisers["cli"]) != old.Advisers["cli"].Fingerprint {
		t.Fatal("version 2 altered legacy identity")
	}
	d := cfg.Advisers["jev"]
	fingerprint := Fingerprint(d)
	t.Setenv(d.APIKeyEnv, "rotated-key")
	if Fingerprint(d) != fingerprint {
		t.Fatal("key rotation changed the fingerprint")
	}
	for _, mutate := range []func(*Definition){
		func(d *Definition) { d.Model = "other" }, func(d *Definition) { d.Provider = "subq" },
		func(d *Definition) { d.BaseURL = "https://other.test/v1" }, func(d *Definition) { d.APIKeyEnv = "OTHER_KEY" },
		func(d *Definition) { d.Timeout = time.Minute },
	} {
		changed := d
		mutate(&changed)
		if Fingerprint(changed) == fingerprint {
			t.Fatal("native settings drift was not detected")
		}
	}
}

func TestNativeConfigurationRefusesInvalidDefinitions(t *testing.T) {
	for _, test := range []struct{ name, document string }{
		{"version one", "version = 1\n[advisers.x]\nprovider = 'typesafe'"},
		{"empty provider", "version = 2\n[advisers.x]\nprovider = ''"},
		{"unknown", "version = 2\n[advisers.x]\nprovider = 'other'"},
		{"mixed", "version = 2\n[advisers.x]\nprovider = 'subq'\ncommand = []"},
		{"missing custom fields", "version = 2\n[advisers.x]\nprovider = 'systemone'"},
		{"plaintext", "version = 2\n[advisers.x]\nprovider = 'typesafe'\nbase_url = 'http://example.test'"},
		{"url credentials", "version = 2\n[advisers.x]\nprovider = 'typesafe'\nbase_url = 'https://key@example.test'"},
		{"query", "version = 2\n[advisers.x]\nprovider = 'typesafe'\nbase_url = 'https://example.test?key=secret'"},
		{"fragment", "version = 2\n[advisers.x]\nprovider = 'typesafe'\nbase_url = 'https://example.test#secret'"},
		{"key name", "version = 2\n[advisers.x]\nprovider = 'typesafe'\napi_key_env = 'KEY=value'"},
		{"inline key", "version = 2\n[advisers.x]\nprovider = 'typesafe'\napi_key = 'secret'"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := Parse([]byte(test.document), false); err == nil {
				t.Fatal("accepted invalid definition")
			}
		})
	}
}

func TestTypedRequestValidation(t *testing.T) {
	for _, document := range []string{
		`{"version":1,"kind":"score","question":"Why?","levels":["Low","High"]}`,
		`{"version":2,"kind":"score","question":"Why?","levels":["Only"]}`,
		`{"version":2,"kind":"score","question":"Why?","levels":["","High"]}`,
		`{"version":2,"kind":"score","question":"Why?","levels":["Low","High"],"criteria":{}}`,
		`{"version":2,"kind":"boolean","question":"Why?","choices":[{"id":"yes","text":"Yes"}]}`,
		`{"version":2,"kind":"boolean","question":"Why?","criteria":{"true":"yes","true":"duplicate"}}`,
		`{"version":2,"kind":"advice","question":"Why?","levels":[]}`,
	} {
		t.Run(document, func(t *testing.T) {
			if _, err := ParseRequest(strings.NewReader(document)); err == nil {
				t.Fatal("accepted invalid request")
			}
		})
	}
	r := Request{Version: 2, Kind: "boolean", Question: "Ready?"}
	if err := ValidateCapability("", r); err == nil {
		t.Fatal("CLI accepted a boolean")
	}
	r.Kind = "advice"
	if err := ValidateCapability("subq", r); err == nil {
		t.Fatal("native provider accepted text advice")
	}
}

func TestNativeResponseValidation(t *testing.T) {
	choice := Request{Version: 1, Kind: "decision", Question: "Which?", Choices: []Choice{{ID: "A", Text: "First"}, {ID: "b", Text: "Second"}}}
	boolean := Request{Version: 2, Kind: "boolean", Question: "Ready?"}
	score := Request{Version: 2, Kind: "score", Question: "How?", Levels: []string{"Low", "High"}}
	for _, test := range []struct {
		name    string
		request Request
		body    string
	}{
		{"wrong answer name", boolean, `{"answers":{"other":{"type":"noul","noul":0}}}`},
		{"duplicate", boolean, `{"answers":{"result":{"type":"noul","noul":0,"noul":1}}}`},
		{"null zero", boolean, `{"answers":{"result":{"type":"noul","noul":null}}}`},
		{"range", boolean, `{"answers":{"result":{"type":"noul","noul":1.1}}}`},
		{"trailing", boolean, `{"answers":{"result":{"type":"noul","noul":0}}} {}`},
		{"partial usage", boolean, `{"answers":{"result":{"type":"noul","noul":0}},"usage":{"input_tokens":0}}`},
		{"negative usage", boolean, `{"answers":{"result":{"type":"noul","noul":0}},"usage":{"input_tokens":-1,"output_tokens":0}}`},
		{"unknown choice", choice, `{"answers":{"result":{"type":"choice","choice":"c","confidence":1,"probabilities":{"A":1,"b":0}}}}`},
		{"wrong best choice", choice, `{"answers":{"result":{"type":"choice","choice":"b","confidence":1,"probabilities":{"A":1,"b":0}}}}`},
		{"missing probability", choice, `{"answers":{"result":{"type":"choice","choice":"A","confidence":1,"probabilities":{"A":1}}}}`},
		{"not distribution", choice, `{"answers":{"result":{"type":"choice","choice":"A","confidence":1,"probabilities":{"A":1,"b":1}}}}`},
		{"bad legend", score, `{"answers":{"result":{"type":"score","score":0,"confidence":1,"legend":{"0":"Wrong","1":"High"},"probabilities":{"0":1,"1":0}}}}`},
		{"score range", score, `{"answers":{"result":{"type":"score","score":2,"confidence":1,"legend":{"0":"Low","1":"High"},"probabilities":{"0":1,"1":0}}}}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, _, _, err := decodeNativeResponse([]byte(test.body), test.request); err == nil {
				t.Fatal("accepted invalid provider result")
			}
		})
	}
	got, model, usage, err := decodeNativeResponse([]byte(`{"answers":{"result":{"type":"noul","noul":0}}}`), boolean)
	if err != nil || got.ProbabilityTrue == nil || *got.ProbabilityTrue != 0 || usage != nil || model != "unknown" {
		t.Fatalf("unknown usage or zero probability: %+v, %s, %+v, %v", got, model, usage, err)
	}
}

func TestNativeRecordValidationAndOfflineReads(t *testing.T) {
	root := t.TempDir()
	d := nativeTestDefinition(t, "typesafe", "https://example.test/v1")
	binding, err := Bind(root, "native-task", "small", &Config{Advisers: map[string]Definition{"jev": d}, Access: map[string][]string{"coordinator": {"jev"}}}, false)
	if err != nil || binding.Version != 2 {
		t.Fatalf("binding: %+v, %v", binding, err)
	}
	r := Request{Version: 2, Kind: "score", Question: "How?", Levels: []string{"Low", "High"}}
	c, err := Begin(root, Consultation{Task: "native-task", Role: "coordinator", Stage: "code", Adviser: "jev", Request: r, Prompt: "explicit context"})
	if err != nil {
		t.Fatal(err)
	}
	body := `{"model":"actual","answers":{"result":{"type":"score","score":0,"confidence":1,"legend":{"0":"Low","1":"High"},"probabilities":{"0":1,"1":0}}},"usage":{"input_tokens":10,"output_tokens":0}}`
	result, model, usage, err := decodeNativeResponse([]byte(body), r)
	if err != nil {
		t.Fatal(err)
	}
	c.Execution = Execution{Version: 2, Outcome: "success", Answer: body, Result: result, ActualModel: model, Usage: usage, UsageKnown: true, StartedAt: time.Now().UTC(), EndedAt: time.Now().UTC()}
	changed := *c
	changed.Request.Levels = []string{"Changed", "High"}
	if err := Finish(root, changed); err == nil {
		t.Fatal("mutated rubric was accepted")
	}
	if err := Finish(root, *c); err != nil {
		t.Fatal(err)
	}
	t.Setenv(d.APIKeyEnv, "")
	if problems, err := ValidateRecords(root, "native-task"); err != nil || len(problems) != 0 {
		t.Fatalf("offline record: %v, %v", problems, err)
	}
	if _, err := Resolve(root, "native-task", c.ID, "defer", "Needs review", ""); err != nil {
		t.Fatal(err)
	}
	// Modifying a normalized score alone must fail offline validation.
	wrong := 1.0
	c.Execution.Result.Score = &wrong
	data, err := json.Marshal(c.Execution)
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(root, "openspec", "changes", ".advice", "native-task", "exchanges", c.ID, "result.json")
	if err := os.WriteFile(file, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if problems, err := ValidateRecords(root, "native-task"); err == nil && len(problems) == 0 {
		t.Fatal("tampered score passed offline validation")
	}
}
