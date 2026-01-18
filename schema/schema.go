// Package schema provides JSON Schema building and validation utilities.
//
// # Quick Start
//
//	tool := gent.NewToolFunc(
//	    "search",
//	    "Search for information",
//	    schema.Object(map[string]*schema.Property{
//	        "query": schema.String("Search query"),
//	        "limit": schema.Integer("Max results").Min(1).Max(100).Default(10),
//	    }, "query"), // "query" is required
//	    searchFunc,
//	    nil,
//	)
//
// ToolChain automatically validates inputs against the schema before execution.
// See [Object], [Property], and individual builder functions for detailed documentation.
package schema

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

// Schema represents a JSON Schema definition.
// It provides both the raw map representation (for serialization/prompts)
// and a compiled validator (for runtime validation).
type Schema struct {
	raw      map[string]any
	compiled *jsonschema.Schema
}

// Raw returns the underlying map[string]any representation.
// This is useful for serialization and passing to LLMs.
func (s *Schema) Raw() map[string]any {
	if s == nil {
		return nil
	}
	return s.raw
}

// Validate validates the given data against the schema.
// Returns nil if valid, or an error describing the validation failure.
func (s *Schema) Validate(data map[string]any) error {
	if s == nil || s.compiled == nil {
		return nil
	}
	err := s.compiled.Validate(data)
	if err != nil {
		return &ValidationError{Err: err}
	}
	return nil
}

// ValidationError wraps a JSON Schema validation error with a cleaner message.
type ValidationError struct {
	Err error
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("schema validation failed: %v", e.Err)
}

func (e *ValidationError) Unwrap() error {
	return e.Err
}

// Compile compiles a raw schema map into a Schema with a compiled validator.
// Returns an error if the schema is invalid.
func Compile(raw map[string]any) (*Schema, error) {
	if raw == nil {
		return nil, nil
	}

	// Marshal the schema to JSON for the compiler
	schemaJSON, err := json.Marshal(raw)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal schema: %w", err)
	}

	// Unmarshal into the format expected by jsonschema
	schemaData, err := jsonschema.UnmarshalJSON(strings.NewReader(string(schemaJSON)))
	if err != nil {
		return nil, fmt.Errorf("failed to parse schema: %w", err)
	}

	// Compile the schema
	c := jsonschema.NewCompiler()
	if err := c.AddResource("schema.json", schemaData); err != nil {
		return nil, fmt.Errorf("failed to add schema resource: %w", err)
	}

	compiled, err := c.Compile("schema.json")
	if err != nil {
		return nil, fmt.Errorf("failed to compile schema: %w", err)
	}

	return &Schema{
		raw:      raw,
		compiled: compiled,
	}, nil
}

// MustCompile is like Compile but panics on error.
// Use this for schemas defined at init time.
func MustCompile(raw map[string]any) *Schema {
	s, err := Compile(raw)
	if err != nil {
		panic(err)
	}
	return s
}

// -----------------------------------------------------------------------------
// Schema Builders
// -----------------------------------------------------------------------------

// Object creates an object schema with the given properties.
// Pass property names as variadic arguments to mark them as required.
//
// Example:
//
//	// All properties optional
//	schema.Object(map[string]*schema.Property{
//	    "name": schema.String("User name"),
//	    "age":  schema.Integer("User age"),
//	})
//
//	// "name" and "email" are required
//	schema.Object(map[string]*schema.Property{
//	    "name":  schema.String("User name"),
//	    "email": schema.String("Email address"),
//	    "phone": schema.String("Phone number"),
//	}, "name", "email")
func Object(properties map[string]*Property, required ...string) map[string]any {
	props := make(map[string]any, len(properties))
	for name, prop := range properties {
		props[name] = prop.build()
	}

	schema := map[string]any{
		"type":       "object",
		"properties": props,
	}

	if len(required) > 0 {
		schema["required"] = required
	}

	return schema
}

// Property represents a property in an object schema.
type Property struct {
	typ         string
	description string
	enum        []any
	format      string
	minimum     *float64
	maximum     *float64
	minLength   *int
	maxLength   *int
	pattern     string
	items       map[string]any
	properties  map[string]any
	required    []string
	def         any // default value
}

func (p *Property) build() map[string]any {
	m := map[string]any{}

	if p.typ != "" {
		m["type"] = p.typ
	}
	if p.description != "" {
		m["description"] = p.description
	}
	if len(p.enum) > 0 {
		m["enum"] = p.enum
	}
	if p.format != "" {
		m["format"] = p.format
	}
	if p.minimum != nil {
		m["minimum"] = *p.minimum
	}
	if p.maximum != nil {
		m["maximum"] = *p.maximum
	}
	if p.minLength != nil {
		m["minLength"] = *p.minLength
	}
	if p.maxLength != nil {
		m["maxLength"] = *p.maxLength
	}
	if p.pattern != "" {
		m["pattern"] = p.pattern
	}
	if p.items != nil {
		m["items"] = p.items
	}
	if p.properties != nil {
		m["properties"] = p.properties
	}
	if len(p.required) > 0 {
		m["required"] = p.required
	}
	if p.def != nil {
		m["default"] = p.def
	}

	return m
}

// String creates a string property.
//
// Example:
//
//	schema.String("User's full name")
//	schema.String("Email").Format("email")
//	schema.String("Username").MinLength(3).MaxLength(20)
//	schema.String("Status").Enum("active", "inactive")
func String(description string) *Property {
	return &Property{typ: "string", description: description}
}

// Integer creates an integer property.
//
// Example:
//
//	schema.Integer("User's age")
//	schema.Integer("Count").Min(0).Max(100)
//	schema.Integer("Priority").Enum(1, 2, 3).Default(2)
func Integer(description string) *Property {
	return &Property{typ: "integer", description: description}
}

// Number creates a number property (floating point).
//
// Example:
//
//	schema.Number("Price in dollars")
//	schema.Number("Temperature").Min(-273.15)
//	schema.Number("Percentage").Min(0).Max(100)
func Number(description string) *Property {
	return &Property{typ: "number", description: description}
}

// Boolean creates a boolean property.
//
// Example:
//
//	schema.Boolean("Whether to include metadata")
//	schema.Boolean("Enable notifications").Default(true)
func Boolean(description string) *Property {
	return &Property{typ: "boolean", description: description}
}

// Array creates an array property with the given item schema.
//
// Example:
//
//	// Array of strings
//	schema.Array("List of tags", map[string]any{"type": "string"})
//
//	// Array of integers
//	schema.Array("List of IDs", map[string]any{"type": "integer"})
//
//	// Array of objects
//	schema.Array("List of users", schema.Object(map[string]*schema.Property{
//	    "name": schema.String("User name"),
//	    "id":   schema.Integer("User ID"),
//	}))
func Array(description string, items map[string]any) *Property {
	return &Property{typ: "array", description: description, items: items}
}

// Enum sets allowed values for the property.
//
// Example:
//
//	schema.String("Status").Enum("pending", "active", "closed")
//	schema.Integer("Priority").Enum(1, 2, 3)
func (p *Property) Enum(values ...any) *Property {
	p.enum = values
	return p
}

// Format sets the format for string validation.
//
// Common formats: "email", "date-time", "date", "time", "uri", "uuid", "ipv4", "ipv6"
//
// Example:
//
//	schema.String("Email").Format("email")
//	schema.String("Website").Format("uri")
//	schema.String("Timestamp").Format("date-time")
func (p *Property) Format(format string) *Property {
	p.format = format
	return p
}

// Min sets the minimum value for number/integer properties.
//
// Example:
//
//	schema.Integer("Age").Min(0)
//	schema.Number("Price").Min(0.01)
func (p *Property) Min(min float64) *Property {
	p.minimum = &min
	return p
}

// Max sets the maximum value for number/integer properties.
//
// Example:
//
//	schema.Integer("Percentage").Max(100)
//	schema.Integer("Count").Min(1).Max(1000)
func (p *Property) Max(max float64) *Property {
	p.maximum = &max
	return p
}

// MinLength sets the minimum length for string properties.
//
// Example:
//
//	schema.String("Password").MinLength(8)
//	schema.String("Username").MinLength(3).MaxLength(20)
func (p *Property) MinLength(min int) *Property {
	p.minLength = &min
	return p
}

// MaxLength sets the maximum length for string properties.
//
// Example:
//
//	schema.String("Title").MaxLength(100)
//	schema.String("Bio").MaxLength(500)
func (p *Property) MaxLength(max int) *Property {
	p.maxLength = &max
	return p
}

// Pattern sets a regex pattern for string validation.
//
// Example:
//
//	schema.String("Phone").Pattern(`^\+?[0-9]{10,14}$`)
//	schema.String("Booking ID").Pattern(`^[A-Z]{2}[0-9]{4}$`)
func (p *Property) Pattern(pattern string) *Property {
	p.pattern = pattern
	return p
}

// Default sets the default value for the property.
//
// Example:
//
//	schema.Integer("Limit").Default(10)
//	schema.String("Sort").Enum("asc", "desc").Default("asc")
//	schema.Boolean("Verbose").Default(false)
func (p *Property) Default(value any) *Property {
	p.def = value
	return p
}
