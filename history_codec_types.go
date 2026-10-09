package gent

import (
	"fmt"
	"reflect"
)

// RegisterValueType registers a declared tool input/output type and its nested value types.
// It never registers an arbitrary value found while encoding: interface contents still need
// explicit application registration. Existing custom codecs take precedence.
func (r *CodecRegistry) RegisterValueType(typ reflect.Type) error {
	return r.registerValueType(typ, map[reflect.Type]bool{})
}

func (r *CodecRegistry) registerValueType(typ reflect.Type, seen map[reflect.Type]bool) error {
	if typ == nil || seen[typ] {
		return nil
	}
	seen[typ] = true
	if _, ok := r.lookupType(typ); ok {
		return nil
	}
	switch typ.Kind() {
	case reflect.Pointer, reflect.Slice, reflect.Array:
		if err := r.registerValueType(typ.Elem(), seen); err != nil {
			return err
		}
		if typ.Name() == "" {
			return nil
		}
	case reflect.Map:
		if err := r.registerValueType(typ.Key(), seen); err != nil {
			return err
		}
		if err := r.registerValueType(typ.Elem(), seen); err != nil {
			return err
		}
		if typ.Name() == "" {
			return nil
		}
	case reflect.Interface:
		return nil
	case reflect.Func, reflect.Chan, reflect.UnsafePointer, reflect.Complex64, reflect.Complex128:
		return fmt.Errorf("gent: unsupported declared value type %s", typ)
	}
	id := typ.PkgPath() + "/" + typ.Name()
	if typ.Name() == "" {
		id = "go/" + typ.String()
	}
	if typ.Kind() == reflect.Struct {
		for i := 0; i < typ.NumField(); i++ {
			field := typ.Field(i)
			if field.PkgPath != "" {
				return fmt.Errorf("gent: unexported field %s.%s requires a codec", typ, field.Name)
			}
			if err := r.registerValueType(field.Type, seen); err != nil {
				return err
			}
		}
	}
	// Validate before publishing registration, so an unsupported child field cannot leave the
	// parent apparently registered on the next attempt.
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.byType[typ]; !ok {
		if old, exists := r.byId[id]; exists && old.typ != typ {
			return fmt.Errorf("gent: conflicting declared type %s", id)
		}
		r.add(registeredValue{id: id, version: 1, typ: typ})
	}
	return nil
}
