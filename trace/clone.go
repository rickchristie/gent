package trace

import "github.com/rickchristie/gent"

func cloneSnapshot(snapshot *Snapshot) *Snapshot {
	if snapshot == nil {
		return nil
	}
	result := *snapshot
	if snapshot.CompletedTs != nil {
		completed := *snapshot.CompletedTs
		result.CompletedTs = &completed
	}
	result.Result = cloneRunResult(snapshot.Result)
	result.Contexts = cloneSlice(snapshot.Contexts, cloneContext)
	result.Iterations = cloneSlice(snapshot.Iterations, cloneIteration)
	result.ModelCalls = cloneSlice(snapshot.ModelCalls, cloneModelCall)
	result.ToolCalls = cloneSlice(snapshot.ToolCalls, cloneToolCall)
	result.Errors = cloneSlice(snapshot.Errors, cloneError)
	result.RecentEvents = cloneSlice(snapshot.RecentEvents, cloneEvent)
	result.RecentLifecycleEvents = cloneSlice(snapshot.RecentLifecycleEvents, cloneEvent)
	result.RecentChunkEvents = cloneSlice(snapshot.RecentChunkEvents, cloneEvent)
	return &result
}

func cloneRunResult(result *RunResult) *RunResult {
	if result == nil {
		return nil
	}
	clone := *result
	clone.Output = cloneAny(result.Output)
	if result.Refusal != nil {
		copy := *result.Refusal
		copy.Content = gent.CloneContentParts(result.Refusal.Content)
		clone.Refusal = &copy
	}
	clone.Error = cloneError(result.Error)
	return &clone
}

func cloneEvent(event *Event) *Event {
	if event == nil {
		return nil
	}
	clone := *event
	clone.Payload = cloneAny(event.Payload)
	clone.Error = cloneError(event.Error)
	return &clone
}

func cloneContext(ctx *Context) *Context {
	if ctx == nil {
		return nil
	}
	clone := *ctx
	clone.Content = gent.CloneContentParts(ctx.Content)
	clone.SourceMessageIds = append([]string(nil), ctx.SourceMessageIds...)
	if ctx.CompletedTs != nil {
		completed := *ctx.CompletedTs
		clone.CompletedTs = &completed
	}
	clone.Error = cloneError(ctx.Error)
	return &clone
}

func cloneIteration(iteration *Iteration) *Iteration {
	if iteration == nil {
		return nil
	}
	clone := *iteration
	if iteration.CompletedTs != nil {
		completed := *iteration.CompletedTs
		clone.CompletedTs = &completed
	}
	clone.ModelCallIds = append([]string(nil), iteration.ModelCallIds...)
	clone.ToolCallIds = append([]string(nil), iteration.ToolCallIds...)
	clone.Result = cloneAny(iteration.Result)
	clone.Error = cloneError(iteration.Error)
	return &clone
}

func cloneModelCall(call *ModelCall) *ModelCall {
	if call == nil {
		return nil
	}
	clone := *call
	if call.CompletedTs != nil {
		completed := *call.CompletedTs
		clone.CompletedTs = &completed
	}
	clone.Request = cloneAny(call.Request)
	clone.Response = cloneAny(call.Response)
	if call.Usage != nil {
		usage := *call.Usage
		clone.Usage = &usage
	}
	if call.Stream != nil {
		stream := *call.Stream
		stream.MetaContent = gent.CloneMetaContent(call.Stream.MetaContent)
		clone.Stream = &stream
	}
	clone.Error = cloneError(call.Error)
	return &clone
}

func cloneToolCall(call *ToolCall) *ToolCall {
	if call == nil {
		return nil
	}
	clone := *call
	if call.CompletedTs != nil {
		completed := *call.CompletedTs
		clone.CompletedTs = &completed
	}
	clone.Args = cloneAny(call.Args)
	clone.Output = cloneAny(call.Output)
	clone.Error = cloneError(call.Error)
	return &clone
}

func cloneError(err *Error) *Error {
	if err == nil {
		return nil
	}
	clone := *err
	return &clone
}

func cloneSlice[T any](items []*T, cloneFn func(*T) *T) []*T {
	if len(items) == 0 {
		return nil
	}
	result := make([]*T, 0, len(items))
	for _, item := range items {
		result = append(result, cloneFn(item))
	}
	return result
}
