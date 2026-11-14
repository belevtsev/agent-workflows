package cli

import (
	"bytes"
	json "encoding/json/v2"
	"maps"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/belevtsev/agent-workflows/v3/internal/taskflow"
	"github.com/spf13/cobra"
)

// schemaInputs maps each command cw task schema covers to the Go type its
// command decodes the JSON input into with readTaskInput.
var schemaInputs = map[string]reflect.Type{
	"start":      reflect.TypeFor[taskflow.Plan](),
	"assign":     reflect.TypeFor[taskflow.WorkerBrief](),
	"handoff":    reflect.TypeFor[taskflow.WorkerResult](),
	"accept":     reflect.TypeFor[taskflow.WorkerAcceptance](),
	"assess":     reflect.TypeFor[taskflow.AssessmentInput](),
	"add-checks": reflect.TypeFor[taskflow.AmendmentInput](),
}

// taskSubcommand returns cw task NAME from a fresh command tree.
func taskSubcommand(t *testing.T, name string) *cobra.Command {
	t.Helper()
	config, _ := testConfig(t)
	command, _, err := NewCommand(config).Find([]string{"task", name})
	if err != nil || command.Name() != name {
		t.Fatalf("no cw task %s: %v", name, err)
	}
	return command
}

// The task commands that read a JSON input file are exactly the ones cw task
// schema covers, apart from consult, which reads the record cw consult wrote.
// oracle: cw task schema exists, and the cw task subcommands with an --input
// or --plan flag, minus consult, equal the schemaCommands list and the keys
// of schemaInputs.
func TestTaskSchemaCoversEveryJSONInputCommand(t *testing.T) {
	taskSubcommand(t, "schema")
	task := taskSubcommand(t, "start").Parent()
	var reading []string
	for _, command := range task.Commands() {
		if (command.Flags().Lookup("input") != nil || command.Flags().Lookup("plan") != nil) && command.Name() != "consult" {
			reading = append(reading, command.Name())
		}
	}
	slices.Sort(reading)
	listed := slices.Sorted(slices.Values(schemaCommands))
	if !slices.Equal(reading, listed) || !slices.Equal(listed, slices.Sorted(maps.Keys(schemaInputs))) {
		t.Fatalf("commands with JSON input %v, cw task schema covers %v, the test maps %v", reading, listed, slices.Sorted(maps.Keys(schemaInputs)))
	}
}

// cw task schema NAME prints a JSON Schema whose members, at every depth,
// are the JSON members of the Go type the command decodes, each with the
// JSON type that Go type takes.
// oracle: reflection over the input struct: each struct's property set
// equals its fields' JSON names, with additionalProperties false (the decoder
// rejects unknown members); a slice is an array whose items match the
// element; a string, integer, float or bool field has that JSON type or an
// enum of values of it.
func TestTaskSchemaMatchesTheInputStruct(t *testing.T) {
	for _, name := range schemaCommands {
		t.Run(name, func(t *testing.T) {
			config, out := testConfig(t)
			if err := Execute(t.Context(), []string{"task", "schema", name}, config); err != nil {
				t.Fatal(err)
			}
			var schema map[string]any
			if err := json.Unmarshal(out.Bytes(), &schema); err != nil {
				t.Fatalf("schema is not JSON: %v", err)
			}
			if schema["$schema"] != "https://json-schema.org/draft/2020-12/schema" {
				t.Errorf("$schema = %v", schema["$schema"])
			}
			matchSchema(t, name, schema, schema, schemaInputs[name])
		})
	}
}

// cw task schema refuses a missing, unknown or extra argument as a usage
// error that names the commands it covers.
// oracle: exit status 2 (docs/operations.md, Exit statuses) and the message
// "name one command: start|assign|handoff|accept|assess|add-checks".
func TestTaskSchemaRefusesAnUnknownCommand(t *testing.T) {
	for _, args := range [][]string{{}, {"run"}, {"start", "assign"}} {
		config, _ := testConfig(t)
		err := Execute(t.Context(), append([]string{"task", "schema"}, args...), config)
		if ExitCode(err) != 2 || err == nil || err.Error() != "name one command: start|assign|handoff|accept|assess|add-checks" {
			t.Errorf("cw task schema %v: exit %d (%v), want 2 naming the commands", args, ExitCode(err), err)
		}
	}
}

// matchSchema checks node, a schema in root, against the Go type typ.
func matchSchema(t *testing.T, path string, root, node map[string]any, typ reflect.Type) {
	t.Helper()
	if ref, ok := node["$ref"].(string); ok {
		defs, _ := root["$defs"].(map[string]any)
		resolved, ok := defs[strings.TrimPrefix(ref, "#/$defs/")].(map[string]any)
		if !strings.HasPrefix(ref, "#/$defs/") || !ok {
			t.Errorf("%s: unresolved $ref %q", path, ref)
			return
		}
		node = resolved
	}
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	jsonType := func(want string) {
		if enum, ok := node["enum"].([]any); ok {
			for _, value := range enum {
				if (want == "string") != (reflect.TypeOf(value) == reflect.TypeFor[string]()) {
					t.Errorf("%s: enum value %v is not a %s", path, value, want)
				}
			}
			return
		}
		if node["type"] != want {
			t.Errorf("%s: type %v, want %s", path, node["type"], want)
		}
	}
	switch {
	case typ == reflect.TypeFor[time.Time]():
		jsonType("string")
	case typ.Kind() == reflect.Struct:
		jsonType("object")
		if node["additionalProperties"] != false {
			t.Errorf("%s: additionalProperties must be false, as the decoder rejects unknown members", path)
		}
		properties, _ := node["properties"].(map[string]any)
		fields := jsonFields(t, typ)
		if got, want := slices.Sorted(maps.Keys(properties)), slices.Sorted(maps.Keys(fields)); !slices.Equal(got, want) {
			t.Errorf("%s: properties %v, %s has JSON members %v", path, got, typ, want)
		}
		for member, field := range fields {
			if property, ok := properties[member].(map[string]any); ok {
				matchSchema(t, path+"."+member, root, property, field)
			}
		}
	case typ.Kind() == reflect.Slice:
		jsonType("array")
		items, _ := node["items"].(map[string]any)
		if items == nil {
			t.Errorf("%s: an array needs items", path)
			return
		}
		matchSchema(t, path+"[]", root, items, typ.Elem())
	case typ.Kind() == reflect.String:
		jsonType("string")
	case typ.Kind() == reflect.Bool:
		jsonType("boolean")
	case typ.Kind() >= reflect.Int && typ.Kind() <= reflect.Uint64:
		jsonType("integer")
	case typ.Kind() == reflect.Float32 || typ.Kind() == reflect.Float64:
		jsonType("number")
	default:
		t.Errorf("%s: no schema rule for %s", path, typ)
	}
}

// jsonFields maps the JSON member names of struct typ to their field types.
func jsonFields(t *testing.T, typ reflect.Type) map[string]reflect.Type {
	t.Helper()
	fields := map[string]reflect.Type{}
	for field := range typ.Fields() {
		if !field.IsExported() {
			continue
		}
		if field.Anonymous {
			t.Fatalf("%s embeds %s: extend jsonFields", typ, field.Type)
		}
		name, _, _ := strings.Cut(field.Tag.Get("json"), ",")
		switch name {
		case "-":
			continue
		case "":
			name = field.Name
		}
		fields[name] = field.Type
	}
	return fields
}

// Every task command that reads JSON input, and run and verify, shows an
// example in its help; each JSON payload in an example, written as a
// here-document, decodes with the command's own strict decoder.
// oracle: Example is non-empty for each command; each here-document of a
// schema command's Example, the lines after a line ending in <<'EOF' up to a
// line EOF, decodes with taskflow.Decode, which readTaskInput uses, into
// that command's input type; each schema command's Example has at least one.
func TestTaskExamplesDecodeWithTheCommandsDecoder(t *testing.T) {
	for _, name := range append(slices.Clone(schemaCommands), "run", "verify", "consult", "usage") {
		t.Run(name, func(t *testing.T) {
			example := taskSubcommand(t, name).Example
			if strings.TrimSpace(example) == "" {
				t.Fatal("no example")
			}
			payloads := hereDocuments(example)
			typ, reads := schemaInputs[name]
			if !reads {
				return
			}
			if len(payloads) == 0 {
				t.Fatal("the example has no JSON payload")
			}
			for _, payload := range payloads {
				if err := taskflow.Decode(bytes.NewReader([]byte(payload)), reflect.New(typ).Interface()); err != nil {
					t.Errorf("example payload does not decode: %v\n%s", err, payload)
				}
			}
		})
	}
}

// hereDocuments returns the bodies of the <<'EOF' here-documents in text.
func hereDocuments(text string) []string {
	var bodies []string
	var body []string
	inside := false
	for line := range strings.Lines(text) {
		line = strings.TrimSuffix(line, "\n")
		switch {
		case inside && line == "EOF":
			bodies = append(bodies, strings.Join(body, "\n"))
			body, inside = nil, false
		case inside:
			body = append(body, line)
		case strings.HasSuffix(line, "<<'EOF'"):
			inside = true
		}
	}
	return bodies
}
