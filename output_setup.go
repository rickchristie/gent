package gent

import (
	"fmt"
	"reflect"
)

// PrepareOutput binds the result codec and output contract before any model call. Custom types
// with private state require a registered ValueCodec. Configuration never calls Process.
func PrepareOutput[T any](ctx *ExecutionContext, output Output[T]) error {
	if err := ctx.Codecs().RegisterValueType(reflect.TypeFor[T]()); err != nil {
		return err
	}
	request := NewModelCallRequest(nil)
	if err := output.Configure(&request); err != nil {
		return err
	}
	controls, err := captureOutputControls(request.Options)
	if err != nil {
		return err
	}
	typeName, err := ctx.Codecs().typeName(reflect.TypeFor[T]())
	if err != nil {
		return err
	}
	configuration := map[string]any{"type": fmt.Sprintf("%T", output),
		"valueType": typeName, "controls": string(controls),
		"valueContract": outputTypeContract(ctx.Codecs(), reflect.TypeFor[T](), map[reflect.Type]bool{})}
	if configured, ok := output.(RecoveryConfig); ok {
		configuration["settings"] = configured.RecoveryConfig()
	}
	fingerprint, err := RecoveryFingerprint(configuration)
	if err != nil {
		return err
	}
	if ctx.Parent() != nil {
		return nil
	}
	manifest := ctx.Compatibility()
	manifest.Version = 1
	if manifest.Components == nil {
		manifest.Components = map[string]string{}
	}
	manifest.Components["gent:output"] = fingerprint
	return ctx.SetCompatibility(manifest)
}

// A named Go type keeps its name when fields change. Include its field shape and codec version
// so a completed run cannot silently bind to a different representation after an application
// update.
func outputTypeContract(registry *CodecRegistry, typ reflect.Type, seen map[reflect.Type]bool) any {
	identity := typ.PkgPath() + "/" + typ.String()
	if seen[typ] {
		return identity
	}
	seen[typ] = true
	result := map[string]any{"goType": identity}
	if registered, ok := registry.lookupType(typ); ok {
		result["codec"], result["version"] = registered.id, registered.version
		if registered.codec != nil {
			return result
		}
	}
	switch typ.Kind() {
	case reflect.Pointer, reflect.Slice, reflect.Array:
		if typ.Kind() == reflect.Array {
			result["length"] = typ.Len()
		}
		result["element"] = outputTypeContract(registry, typ.Elem(), seen)
	case reflect.Map:
		result["key"] = outputTypeContract(registry, typ.Key(), seen)
		result["element"] = outputTypeContract(registry, typ.Elem(), seen)
	case reflect.Struct:
		fields := make([]any, typ.NumField())
		for i := range fields {
			field := typ.Field(i)
			fields[i] = map[string]any{"name": field.Name, "tag": string(field.Tag),
				"type": outputTypeContract(registry, field.Type, seen)}
		}
		result["fields"] = fields
	}
	return result
}
