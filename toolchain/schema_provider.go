package toolchain

import "github.com/rickchristie/gent/schema"

// SchemaProvider allows access to tool schemas by name.
// Implemented by toolchains that support schema
// validation.
type SchemaProvider interface {
	GetToolSchema(name string) *schema.Schema
}
