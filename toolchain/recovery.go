package toolchain

import (
	"context"
	"fmt"

	"github.com/rickchristie/gent"
)

func (c *Registry) Recover(ctx context.Context,
	request *gent.ToolRecoveryRequest[gent.EncodedValue],
) (
	*gent.ToolRecoveryResponse, error,
) {
	tool := c.toolMap[request.Name()]
	if tool == nil {
		return nil, fmt.Errorf("cannot recover unavailable tool %q", request.Name())
	}
	return recoverToolReflect(ctx, tool, request)
}

func (c *SearchChain) Recover(ctx context.Context,
	request *gent.ToolRecoveryRequest[gent.EncodedValue],
) (
	*gent.ToolRecoveryResponse, error,
) {
	c.mu.RLock()
	tool := c.toolMap[request.Name()]
	c.mu.RUnlock()
	if tool == nil {
		return nil, fmt.Errorf("cannot recover unavailable tool %q", request.Name())
	}
	return recoverToolReflect(ctx, tool, request)
}

func (w *JsToolChainWrapper) Recover(ctx context.Context,
	request *gent.ToolRecoveryRequest[gent.EncodedValue],
) (
	*gent.ToolRecoveryResponse, error,
) {
	if request.Name() != nativeCodeTool {
		return w.wrapped.Recover(ctx, request)
	}
	return &gent.ToolRecoveryResponse{Status: gent.ToolRecoveryUnresolved,
		Instructions: "The script will not restart. Review partial results and choose the next action.",
	}, nil
}

// Catalog definitions and discovery metadata can change on resume. Execution configuration still
// contributes to compatibility; removing catalog hashes must not remove run-state checks.
func (c *Registry) RecoveryConfig() any { return map[string]any{"toolContract": 2} }
func (c *SearchChain) RecoveryConfig() any {
	return map[string]any{"toolContract": 2, "pageSize": c.pageSize,
		"noResults": c.noResultsMessage}
}
