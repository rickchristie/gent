package schema

import (
	"encoding"
	"encoding/json"
	"fmt"
	"math/big"
	"reflect"
	"slices"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

type outputTarget struct {
	typ    reflect.Type
	quoted bool
}

type outputSchemaPair struct {
	schema *jsonschema.Schema
	target outputTarget
}

type outputTypeCheck struct {
	types   map[reflect.Type]bool
	fields  map[reflect.Type][]outputField
	schemas map[outputSchemaPair]bool
}

// CheckOutputType rejects unsupported encoding/json types and incompatible declared shapes.
// Use the compiled schema graph so references and draft-specific array rules have the same
// meaning as validation. This is a setup check, not a proof that every constraint is satisfiable.
// Value ranges, custom JSON methods, and other value-dependent checks still run on each answer.
func (s *Schema) CheckOutputType(typ reflect.Type) error {
	check := &outputTypeCheck{types: map[reflect.Type]bool{},
		fields: map[reflect.Type][]outputField{}, schemas: map[outputSchemaPair]bool{}}
	if err := check.goType(typ); err != nil {
		return err
	}
	return check.schema(s.compiled, outputTarget{typ: typ})
}

func (check *outputTypeCheck) goType(typ reflect.Type) error {
	if check.types[typ] {
		return nil
	}
	check.types[typ] = true
	if typ.Kind() == reflect.Interface {
		if typ.NumMethod() == 0 {
			return nil
		}
		return fmt.Errorf("unsupported JSON result type %s", typ)
	}
	if outputJSONDecoder(typ) || outputTextDecoder(typ) {
		return nil
	}
	switch typ.Kind() {
	case reflect.Pointer, reflect.Array, reflect.Slice:
		return check.goType(typ.Elem())
	case reflect.Map:
		if !outputMapKey(typ.Key()) {
			return fmt.Errorf("unsupported JSON map key type %s in %s", typ.Key(), typ)
		}
		return check.goType(typ.Elem())
	case reflect.Struct:
		fields := outputFields(typ)
		check.fields[typ] = fields
		for _, field := range fields {
			if err := check.goType(field.typ); err != nil {
				return fmt.Errorf("field %q: %w", field.name, err)
			}
		}
		return nil
	default:
		if quotedJSONKind(typ.Kind()) {
			return nil
		}
	}
	return fmt.Errorf("unsupported JSON result type %s", typ)
}

func outputMapKey(typ reflect.Type) bool {
	switch typ.Kind() {
	case reflect.String, reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return true
	}
	// A custom key needs both directions. encoding/json does not take the address of map keys
	// when encoding, but it uses a pointer to a new key when decoding.
	return reflect.PointerTo(typ).Implements(reflect.TypeFor[encoding.TextUnmarshaler]()) &&
		typ.Implements(reflect.TypeFor[encoding.TextMarshaler]())
}

func outputJSONDecoder(typ reflect.Type) bool {
	return typ.Implements(reflect.TypeFor[json.Unmarshaler]()) ||
		reflect.PointerTo(typ).Implements(reflect.TypeFor[json.Unmarshaler]())
}

func outputTextDecoder(typ reflect.Type) bool {
	return typ.Implements(reflect.TypeFor[encoding.TextUnmarshaler]()) ||
		reflect.PointerTo(typ).Implements(reflect.TypeFor[encoding.TextUnmarshaler]())
}

func (check *outputTypeCheck) schema(node *jsonschema.Schema, target outputTarget) error {
	if node == nil {
		return nil
	}
	pair := outputSchemaPair{node, target}
	if check.schemas[pair] {
		return nil
	}
	check.schemas[pair] = true
	typ := target.typ
	pointers := map[reflect.Type]bool{}
	for typ.Kind() == reflect.Pointer {
		if pointers[typ] {
			return fmt.Errorf("unsupported recursive JSON pointer type %s", typ)
		}
		pointers[typ] = true
		typ = typ.Elem()
	}
	if outputJSONDecoder(typ) || (typ.Kind() == reflect.Interface && typ.NumMethod() == 0) {
		return nil
	}
	for _, kind := range outputSchemaTypes(node) {
		if kind == "null" {
			if outputNullable(target.typ) {
				continue
			}
			return fmt.Errorf("JSON null cannot be stored as %s", target.typ)
		}
		expected := outputJSONKind(typ, target.quoted)
		if kind != expected && !(kind == "integer" && expected == "number") {
			return fmt.Errorf("JSON schema type %s cannot be stored as %s", kind, target.typ)
		}
	}
	if err := check.relatedSchemas(node, target); err != nil {
		return err
	}
	if outputTextDecoder(typ) {
		return nil
	}
	switch typ.Kind() {
	case reflect.Struct, reflect.Map:
		return check.object(node, typ)
	case reflect.Slice, reflect.Array:
		if outputJSONKind(typ, false) == "array" {
			return check.array(node, typ.Elem())
		}
	}
	return nil
}

func (check *outputTypeCheck) relatedSchemas(node *jsonschema.Schema, target outputTarget) error {
	refs := []*jsonschema.Schema{node.Ref, node.RecursiveRef}
	if node.DynamicRef != nil {
		refs = append(refs, node.DynamicRef.Ref)
	}
	for _, nodes := range [][]*jsonschema.Schema{refs, node.AllOf, node.AnyOf, node.OneOf} {
		for _, child := range nodes {
			if err := check.schema(child, target); err != nil {
				return err
			}
		}
	}
	return nil
}

func outputNullable(typ reflect.Type) bool {
	switch typ.Kind() {
	case reflect.Pointer, reflect.Map, reflect.Slice, reflect.Interface:
		return true
	default:
		return false
	}
}

func outputJSONKind(typ reflect.Type, quoted bool) string {
	if quoted || outputTextDecoder(typ) {
		return "string"
	}
	if typ == reflect.TypeFor[json.Number]() {
		return "number"
	}
	switch typ.Kind() {
	case reflect.String:
		return "string"
	case reflect.Bool:
		return "boolean"
	case reflect.Float32, reflect.Float64:
		return "number"
	case reflect.Struct, reflect.Map:
		return "object"
	case reflect.Slice:
		if typ.Elem().Kind() == reflect.Uint8 {
			item := reflect.PointerTo(typ.Elem())
			if !item.Implements(reflect.TypeFor[json.Marshaler]()) &&
				!item.Implements(reflect.TypeFor[encoding.TextMarshaler]()) {
				return "string"
			}
		}
		return "array"
	case reflect.Array:
		return "array"
	default:
		return "integer"
	}
}

func outputSchemaTypes(node *jsonschema.Schema) []string {
	if node.Types != nil {
		return node.Types.ToStrings()
	}
	var values []any
	if node.Const != nil {
		values = []any{*node.Const}
	} else if node.Enum != nil {
		values = node.Enum.Values
	}
	var kinds []string
	for _, value := range values {
		kind := "null"
		switch value := value.(type) {
		case string:
			kind = "string"
		case bool:
			kind = "boolean"
		case map[string]any:
			kind = "object"
		case []any:
			kind = "array"
		case json.Number:
			kind = "number"
			if number, ok := new(big.Rat).SetString(value.String()); ok && number.IsInt() {
				kind = "integer"
			}
		}
		if !slices.Contains(kinds, kind) {
			kinds = append(kinds, kind)
		}
	}
	return kinds
}

func (check *outputTypeCheck) object(node *jsonschema.Schema, typ reflect.Type) error {
	names := make([]string, 0, len(node.Properties))
	for name := range node.Properties {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		target, err := check.property(typ, name)
		if err != nil {
			return err
		}
		if err := check.schema(node.Properties[name], target); err != nil {
			return fmt.Errorf("field %q: %w", name, err)
		}
	}
	for _, name := range node.Required {
		if _, err := check.property(typ, name); err != nil {
			return err
		}
	}
	return check.otherProperties(node, typ)
}

func (check *outputTypeCheck) otherProperties(node *jsonschema.Schema, typ reflect.Type) error {
	patterns := make([]jsonschema.Regexp, 0, len(node.PatternProperties))
	for pattern := range node.PatternProperties {
		patterns = append(patterns, pattern)
	}
	slices.SortFunc(patterns, func(a, b jsonschema.Regexp) int {
		return strings.Compare(a.String(), b.String())
	})
	additional, _ := node.AdditionalProperties.(*jsonschema.Schema)
	if typ.Kind() == reflect.Map {
		children := []*jsonschema.Schema{additional, node.UnevaluatedProperties}
		for _, pattern := range patterns {
			children = append(children, node.PatternProperties[pattern])
		}
		for _, child := range children {
			if err := check.schema(child, outputTarget{typ: typ.Elem()}); err != nil {
				return fmt.Errorf("map value: %w", err)
			}
		}
		return nil
	}
	for _, field := range check.fields[typ] {
		target := outputTarget{field.typ, field.quoted}
		matched := node.Properties[field.name] != nil
		for _, pattern := range patterns {
			if !pattern.MatchString(field.name) {
				continue
			}
			matched = true
			if err := check.schema(node.PatternProperties[pattern], target); err != nil {
				return fmt.Errorf("field %q: %w", field.name, err)
			}
		}
		if !matched {
			if err := check.schema(additional, target); err != nil {
				return fmt.Errorf("field %q: %w", field.name, err)
			}
		}
	}
	return nil
}

func (check *outputTypeCheck) property(typ reflect.Type, name string) (outputTarget, error) {
	if typ.Kind() == reflect.Map {
		return outputTarget{typ: typ.Elem()}, nil
	}
	fields := check.fields[typ]
	for _, field := range fields {
		if field.name == name {
			return outputTarget{field.typ, field.quoted}, nil
		}
	}
	// Output is encoded back from T. A decode-only case alias cannot satisfy the exact JSON
	// field names in the declared contract when that encoded value is validated again.
	return outputTarget{}, fmt.Errorf("JSON field %q has no destination in %s", name, typ)
}

func (check *outputTypeCheck) array(node *jsonschema.Schema, typ reflect.Type) error {
	items := append([]*jsonschema.Schema{}, node.PrefixItems...)
	items = append(items, node.Items2020, node.UnevaluatedItems)
	if node.MinContains == nil || *node.MinContains > 0 {
		items = append(items, node.Contains)
	}
	switch child := node.Items.(type) {
	case *jsonschema.Schema:
		items = append(items, child)
	case []*jsonschema.Schema:
		items = append(items, child...)
	}
	if child, ok := node.AdditionalItems.(*jsonschema.Schema); ok {
		items = append(items, child)
	}
	for _, child := range items {
		if err := check.schema(child, outputTarget{typ: typ}); err != nil {
			return fmt.Errorf("array element: %w", err)
		}
	}
	return nil
}
