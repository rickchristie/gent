package models

import "github.com/rickchristie/gent"

// PrepareRequest resolves SharedLocalSearch and native definitions for this request.
func (m *Responses) PrepareRequest(ctx *gent.ExecutionContext, request *gent.ModelCallRequest,
	chain gent.ToolChain,
) (*gent.ModelCallRequest, error) {
	return gent.PrepareModelRequest(ctx, request, chain)
}

// PrepareRequest resolves SharedLocalSearch and native definitions for this request.
func (m *Chat) PrepareRequest(ctx *gent.ExecutionContext, request *gent.ModelCallRequest,
	chain gent.ToolChain,
) (*gent.ModelCallRequest, error) {
	return gent.PrepareModelRequest(ctx, request, chain)
}

// PrepareRequest resolves SharedLocalSearch and native definitions for this request.
func (m *Anthropic) PrepareRequest(ctx *gent.ExecutionContext, request *gent.ModelCallRequest,
	chain gent.ToolChain,
) (*gent.ModelCallRequest, error) {
	return gent.PrepareModelRequest(ctx, request, chain)
}

// PrepareRequest resolves SharedLocalSearch and native definitions for this request.
func (m *generateContentModel) PrepareRequest(ctx *gent.ExecutionContext,
	request *gent.ModelCallRequest, chain gent.ToolChain,
) (*gent.ModelCallRequest, error) {
	return gent.PrepareModelRequest(ctx, request, chain)
}

// PrepareRequest resolves SharedLocalSearch and native definitions for this request.
func (m *Ollama) PrepareRequest(ctx *gent.ExecutionContext, request *gent.ModelCallRequest,
	chain gent.ToolChain,
) (*gent.ModelCallRequest, error) {
	return gent.PrepareModelRequest(ctx, request, chain)
}
