package schema

import (
	"reflect"

	"github.com/rickchristie/gent/internal/jsonfields"
)

type outputField struct {
	name   string
	typ    reflect.Type
	quoted bool
}

// Share JSON field selection with DataPart so schema checks and captured data agree.
func outputFields(typ reflect.Type) []outputField {
	fields := jsonfields.Fields(typ)
	result := make([]outputField, len(fields))
	for i, field := range fields {
		result[i] = outputField{name: field.Name, typ: field.Type, quoted: field.Quoted}
	}
	return result
}

func quotedJSONKind(kind reflect.Kind) bool { return jsonfields.QuotedKind(kind) }
