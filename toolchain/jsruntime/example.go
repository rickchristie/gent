package jsruntime

import (
	"bytes"
	"encoding/json"
	"github.com/rickchristie/gent/schema"
	"strings"
)

// WriteExampleCall appends a tool.call() example to sb
// using the schema's ExampleObject. Writes nothing if the
// schema has no properties.
func WriteExampleCall(
	sb *strings.Builder,
	toolName string,
	sch *schema.Schema,
) {
	example := sch.ExampleObject()
	if example == nil {
		return
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("    ", "  ")
	if err := enc.Encode(example); err != nil {
		return
	}
	argsJSON := bytes.TrimRight(
		buf.Bytes(), "\n",
	)
	sb.WriteString("\nExample:\n")
	sb.WriteString("  tool.call({\n")
	sb.WriteString("    tool: \"")
	sb.WriteString(toolName)
	sb.WriteString("\",\n")
	sb.WriteString("    args: ")
	sb.Write(argsJSON)
	sb.WriteString("\n  });\n")
}
