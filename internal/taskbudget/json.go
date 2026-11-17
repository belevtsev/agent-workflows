package taskbudget

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
)

// object validates exact, unique field names before typed decoding. In
// particular, encoding/json's case-insensitive aliases cannot fill a field.
func object(data []byte, allowed, required []string) (map[string]json.RawMessage, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	first, err := decoder.Token()
	if err != nil || first != json.Delim('{') {
		return nil, errors.New("expected a JSON object")
	}
	fields := map[string]json.RawMessage{}
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return nil, err
		}
		key, ok := token.(string)
		if !ok || !slices.Contains(allowed, key) {
			return nil, fmt.Errorf("unknown field %q", token)
		}
		if _, exists := fields[key]; exists {
			return nil, fmt.Errorf("duplicate field %q", key)
		}
		var value json.RawMessage
		if err = decoder.Decode(&value); err != nil {
			return nil, err
		}
		if bytes.Equal(value, []byte("null")) {
			return nil, fmt.Errorf("field %q must not be null", key)
		}
		fields[key] = value
	}
	if _, err = decoder.Token(); err != nil {
		return nil, err
	}
	if _, err = decoder.Token(); !errors.Is(err, io.EOF) {
		return nil, errors.New("expected exactly one JSON object")
	}
	for _, key := range required {
		if _, exists := fields[key]; !exists {
			return nil, fmt.Errorf("missing required field %q", key)
		}
	}
	return fields, nil
}
func decode(data []byte, task string) (*Record, error) {
	fields, err := object(data, []string{"version", "task", "entries"}, []string{"version", "task", "entries"})
	if err != nil {
		return nil, err
	}
	var record Record
	if err = json.Unmarshal(data, &record); err != nil {
		return nil, err
	}
	if record.Version != 1 || record.Task != task {
		return nil, errors.New("budget requires version 1 and the matching task")
	}
	entries, err := object(fields["entries"], []string{Tokens, Seconds}, nil)
	if err != nil {
		return nil, err
	}
	if len(entries) == 0 {
		return nil, errors.New("budget requires at least one unit")
	}
	record.raw = map[string][]byte{}
	for unit, raw := range entries {
		parts, err := object(raw, []string{"estimate", "limit"}, nil)
		if err != nil {
			return nil, err
		}
		if estimate, ok := parts["estimate"]; ok {
			if _, err := object(estimate, []string{"min", "max", "basis", "assumptions"}, []string{"min", "max", "basis"}); err != nil {
				return nil, err
			}
		}
		if limit, ok := parts["limit"]; ok {
			if _, err := object(limit, []string{"n", "source"}, []string{"n", "source"}); err != nil {
				return nil, err
			}
		}
		if err := validateEntry(unit, record.Entries[unit]); err != nil {
			return nil, err
		}
		record.raw[unit] = bytes.Clone(raw)
	}
	return &record, nil
}
