package gent

// PrepareModelRequest resolves a local tool catalog for adapters that use SharedLocalSearch.
// A nil chain leaves explicitly supplied definitions in place. It does not change request.
// Definitions remain structured until native encoding. Hooks run at the managed boundary.
func PrepareModelRequest(ctx *ExecutionContext, request *ModelCallRequest, chain ToolChain) (
	*ModelCallRequest, error,
) {
	if chain == nil {
		return request, nil
	}
	definitions, err := chain.NativeTools(ctx)
	if err != nil {
		return nil, err
	}
	prepared := *request
	prepared.Options.Tools = definitions
	return &prepared, nil
}
