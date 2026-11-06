package toolchain

import (
	"fmt"

	"github.com/rickchristie/gent"
	"github.com/rickchristie/gent/schema"
)

// Registry binds Tool[I] implementations to native calls. Configure it before sharing between
// executions. Deprecated registrations remain callable but are excluded from advertised tools.
type Registry struct {
	tools             []any
	toolMap           map[string]any
	schemaMap         map[string]*schema.Schema
	deprecated        map[string]bool
	printOutputSchema bool
}

// New creates a registry. Tool output schemas remain structured until provider preparation.
func New() *Registry {
	return &Registry{toolMap: map[string]any{}, schemaMap: map[string]*schema.Schema{},
		deprecated: map[string]bool{}, printOutputSchema: true}
}

// WithOutputSchema controls optional output-schema guidance in definitions. It does not enable
// local validation of returned data. Configure the registry before concurrent execution.
func (c *Registry) WithOutputSchema(enabled bool) *Registry {
	c.printOutputSchema = enabled
	return c
}

// RegisterTool validates and registers a complete Tool[I]. Invalid or duplicate definitions panic
// during setup. Returned schema views must remain read-only while the registry is in use.
func (c *Registry) RegisterTool(tool any) gent.ToolChain {
	meta, err := GetToolMeta(tool)
	if err != nil {
		panic(fmt.Errorf("register tool: %w", err))
	}
	if _, found := c.toolMap[meta.Name()]; found {
		panic(fmt.Errorf("duplicate tool %q", meta.Name()))
	}
	compiled, err := schema.Compile(meta.Schema())
	if err != nil {
		panic(fmt.Errorf("register tool %q schema: %w", meta.Name(), err))
	}
	c.tools = append(c.tools, tool)
	c.toolMap[meta.Name()], c.schemaMap[meta.Name()] = tool, compiled
	return c
}

// RegisterDeprecatedTool keeps a normal execution and recovery binding without advertising it.
// New calls can still name it. Keep old handlers while unfinished calls need their typed input.
func (c *Registry) RegisterDeprecatedTool(tool any) *Registry {
	c.RegisterTool(tool)
	c.deprecated[tool.(gent.IndexableTool).Name()] = true
	return c
}

// GetToolSchema returns the compiled input schema, or nil when the tool has no schema or binding.
func (c *Registry) GetToolSchema(name string) *schema.Schema { return c.schemaMap[name] }

func (c *Registry) executeParsed(ctx *gent.ExecutionContext, calls []*parsedCall,
	format gent.TextFormat,
) ([]*gent.ToolCallRecord, error) {
	results := make([]*gent.ToolCallRecord, 0, len(calls))
	for _, call := range calls {
		result, err := c.executeCall(ctx, call)
		if err != nil {
			return results, err
		}
		results = append(results, result)
	}
	return results, nil
}

var _ gent.ToolChain = (*Registry)(nil)
