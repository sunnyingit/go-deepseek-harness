package tools

import (
	"reflect"
	"testing"
)

func TestValidateArgsRequiredMissing(t *testing.T) {
	s := ParameterSchema{"path": {ValueSchema: ValueSchema{Kind: KindString}, Required: true}}
	v := ValidateArgs(s, map[string]any{})
	if len(v) != 1 || v[0] != `"path" is required` {
		t.Fatalf("violations = %v", v)
	}
}

func TestValidateArgsOptionalMissingIsFine(t *testing.T) {
	s := ParameterSchema{"limit": {ValueSchema: ValueSchema{Kind: KindNumber}}}
	if v := ValidateArgs(s, map[string]any{}); len(v) != 0 {
		t.Fatalf("violations = %v, want none", v)
	}
}

func TestValidateArgsUnrecognizedTopLevelKeyIsOpen(t *testing.T) {
	s := ParameterSchema{"path": {ValueSchema: ValueSchema{Kind: KindString}, Required: true}}
	v := ValidateArgs(s, map[string]any{"path": "x", "extra": true})
	if len(v) != 0 {
		t.Fatalf("violations = %v, want none — the parameter root is always open", v)
	}
}

func TestValidateValueTypeMismatches(t *testing.T) {
	cases := []struct {
		kind  ValueKind
		value any
	}{
		{KindString, 5.0},
		{KindNumber, "x"},
		{KindBoolean, "x"},
		{KindNull, "x"},
		{KindArray, "x"},
		{KindObject, "x"},
	}
	for _, c := range cases {
		v := validateValue(ValueSchema{Kind: c.kind}, c.value, "p")
		if len(v) != 1 {
			t.Errorf("kind=%s value=%v: violations = %v, want exactly 1", c.kind, c.value, v)
		}
	}
}

func TestValidateValueIntegerRejectsFraction(t *testing.T) {
	if v := validateValue(ValueSchema{Kind: KindInteger}, 3.5, "p"); len(v) != 1 {
		t.Fatalf("violations = %v, want a fractional-integer violation", v)
	}
	if v := validateValue(ValueSchema{Kind: KindInteger}, 3.0, "p"); len(v) != 0 {
		t.Fatalf("violations = %v, want none for a whole number", v)
	}
}

func TestValidateValueEnum(t *testing.T) {
	spec := ValueSchema{Kind: KindString, Enum: []any{"a", "b"}}
	if v := validateValue(spec, "a", "p"); len(v) != 0 {
		t.Fatalf("violations = %v, want none", v)
	}
	if v := validateValue(spec, "c", "p"); len(v) != 1 {
		t.Fatalf("violations = %v, want one enum violation", v)
	}
}

func TestValidateValueConst(t *testing.T) {
	spec := ValueSchema{Kind: KindString, Const: "fixed"}
	if v := validateValue(spec, "fixed", "p"); len(v) != 0 {
		t.Fatalf("violations = %v, want none", v)
	}
	if v := validateValue(spec, "other", "p"); len(v) != 1 {
		t.Fatalf("violations = %v, want one const violation", v)
	}
}

func TestValidateValueJSONKindAcceptsAnything(t *testing.T) {
	spec := ValueSchema{Kind: KindJSON}
	for _, v := range []any{"x", 5.0, nil, []any{1, 2}, map[string]any{"a": 1}} {
		if got := validateValue(spec, v, "p"); len(got) != 0 {
			t.Fatalf("json kind rejected %v: %v", v, got)
		}
	}
}

func TestValidateValueOneOfMatchesAnyBranch(t *testing.T) {
	spec := ValueSchema{Kind: KindOneOf, OneOf: []ValueSchema{{Kind: KindString}, {Kind: KindNumber}}}
	if v := validateValue(spec, "x", "p"); len(v) != 0 {
		t.Fatalf("violations = %v, want none for the string branch", v)
	}
	if v := validateValue(spec, 5.0, "p"); len(v) != 0 {
		t.Fatalf("violations = %v, want none for the number branch", v)
	}
	if v := validateValue(spec, true, "p"); len(v) != 1 {
		t.Fatalf("violations = %v, want one violation — matches neither branch", v)
	}
}

func TestValidateValueArrayItems(t *testing.T) {
	spec := ValueSchema{Kind: KindArray, Items: &ValueSchema{Kind: KindString}}
	if v := validateValue(spec, []any{"a", "b"}, "p"); len(v) != 0 {
		t.Fatalf("violations = %v, want none", v)
	}
	v := validateValue(spec, []any{"a", 5.0}, "p")
	if len(v) != 1 || v[0] != "p[1]: expected string, got number" {
		t.Fatalf("violations = %v", v)
	}
}

func TestValidateValueObjectNestedAndAdditionalProperties(t *testing.T) {
	spec := ValueSchema{
		Kind: KindObject,
		Properties: ParameterSchema{
			"name": {ValueSchema: ValueSchema{Kind: KindString}, Required: true},
		},
		AdditionalProperties: false,
	}
	if v := validateValue(spec, map[string]any{"name": "x"}, "p"); len(v) != 0 {
		t.Fatalf("violations = %v, want none", v)
	}
	v := validateValue(spec, map[string]any{}, "p")
	if len(v) != 1 || v[0] != "p.name is required" {
		t.Fatalf("violations = %v", v)
	}
	v = validateValue(spec, map[string]any{"name": "x", "extra": 1.0}, "p")
	if len(v) != 1 || v[0] != `p: unexpected property "extra"` {
		t.Fatalf("violations = %v", v)
	}
}

func TestValidateValueObjectAdditionalPropertiesAllowed(t *testing.T) {
	spec := ValueSchema{Kind: KindObject, AdditionalProperties: true}
	if v := validateValue(spec, map[string]any{"anything": 1.0}, "p"); len(v) != 0 {
		t.Fatalf("violations = %v, want none — additionalProperties: true", v)
	}
}

func TestNumericValueAcceptsCommonGoNumericTypes(t *testing.T) {
	for _, v := range []any{5.0, float32(5), 5, int32(5), int64(5)} {
		n, ok := numericValue(v)
		if !ok || n != 5 {
			t.Fatalf("numericValue(%T %v) = %v, %v", v, v, n, ok)
		}
	}
	if _, ok := numericValue("5"); ok {
		t.Fatal("numericValue(\"5\") should not accept a string")
	}
}

func TestValueSchemaToJSONSchemaScalar(t *testing.T) {
	spec := ValueSchema{Kind: KindString, Description: "a path", Enum: []any{"a", "b"}}
	got := spec.ToJSONSchema()
	want := map[string]any{"type": "string", "description": "a path", "enum": []any{"a", "b"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

func TestValueSchemaToJSONSchemaArray(t *testing.T) {
	spec := ValueSchema{Kind: KindArray, Items: &ValueSchema{Kind: KindNumber}}
	got := spec.ToJSONSchema()
	want := map[string]any{"type": "array", "items": map[string]any{"type": "number"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

func TestValueSchemaToJSONSchemaObjectWithRequired(t *testing.T) {
	spec := ValueSchema{
		Kind: KindObject,
		Properties: ParameterSchema{
			"a": {ValueSchema: ValueSchema{Kind: KindString}, Required: true},
			"b": {ValueSchema: ValueSchema{Kind: KindNumber}},
		},
		AdditionalProperties: false,
	}
	got := spec.ToJSONSchema()
	want := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"a": map[string]any{"type": "string"},
			"b": map[string]any{"type": "number"},
		},
		"required":             []string{"a"},
		"additionalProperties": false,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

func TestValueSchemaToJSONSchemaOneOf(t *testing.T) {
	spec := ValueSchema{Kind: KindOneOf, OneOf: []ValueSchema{{Kind: KindString}, {Kind: KindNumber}}}
	got := spec.ToJSONSchema()
	want := map[string]any{"oneOf": []any{
		map[string]any{"type": "string"},
		map[string]any{"type": "number"},
	}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

func TestValueSchemaToJSONSchemaJSONKind(t *testing.T) {
	spec := ValueSchema{Kind: KindJSON, Title: "anything"}
	got := spec.ToJSONSchema()
	want := map[string]any{"title": "anything"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v (no `type` — the escape hatch is unconstrained)", got, want)
	}
}

func TestParameterSchemaToJSONSchema(t *testing.T) {
	s := ParameterSchema{
		"path": {ValueSchema: ValueSchema{Kind: KindString}, Required: true},
		"n":    {ValueSchema: ValueSchema{Kind: KindNumber}},
	}
	got := s.ToJSONSchema()
	want := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{"type": "string"},
			"n":    map[string]any{"type": "number"},
		},
		"required": []string{"path"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}
