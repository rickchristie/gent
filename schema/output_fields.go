package schema

import (
	"reflect"
	"slices"
	"strings"
	"unicode"
)

type outputField struct {
	name   string
	typ    reflect.Type
	depth  int
	tagged bool
	quoted bool
	order  int
}

// Follow encoding/json's field selection rules. Go's VisibleFields instead compares Go names,
// so it can hide valid JSON tags or select an ambiguous JSON name from embedded structs.
func outputFields(typ reflect.Type) []outputField {
	var fields []outputField
	var visit func(reflect.Type, int)
	active := map[reflect.Type]bool{}
	visit = func(typ reflect.Type, depth int) {
		if active[typ] {
			return
		}
		active[typ] = true
		defer delete(active, typ)
		for i := range typ.NumField() {
			field := typ.Field(i)
			inner := field.Type
			if inner.Name() == "" && inner.Kind() == reflect.Pointer {
				inner = inner.Elem()
			}
			if !field.IsExported() && (!field.Anonymous || inner.Kind() != reflect.Struct) {
				continue
			}
			rawTag := field.Tag.Get("json")
			if rawTag == "-" {
				continue
			}
			tag := strings.Split(rawTag, ",")
			name := tag[0]
			if !validOutputFieldName(name) {
				name = ""
			}
			if name == "" && field.Anonymous && inner.Kind() == reflect.Struct {
				visit(inner, depth+1)
				continue
			}
			tagged := name != ""
			if name == "" {
				name = field.Name
			}
			quoted := slices.Contains(tag[1:], "string") && quotedJSONKind(inner.Kind())
			fields = append(fields, outputField{name: name, typ: field.Type, depth: depth,
				tagged: tagged, quoted: quoted, order: len(fields)})
		}
	}
	visit(typ, 0)
	// The least depth wins. At equal depth, one tagged field wins over untagged fields.
	slices.SortStableFunc(fields, func(a, b outputField) int {
		if order := strings.Compare(a.name, b.name); order != 0 {
			return order
		}
		if a.depth != b.depth {
			return a.depth - b.depth
		}
		if a.tagged == b.tagged {
			return 0
		}
		if a.tagged {
			return -1
		}
		return 1
	})
	selected := fields[:0]
	for i := 0; i < len(fields); {
		end := i + 1
		for end < len(fields) && fields[end].name == fields[i].name {
			end++
		}
		ambiguous := end > i+1 && fields[i].depth == fields[i+1].depth &&
			fields[i].tagged == fields[i+1].tagged
		if !ambiguous {
			selected = append(selected, fields[i])
		}
		i = end
	}
	slices.SortFunc(selected, func(a, b outputField) int { return a.order - b.order })
	return selected
}

func validOutputFieldName(name string) bool {
	for _, char := range name {
		if unicode.IsLetter(char) || unicode.IsDigit(char) ||
			strings.ContainsRune("!#$%&()*+-./:;<=>?@[]^_{|}~ ", char) {
			continue
		}
		return false
	}
	return name != ""
}

func quotedJSONKind(kind reflect.Kind) bool {
	switch kind {
	case reflect.Bool, reflect.String,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr,
		reflect.Float32, reflect.Float64:
		return true
	default:
		return false
	}
}
