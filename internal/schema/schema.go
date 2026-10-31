package schema

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Schema implements the JSON Schema subset used by tool manifests and workflow
// inputs. It intentionally rejects malformed schemas instead of silently
// ignoring constraints.
type Schema struct {
	raw map[string]any
}

type Issue struct {
	Path    string `json:"path"`
	Message string `json:"message"`
}

func Compile(raw []byte) (*Schema, error) {
	if len(raw) == 0 {
		return &Schema{raw: map[string]any{}}, nil
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("invalid JSON schema: %w", err)
	}
	s := &Schema{raw: m}
	if err := s.check(); err != nil {
		return nil, err
	}
	return s, nil
}

func CompileValue(m map[string]any) (*Schema, error) {
	b, err := json.Marshal(m)
	if err != nil {
		return nil, err
	}
	return Compile(b)
}

func (s *Schema) Raw() map[string]any { return s.raw }

func (s *Schema) check() error {
	if s.raw == nil {
		return nil
	}
	if t, ok := s.raw["type"]; ok {
		if err := validType(t); err != nil {
			return err
		}
	}
	if props, ok := s.raw["properties"].(map[string]any); ok {
		for name, raw := range props {
			m, ok := raw.(map[string]any)
			if !ok {
				return fmt.Errorf("properties.%s: schema must be an object", name)
			}
			if err := (&Schema{raw: m}).check(); err != nil {
				return fmt.Errorf("properties.%s: %w", name, err)
			}
		}
	}
	if items, ok := s.raw["items"].(map[string]any); ok {
		if err := (&Schema{raw: items}).check(); err != nil {
			return fmt.Errorf("items: %w", err)
		}
	}
	if ap, ok := s.raw["additionalProperties"].(map[string]any); ok {
		if err := (&Schema{raw: ap}).check(); err != nil {
			return fmt.Errorf("additionalProperties: %w", err)
		}
	}
	for _, key := range []string{"oneOf", "anyOf", "allOf"} {
		if arr, ok := s.raw[key].([]any); ok {
			for i, raw := range arr {
				m, ok := raw.(map[string]any)
				if !ok {
					return fmt.Errorf("%s[%d]: schema must be an object", key, i)
				}
				if err := (&Schema{raw: m}).check(); err != nil {
					return fmt.Errorf("%s[%d]: %w", key, i, err)
				}
			}
		}
	}
	return nil
}

func validType(v any) error {
	switch t := v.(type) {
	case string:
		switch t {
		case "object", "array", "string", "number", "integer", "boolean", "null":
			return nil
		}
		return fmt.Errorf("unknown type %q", t)
	case []any:
		for _, x := range t {
			if err := validType(x); err != nil {
				return err
			}
		}
		return nil
	default:
		return fmt.Errorf("type must be a string or array of strings")
	}
}

func (s *Schema) Validate(v any) []Issue {
	var issues []Issue
	s.validate("", v, &issues)
	return issues
}

func (s *Schema) validate(path string, v any, issues *[]Issue) {
	if s.raw == nil {
		return
	}
	if ref, ok := s.raw["$ref"].(string); ok && strings.HasPrefix(ref, "#/definitions/") {
		defs, _ := s.raw["definitions"].(map[string]any)
		name := strings.TrimPrefix(ref, "#/definitions/")
		if m, ok := defs[name].(map[string]any); ok {
			(&Schema{raw: m}).validate(path, v, issues)
			return
		}
	}
	if t, ok := s.raw["type"]; ok && !matchesType(t, v) {
		*issues = append(
			*issues,
			Issue{
				Path:    path,
				Message: fmt.Sprintf("expected %s, got %s", typeName(t), valueType(v)),
			},
		)
		return
	}
	if enum, ok := s.raw["enum"].([]any); ok {
		found := false
		for _, e := range enum {
			if equalJSON(e, v) {
				found = true
				break
			}
		}
		if !found {
			*issues = append(
				*issues,
				Issue{Path: path, Message: "value is not one of the allowed enum values"},
			)
		}
	}
	switch val := v.(type) {
	case map[string]any:
		props, _ := s.raw["properties"].(map[string]any)
		if req, ok := s.raw["required"].([]any); ok {
			for _, r := range req {
				name, _ := r.(string)
				if _, exists := val[name]; !exists {
					*issues = append(
						*issues,
						Issue{Path: join(path, name), Message: "required field is missing"},
					)
				}
			}
		}
		for name, pv := range val {
			if props != nil {
				if raw, exists := props[name]; exists {
					if m, ok := raw.(map[string]any); ok {
						(&Schema{raw: m}).validate(join(path, name), pv, issues)
					}
				}
			}
			if _, known := props[name]; !known {
				switch ap := s.raw["additionalProperties"].(type) {
				case bool:
					if !ap {
						*issues = append(
							*issues,
							Issue{Path: join(path, name), Message: "unexpected field"},
						)
					}
				case map[string]any:
					(&Schema{raw: ap}).validate(join(path, name), pv, issues)
				}
			}
		}
	case []any:
		if items, ok := s.raw["items"].(map[string]any); ok {
			for i, item := range val {
				(&Schema{raw: items}).validate(fmt.Sprintf("%s[%d]", path, i), item, issues)
			}
		}
		if n, ok := number(s.raw["minItems"]); ok && float64(len(val)) < n {
			*issues = append(
				*issues,
				Issue{Path: path, Message: fmt.Sprintf("must contain at least %v items", n)},
			)
		}
		if n, ok := number(s.raw["maxItems"]); ok && float64(len(val)) > n {
			*issues = append(
				*issues,
				Issue{Path: path, Message: fmt.Sprintf("must contain at most %v items", n)},
			)
		}
	case string:
		if n, ok := number(s.raw["minLength"]); ok && float64(len([]rune(val))) < n {
			*issues = append(
				*issues,
				Issue{Path: path, Message: fmt.Sprintf("must be at least %v characters", n)},
			)
		}
		if n, ok := number(s.raw["maxLength"]); ok && float64(len([]rune(val))) > n {
			*issues = append(
				*issues,
				Issue{Path: path, Message: fmt.Sprintf("must be at most %v characters", n)},
			)
		}
		if pat, ok := s.raw["pattern"].(string); ok {
			if re, err := regexp.Compile(pat); err == nil && !re.MatchString(val) {
				*issues = append(
					*issues,
					Issue{Path: path, Message: "does not match the required pattern"},
				)
			}
		}
	case float64:
		if n, ok := number(s.raw["minimum"]); ok && val < n {
			*issues = append(*issues, Issue{Path: path, Message: fmt.Sprintf("must be >= %v", n)})
		}
		if n, ok := number(s.raw["maximum"]); ok && val > n {
			*issues = append(*issues, Issue{Path: path, Message: fmt.Sprintf("must be <= %v", n)})
		}
	}
	if one, ok := s.raw["oneOf"].([]any); ok {
		okCount := 0
		for _, raw := range one {
			if m, ok := raw.(map[string]any); ok {
				if len((&Schema{raw: m}).Validate(v)) == 0 {
					okCount++
				}
			}
		}
		if okCount != 1 {
			*issues = append(
				*issues,
				Issue{Path: path, Message: "must match exactly one alternative"},
			)
		}
	}
	if anyOf, ok := s.raw["anyOf"].([]any); ok {
		okCount := 0
		for _, raw := range anyOf {
			if m, ok := raw.(map[string]any); ok {
				if len((&Schema{raw: m}).Validate(v)) == 0 {
					okCount++
				}
			}
		}
		if okCount == 0 {
			*issues = append(
				*issues,
				Issue{Path: path, Message: "must match at least one alternative"},
			)
		}
	}
	if all, ok := s.raw["allOf"].([]any); ok {
		for _, raw := range all {
			if m, ok := raw.(map[string]any); ok {
				(&Schema{raw: m}).validate(path, v, issues)
			}
		}
	}
}

// ApplyDefaults fills object defaults declared in the schema, recursively.
func (s *Schema) ApplyDefaults(v any) any {
	obj, ok := v.(map[string]any)
	if !ok {
		return v
	}
	props, _ := s.raw["properties"].(map[string]any)
	out := make(map[string]any, len(obj)+len(props))
	for k, val := range obj {
		out[k] = val
	}
	for name, raw := range props {
		pm, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		child := &Schema{raw: pm}
		if cur, exists := out[name]; exists {
			out[name] = child.ApplyDefaults(cur)
			continue
		}
		if def, exists := pm["default"]; exists {
			out[name] = def
		}
	}
	return out
}

func matchesType(t, v any) bool {
	switch tt := t.(type) {
	case string:
		return matchesOne(tt, v)
	case []any:
		for _, x := range tt {
			if s, ok := x.(string); ok && matchesOne(s, v) {
				return true
			}
		}
	}
	return true
}

func matchesOne(t string, v any) bool {
	switch t {
	case "object":
		_, ok := v.(map[string]any)
		return ok
	case "array":
		_, ok := v.([]any)
		return ok
	case "string":
		_, ok := v.(string)
		return ok
	case "number":
		switch v.(type) {
		case float64, int, int64, json.Number:
			return true
		}
		return false
	case "integer":
		switch n := v.(type) {
		case float64:
			return n == float64(int64(n))
		case int, int64:
			return true
		case json.Number:
			return !strings.Contains(n.String(), ".")
		}
		return false
	case "boolean":
		_, ok := v.(bool)
		return ok
	case "null":
		return v == nil
	}
	return true
}

func typeName(t any) string {
	switch v := t.(type) {
	case string:
		return v
	case []any:
		parts := []string{}
		for _, x := range v {
			parts = append(parts, fmt.Sprint(x))
		}
		return strings.Join(parts, " or ")
	}
	return "value"
}

func valueType(v any) string {
	switch v.(type) {
	case nil:
		return "null"
	case bool:
		return "boolean"
	case string:
		return "string"
	case float64:
		return "number"
	case int, int64, json.Number:
		return "number"
	case []any:
		return "array"
	case map[string]any:
		return "object"
	}
	return fmt.Sprintf("%T", v)
}

func equalJSON(a, b any) bool {
	ab, _ := json.Marshal(a)
	bb, _ := json.Marshal(b)
	return string(ab) == string(bb)
}

func number(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case json.Number:
		f, err := n.Float64()
		return f, err == nil
	case string:
		f, err := strconv.ParseFloat(n, 64)
		return f, err == nil
	}
	return 0, false
}

func join(path, name string) string {
	if path == "" {
		return name
	}
	return path + "." + name
}
