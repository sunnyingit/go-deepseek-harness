package tools

import (
	"fmt"
	"math"
	"sort"
)

// ValueKind is one JSON Schema node's shape.
type ValueKind string

const (
	KindString  ValueKind = "string"
	KindNumber  ValueKind = "number"
	KindInteger ValueKind = "integer"
	KindBoolean ValueKind = "boolean"
	KindNull    ValueKind = "null"
	KindArray   ValueKind = "array"
	KindObject  ValueKind = "object"
	// KindJSON is the author-only escape hatch: any value validates, unchecked.
	KindJSON  ValueKind = "json"
	KindOneOf ValueKind = "oneOf"
)

// ValueSchema describes one JSON value's shape — a single flattened struct
// standing in for the TS original's eight-member ValueSchemaSpec union. Go
// has no tagged unions; Kind plus only the fields that kind actually uses is
// simpler here than an interface plus eight concrete implementations.
type ValueSchema struct {
	Kind ValueKind

	Description string
	Title       string
	Default     any
	Examples    any

	Enum  []any // string/number/integer/boolean/null
	Const any   // string/number/integer/boolean/null

	Items *ValueSchema // array

	Properties           ParameterSchema // object
	AdditionalProperties bool            // object

	OneOf []ValueSchema // oneOf
}

// Param is one parameter schema entry: a ValueSchema plus whether the model
// must supply it.
type Param struct {
	ValueSchema
	Required bool
}

// ParameterSchema is a tool's whole `parameters` declaration. The root it
// describes is always implicitly open (no additionalProperties enforcement
// at that level) — only a nested object schema enforces its own
// AdditionalProperties flag, matching the TS DSL.
type ParameterSchema map[string]Param

// ToJSONSchema renders one node as wire JSON Schema.
func (v ValueSchema) ToJSONSchema() map[string]any {
	m := map[string]any{}
	if v.Kind == KindOneOf {
		variants := make([]any, len(v.OneOf))
		for i, s := range v.OneOf {
			variants[i] = s.ToJSONSchema()
		}
		m["oneOf"] = variants
		annotateSchema(m, v)
		return m
	}
	if v.Kind == KindJSON {
		annotateSchema(m, v)
		return m
	}

	m["type"] = string(v.Kind)
	if len(v.Enum) > 0 {
		m["enum"] = v.Enum
	}
	if v.Const != nil {
		m["const"] = v.Const
	}
	switch v.Kind {
	case KindArray:
		if v.Items != nil {
			m["items"] = v.Items.ToJSONSchema()
		}
	case KindObject:
		props := map[string]any{}
		var required []string
		for name, p := range v.Properties {
			props[name] = p.ValueSchema.ToJSONSchema()
			if p.Required {
				required = append(required, name)
			}
		}
		sort.Strings(required)
		m["properties"] = props
		if len(required) > 0 {
			m["required"] = required
		}
		m["additionalProperties"] = v.AdditionalProperties
	}
	annotateSchema(m, v)
	return m
}

func annotateSchema(m map[string]any, v ValueSchema) {
	if v.Description != "" {
		m["description"] = v.Description
	}
	if v.Title != "" {
		m["title"] = v.Title
	}
	if v.Default != nil {
		m["default"] = v.Default
	}
	if v.Examples != nil {
		m["examples"] = v.Examples
	}
}

// ToJSONSchema renders the whole parameter set as the wire object schema —
// what a ToolSchema.Parameters carries.
func (s ParameterSchema) ToJSONSchema() map[string]any {
	props := map[string]any{}
	var required []string
	for name, p := range s {
		props[name] = p.ValueSchema.ToJSONSchema()
		if p.Required {
			required = append(required, name)
		}
	}
	sort.Strings(required)
	m := map[string]any{"type": "object", "properties": props}
	if len(required) > 0 {
		m["required"] = required
	}
	return m
}

// ValidateArgs checks args against s, returning one message per violation
// (nil = valid). Unrecognized top-level keys are never a violation — the
// parameter root is always open, matching ParameterSchema's doc comment.
func ValidateArgs(s ParameterSchema, args map[string]any) []string {
	var violations []string
	for name, p := range s {
		value, present := args[name]
		if !present {
			if p.Required {
				violations = append(violations, fmt.Sprintf("%q is required", name))
			}
			continue
		}
		violations = append(violations, validateValue(p.ValueSchema, value, name)...)
	}
	sort.Strings(violations)
	return violations
}

// validateValue checks one JSON value against spec, prefixing every
// violation with path.
func validateValue(spec ValueSchema, value any, path string) []string {
	switch spec.Kind {
	case KindJSON:
		return nil
	case KindOneOf:
		for _, branch := range spec.OneOf {
			if len(validateValue(branch, value, path)) == 0 {
				// TS's "exact-one" is a type-level constraint (the tuple needs
				// >=2 variants); at runtime, matching any one branch is valid.
				return nil
			}
		}
		return []string{fmt.Sprintf("%s: matches none of the %d alternatives", path, len(spec.OneOf))}
	case KindString:
		s, ok := value.(string)
		if !ok {
			return []string{fmt.Sprintf("%s: expected string, got %s", path, jsonTypeName(value))}
		}
		return validateEnumConst(spec, s, path)
	case KindBoolean:
		b, ok := value.(bool)
		if !ok {
			return []string{fmt.Sprintf("%s: expected boolean, got %s", path, jsonTypeName(value))}
		}
		return validateEnumConst(spec, b, path)
	case KindNull:
		if value != nil {
			return []string{fmt.Sprintf("%s: expected null, got %s", path, jsonTypeName(value))}
		}
		return nil
	case KindNumber, KindInteger:
		n, ok := numericValue(value)
		if !ok {
			return []string{fmt.Sprintf("%s: expected %s, got %s", path, spec.Kind, jsonTypeName(value))}
		}
		if spec.Kind == KindInteger && n != math.Trunc(n) {
			return []string{fmt.Sprintf("%s: expected integer, got a fractional number", path)}
		}
		return validateEnumConst(spec, n, path)
	case KindArray:
		arr, ok := value.([]any)
		if !ok {
			return []string{fmt.Sprintf("%s: expected array, got %s", path, jsonTypeName(value))}
		}
		var violations []string
		if spec.Items != nil {
			for i, elem := range arr {
				violations = append(violations, validateValue(*spec.Items, elem, fmt.Sprintf("%s[%d]", path, i))...)
			}
		}
		return violations
	case KindObject:
		obj, ok := value.(map[string]any)
		if !ok {
			return []string{fmt.Sprintf("%s: expected object, got %s", path, jsonTypeName(value))}
		}
		var violations []string
		for name, p := range spec.Properties {
			fieldPath := path + "." + name
			fv, present := obj[name]
			if !present {
				if p.Required {
					violations = append(violations, fmt.Sprintf("%s is required", fieldPath))
				}
				continue
			}
			violations = append(violations, validateValue(p.ValueSchema, fv, fieldPath)...)
		}
		if !spec.AdditionalProperties {
			for name := range obj {
				if _, declared := spec.Properties[name]; !declared {
					violations = append(violations, fmt.Sprintf("%s: unexpected property %q", path, name))
				}
			}
		}
		sort.Strings(violations)
		return violations
	default:
		return []string{fmt.Sprintf("%s: unknown schema kind %q", path, spec.Kind)}
	}
}

// validateEnumConst applies the two annotations every scalar kind shares.
// value must already be the Go-native type the caller matched (string,
// bool, or float64) so `==`/slice-membership comparisons are meaningful.
func validateEnumConst(spec ValueSchema, value any, path string) []string {
	if spec.Const != nil && spec.Const != value {
		return []string{fmt.Sprintf("%s: expected const %v, got %v", path, spec.Const, value)}
	}
	if len(spec.Enum) > 0 {
		for _, e := range spec.Enum {
			if e == value {
				return nil
			}
		}
		return []string{fmt.Sprintf("%s: %v is not one of the allowed values", path, value)}
	}
	return nil
}

// numericValue accepts any Go numeric type a caller might reasonably
// construct a schema value with (float64 from JSON decoding, int/int64 from
// a hand-built test or literal call site) and normalizes it to float64.
func numericValue(value any) (float64, bool) {
	switch n := value.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int32:
		return float64(n), true
	case int64:
		return float64(n), true
	default:
		return 0, false
	}
}

func jsonTypeName(value any) string {
	switch value.(type) {
	case nil:
		return "null"
	case bool:
		return "boolean"
	case string:
		return "string"
	case float64, float32, int, int32, int64:
		return "number"
	case []any:
		return "array"
	case map[string]any:
		return "object"
	default:
		return fmt.Sprintf("%T", value)
	}
}
