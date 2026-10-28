package schema

import (
	"fmt"
	"strings"
)

// FieldBuilder adds documentation and constraints to a field inferred from a Go type.
// Use JSON names. A dot selects a nested object field, such as "address.city".
type FieldBuilder struct {
	path     string
	optional bool
	values   map[string]any
}

func Field(name string) *FieldBuilder {
	return &FieldBuilder{path: name, values: map[string]any{}}
}

func (f *FieldBuilder) Description(value string) *FieldBuilder {
	f.values["description"] = value
	return f
}

// Optional permits an absent field. It does not add null or insert a default value.
func (f *FieldBuilder) Optional() *FieldBuilder { f.optional = true; return f }

func (f *FieldBuilder) Enum(values ...any) *FieldBuilder {
	f.values["enum"] = values
	return f
}

func (f *FieldBuilder) Format(value string) *FieldBuilder {
	f.values["format"] = value
	return f
}

func (f *FieldBuilder) Min(value float64) *FieldBuilder {
	f.values["minimum"] = value
	return f
}

func (f *FieldBuilder) Max(value float64) *FieldBuilder {
	f.values["maximum"] = value
	return f
}

func (f *FieldBuilder) MinLength(value int) *FieldBuilder {
	f.values["minLength"] = value
	return f
}

func (f *FieldBuilder) MaxLength(value int) *FieldBuilder {
	f.values["maxLength"] = value
	return f
}

func (f *FieldBuilder) Pattern(value string) *FieldBuilder {
	f.values["pattern"] = value
	return f
}

func (f *FieldBuilder) MinItems(value int) *FieldBuilder {
	f.values["minItems"] = value
	return f
}

func (f *FieldBuilder) MaxItems(value int) *FieldBuilder {
	f.values["maxItems"] = value
	return f
}

// ApplyFields modifies an owned inferred schema. Unknown paths and invalid constraints fail
// at build time, because JSON Schema would otherwise ignore constraints of the wrong type.
func ApplyFields(raw map[string]any, fields []*FieldBuilder) error {
	seen := map[string]bool{}
	for _, field := range fields {
		if seen[field.path] {
			return fmt.Errorf("duplicate field %q", field.path)
		}
		seen[field.path] = true
		parent := raw
		parts := strings.Split(field.path, ".")
		for i, name := range parts {
			parent = nonNull(parent)
			properties, _ := parent["properties"].(map[string]any)
			value, ok := properties[name].(map[string]any)
			if !ok {
				return fmt.Errorf("unknown field %q", field.path)
			}
			if i < len(parts)-1 {
				parent = value
				continue
			}
			if err := applyField(nonNull(value), field); err != nil {
				return fmt.Errorf("field %q: %w", field.path, err)
			}
			if field.optional {
				required, _ := parent["required"].([]string)
				kept := []string{}
				for _, item := range required {
					if item != name {
						kept = append(kept, item)
					}
				}
				parent["required"] = kept
			}
		}
	}
	return nil
}

func applyField(value map[string]any, field *FieldBuilder) error {
	for _, key := range []string{"format", "minLength", "maxLength", "pattern"} {
		if _, found := field.values[key]; found && value["type"] != "string" {
			return fmt.Errorf("%s requires a string", key)
		}
	}
	for _, key := range []string{"minimum", "maximum"} {
		if _, found := field.values[key]; found && value["type"] != "integer" &&
			value["type"] != "number" {
			return fmt.Errorf("%s requires a number", key)
		}
	}
	for _, key := range []string{"minItems", "maxItems"} {
		if _, found := field.values[key]; found && value["type"] != "array" {
			return fmt.Errorf("%s requires an array", key)
		}
	}
	for key, constraint := range field.values {
		value[key] = constraint
	}
	for _, pair := range [][2]string{
		{"minimum", "maximum"}, {"minLength", "maxLength"}, {"minItems", "maxItems"},
	} {
		low, lowOK := number(value[pair[0]])
		high, highOK := number(value[pair[1]])
		if lowOK && highOK && low > high {
			return fmt.Errorf("%s exceeds %s", pair[0], pair[1])
		}
	}
	// Check enum values against the type and constraints before adding the enum itself.
	if values, ok := value["enum"].([]any); ok {
		values, err := copyJSON(values)
		if err != nil {
			return fmt.Errorf("invalid enum: %w", err)
		}
		delete(value, "enum")
		validator, err := Compile(value)
		if err != nil {
			return err
		}
		for _, item := range values {
			if err := validator.compiled.Validate(item); err != nil {
				return fmt.Errorf("enum value %v does not match the field schema", item)
			}
		}
		value["enum"] = values
	}
	return nil
}

func number(value any) (float64, bool) {
	switch n := value.(type) {
	case int:
		return float64(n), true
	case float64:
		return n, true
	default:
		return 0, false
	}
}
