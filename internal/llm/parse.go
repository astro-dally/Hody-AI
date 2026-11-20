package llm

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
)

// Parse decodes a model answer into out, a pointer to a struct. It rejects
// unknown fields, missing fields (every field without `omitempty` is
// required), trailing data, and, if out has a Validate method, answers
// that fail it.
func Parse(text string, out any) error {
	text = strings.TrimSpace(text)
	text = strings.TrimPrefix(text, "```json")
	text = strings.TrimPrefix(text, "```")
	text = strings.TrimSuffix(text, "```")
	text = strings.TrimSpace(text)
	if !strings.HasPrefix(text, "{") {
		return errors.New("answer is not a JSON object")
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(text), &fields); err != nil {
		return fmt.Errorf("answer is not valid JSON: %v", err)
	}
	if missing := missingFields(out, fields); len(missing) > 0 {
		return fmt.Errorf("missing required fields: %s", strings.Join(missing, ", "))
	}
	dec := json.NewDecoder(bytes.NewReader([]byte(text)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(out); err != nil {
		return fmt.Errorf("answer does not match the schema: %v", err)
	}
	if dec.More() {
		return errors.New("unexpected data after the JSON object")
	}
	if v, ok := out.(interface{ Validate() error }); ok {
		return v.Validate()
	}
	return nil
}

// missingFields lists required JSON fields of *out that are absent.
func missingFields(out any, present map[string]json.RawMessage) []string {
	t := reflect.TypeOf(out)
	if t.Kind() != reflect.Pointer || t.Elem().Kind() != reflect.Struct {
		return nil
	}
	var missing []string
	t = t.Elem()
	for i := 0; i < t.NumField(); i++ {
		tag := t.Field(i).Tag.Get("json")
		name, opts, _ := strings.Cut(tag, ",")
		if name == "" || name == "-" || strings.Contains(opts, "omitempty") {
			continue
		}
		if _, ok := present[name]; !ok {
			missing = append(missing, name)
		}
	}
	return missing
}
