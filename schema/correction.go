package schema

import (
	"errors"
	"fmt"
	"reflect"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/santhosh-tekuri/jsonschema/v6/kind"
)

// SafeValidationDetails uses field paths and declared constraints, never raw error strings.
// Custom format errors can contain private values; only the declared format name is included.
func SafeValidationDetails(err error) string {
	var validation *jsonschema.ValidationError
	if !errors.As(err, &validation) {
		return "args: expected an object that satisfies the schema."
	}
	var lines []string
	var visit func(*jsonschema.ValidationError)
	visit = func(e *jsonschema.ValidationError) {
		if len(e.Causes) > 0 {
			for _, cause := range e.Causes {
				visit(cause)
			}
			return
		}
		path := "args"
		for _, segment := range e.InstanceLocation {
			if isDigit(segment) {
				path += "[" + segment + "]"
			} else {
				path += "." + segment
			}
		}
		switch constraint := e.ErrorKind.(type) {
		case *kind.Required:
			for _, name := range constraint.Missing {
				lines = append(lines, path+"."+name+": required field is missing.")
			}
		case *kind.AdditionalProperties:
			for _, name := range constraint.Properties {
				lines = append(lines, path+"."+name+": additional field is not permitted.")
			}
		case *kind.Type:
			lines = append(lines, path+": expected "+strings.Join(constraint.Want, " or ")+".")
		default:
			keyword := strings.Join(e.ErrorKind.KeywordPath(), "/")
			if keyword == "" {
				lines = append(lines, path+": expected a JSON value compatible with the schema.")
				return
			}
			message := path + ": must satisfy " + keyword
			// Want is declared schema data. Never read Got, Err, or error formatting methods.
			value := reflect.ValueOf(e.ErrorKind)
			if value.Kind() == reflect.Pointer && value.Elem().Kind() == reflect.Struct {
				if want := value.Elem().FieldByName("Want"); want.IsValid() && want.CanInterface() {
					message += fmt.Sprintf(" (%v)", want.Interface())
				}
			}
			lines = append(lines, message+".")
		}
	}
	visit(validation)
	return strings.Join(lines, "\n")
}
