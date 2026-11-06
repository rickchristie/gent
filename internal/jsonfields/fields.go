// Package jsonfields matches Go values to JSON fields for capture, schemas, and input validation.
package jsonfields

import (
	"reflect"
	"slices"
	"strings"
	"unicode"
)

// Field identifies one visible JSON property and its path through embedded Go fields.
type Field struct {
	Name   string
	Type   reflect.Type
	depth  int
	tagged bool
	Quoted bool
	Index  []int
	order  int
}

// Fields follows encoding/json's field selection rules. Go's VisibleFields compares Go names,
// so it can hide valid JSON tags or select an ambiguous JSON name from embedded structs.
func Fields(typ reflect.Type) []Field {
	var fields []Field
	var visit func(reflect.Type, []int)
	active := map[reflect.Type]bool{}
	visit = func(typ reflect.Type, path []int) {
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
			if !validName(name) {
				name = ""
			}
			if name == "" && field.Anonymous && inner.Kind() == reflect.Struct {
				visit(inner, appendPath(path, i))
				continue
			}
			tagged := name != ""
			if name == "" {
				name = field.Name
			}
			quoted := slices.Contains(tag[1:], "string") && QuotedKind(inner.Kind())
			fields = append(fields, Field{Name: name, Type: field.Type, depth: len(path),
				tagged: tagged, Quoted: quoted, Index: appendPath(path, i), order: len(fields)})
		}
	}
	visit(typ, nil)
	// The least depth wins. At equal depth, one tagged field wins over untagged fields.
	slices.SortStableFunc(fields, func(a, b Field) int {
		if order := strings.Compare(a.Name, b.Name); order != 0 {
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
		for end < len(fields) && fields[end].Name == fields[i].Name {
			end++
		}
		ambiguous := end > i+1 && fields[i].depth == fields[i+1].depth &&
			fields[i].tagged == fields[i+1].tagged
		if !ambiguous {
			selected = append(selected, fields[i])
		}
		i = end
	}
	slices.SortFunc(selected, func(a, b Field) int { return a.order - b.order })
	return selected
}

func validName(name string) bool {
	for _, char := range name {
		if unicode.IsLetter(char) || unicode.IsDigit(char) ||
			strings.ContainsRune("!#$%&()*+-./:;<=>?@[]^_{|}~ ", char) {
			continue
		}
		return false
	}
	return name != ""
}

// QuotedKind reports whether encoding/json applies the string option to this kind.
func QuotedKind(kind reflect.Kind) bool {
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

// Each selected path is retained after traversal, so it must not share the traversal buffer.
func appendPath(path []int, index int) []int {
	result := make([]int, len(path)+1)
	copy(result, path)
	result[len(path)] = index
	return result
}
