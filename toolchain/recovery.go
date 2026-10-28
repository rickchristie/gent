package toolchain

import (
	"context"
	"fmt"

	"github.com/rickchristie/gent"
)

func (c *Registry) Recover(ctx context.Context, request *gent.ToolRecoveryRequest) (
	*gent.ToolRecoveryResponse, error,
) {
	tool, found := c.toolMap[request.Name]
	if !found {
		return nil, fmt.Errorf("cannot recover unknown tool %q", request.Name)
	}
	return tool.(toolDefinition).Recover(ctx, request)
}

func (c *SearchChain) Recover(ctx context.Context, request *gent.ToolRecoveryRequest) (
	*gent.ToolRecoveryResponse, error,
) {
	c.mu.RLock()
	tool, found := c.toolMap[request.Name]
	c.mu.RUnlock()
	// The two search operations only read the index and can be repeated after restoration.
	if request.Name == searchToolName || request.Name == getSchemaToolName {
		return &gent.ToolRecoveryResponse{Result: gent.ToolRecoveryRetryAllowed}, nil
	}
	if !found {
		return nil, fmt.Errorf("cannot recover unknown tool %q", request.Name)
	}
	return tool.(toolDefinition).Recover(ctx, request)
}

func (w *JsToolChainWrapper) Recover(ctx context.Context, request *gent.ToolRecoveryRequest) (
	*gent.ToolRecoveryResponse, error,
) {
	if request.Name != nativeCodeTool {
		return w.wrapped.Recover(ctx, request)
	}
	// The wrapper owns program recovery. It recovers inner calls and retains partial output;
	// restarting a script could repeat operations that have already completed.
	return &gent.ToolRecoveryResponse{Result: gent.ToolRecoveryUnresolved,
		SteeringMessage: "Review the partial results and make a new plan."}, nil
}

func (c *Registry) RecoveryConfig() any {
	definitions, err := nativeDefinitions(c.tools, c.printOutputSchema)
	if err != nil {
		panic(err)
	}
	return map[string]any{"tools": definitions}
}

func (c *SearchChain) RecoveryConfig() any {
	c.mu.RLock()
	defer c.mu.RUnlock()
	definitions, err := nativeDefinitions(c.tools, c.printOutputSchema)
	if err != nil {
		panic(err)
	}
	return map[string]any{"tools": definitions, "search": c.nativeSearchTools(),
		"metadata": toolSearchMetadata(c.indexableTools),
		"pinned":   c.pinnedToolNames, "pageSize": c.pageSize, "noResults": c.noResultsMessage}
}

func skippedTool(plan *gent.ToolPlan) *gent.ToolCallResult {
	call := plan.Calls[plan.NextCall]
	return &gent.ToolCallResult{ToolCallId: call.Id, Name: call.Name, Args: call.Args,
		Outcome: "not_started", SummaryCaptured: true,
		Text: "This call did not start. Review the interrupted operation before making a new plan."}
}

// Search behavior depends on keywords and example queries as well as the visible catalog.
func toolSearchMetadata(tools []gent.IndexableTool) []map[string]any {
	metadata := make([]map[string]any, 0, len(tools))
	for _, tool := range tools {
		metadata = append(metadata, map[string]any{"name": tool.Name(), "domain": tool.Domain(),
			"categories": tool.Categories(), "keywords": tool.Keywords(),
			"syntheticQueries": tool.SyntheticQueries()})
	}
	return metadata
}
