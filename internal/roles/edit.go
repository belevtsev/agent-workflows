package roles

import (
	"bytes"
	"fmt"
	"slices"
	"strings"

	"github.com/pelletier/go-toml/v2"
	"github.com/pelletier/go-toml/v2/unstable"
)

// Edit sets the nonempty fields of model, or resets both overrides. It keeps
// unrelated expressions and comments; an edited inline table is re-encoded.
func Edit(data []byte, host, role string, model Model, reset bool) ([]byte, error) {
	if !slices.Contains(Hosts, host) || !Known(role) {
		return nil, fmt.Errorf("unknown host %q or role %q", host, role)
	}
	if _, err := Parse(data); err != nil {
		return nil, err
	}
	for _, field := range []struct{ key, value string }{{"model", model.Model}, {"effort", model.Effort}} {
		if !reset && field.value == "" {
			continue
		}
		var err error
		data, err = editField(data, []string{host, role, field.key}, field.value, reset)
		if err != nil {
			return nil, err
		}
	}
	if _, err := Parse(data); err != nil {
		return nil, err
	}
	return data, nil
}

func editField(data []byte, path []string, value string, reset bool) ([]byte, error) {
	var raw map[string]any
	if err := toml.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	parent := raw
	exists := true
	for _, key := range path[:2] {
		next, ok := parent[key].(map[string]any)
		exists = exists && ok
		if !ok {
			next = map[string]any{}
			parent[key] = next
		}
		parent = next
	}
	if reset {
		if _, ok := parent[path[2]]; !ok {
			return data, nil
		}
		delete(parent, path[2])
	} else {
		parent[path[2]] = value
	}
	return editExpression(data, raw, path, reset, !exists)
}

func editExpression(data []byte, raw map[string]any, path []string, reset, newRole bool) ([]byte, error) {
	var parser unstable.Parser
	parser.Reset(data)
	var table, best []string
	insert := len(data)
	inBest := true
	for parser.NextExpression() {
		node := parser.Expression()
		keys := expressionKeys(node)
		if node.Kind == unstable.Table {
			if inBest {
				insert = lineStart(data, node.Child().Raw.Offset)
				inBest = false
			}
			table = keys
			if prefix(path[:2], table) && len(table) >= len(best) {
				best, insert, inBest = table, len(data), true
			}
			continue
		}
		if node.Kind != unstable.KeyValue {
			continue
		}
		full := append(slices.Clone(table), keys...)
		if !prefix(path, full) {
			continue
		}
		var replacement []byte
		if !reset || len(full) < len(path) {
			var selected any = raw
			for _, key := range full {
				selected = selected.(map[string]any)[key]
			}
			encoded, err := inlineValue(selected)
			if err != nil {
				return nil, err
			}
			replacement = []byte(strings.Join(keys, ".") + " = " + encoded)
		}
		return replaceExpression(data, int(node.Raw.Offset), int(node.Raw.Length), replacement), nil
	}
	if err := parser.Error(); err != nil {
		return nil, err
	}
	encoded, err := inlineValue(raw[path[0]].(map[string]any)[path[1]].(map[string]any)[path[2]])
	if err != nil {
		return nil, err
	}
	newline := "\n"
	if bytes.Contains(data, []byte("\r\n")) {
		newline = "\r\n"
	}
	addition := ""
	if newRole {
		best, insert = path[:2], len(data)
		addition = newline + "[" + strings.Join(best, ".") + "]" + newline
	}
	addition += strings.Join(path[len(best):], ".") + " = " + encoded + newline
	if insert > 0 && data[insert-1] != '\n' {
		addition = newline + addition
	}
	return replaceExpression(data, insert, 0, []byte(addition)), nil
}

func expressionKeys(node *unstable.Node) []string {
	var keys []string
	if node.Kind == unstable.Table || node.Kind == unstable.KeyValue {
		for it := node.Key(); it.Next(); {
			keys = append(keys, string(it.Node().Data))
		}
	}
	return keys
}

func prefix(path, candidate []string) bool {
	return len(candidate) <= len(path) && slices.Equal(path[:len(candidate)], candidate)
}

func lineStart(data []byte, offset uint32) int {
	return bytes.LastIndexByte(data[:offset], '\n') + 1
}

func inlineValue(value any) (string, error) {
	var out bytes.Buffer
	if err := toml.NewEncoder(&out).SetTablesInline(true).Encode(map[string]any{"value": value}); err != nil {
		return "", err
	}
	return strings.TrimSuffix(strings.TrimPrefix(out.String(), "value = "), "\n"), nil
}

func replaceExpression(data []byte, offset, length int, replacement []byte) []byte {
	out := append([]byte(nil), data[:offset]...)
	out = append(out, replacement...)
	return append(out, data[offset+length:]...)
}
