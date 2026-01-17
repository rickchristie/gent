# Plan: ReAct AgentLoop Implementation

## Overview

Implement a ReAct (Reasoning and Acting) AgentLoop that orchestrates the classic ReAct flow:
Think → Act → Observe → Repeat until termination.

## Flow

Per user specification:
1. Construct prompt: SystemPrompt + OutputPrompt (tools, termination) + UserInput + LoopText
2. Run prompt with model call
3. Parse response with formatter to get sectioned output
4. Check termination first - if should terminate, return early
5. Execute tool calls via ToolChain
6. Construct observation, add to LoopData, return for next iteration

## Files to Create

### 1. `/home/ricky/Personal/gent/agent_react.go`

Main ReAct AgentLoop implementation.

### 2. `/home/ricky/Personal/gent/agent_react_test.go`

Tests for the ReAct AgentLoop.

## Implementation Details

### ReactLoopData (LoopData Implementation)

```go
type ReactLoopData struct {
    originalInput    []ContentPart
    iterationHistory [][]*IterationInfo
    iterations       [][]*IterationInfo
}
```

Methods to implement:
- `GetOriginalInput() []ContentPart`
- `GetIterationHistory() [][]*IterationInfo`
- `AddIterationHistory(info *IterationInfo)`
- `GetIterations() [][]*IterationInfo`
- `SetIterations([][]*IterationInfo)`

Constructor: `NewReactLoopData(input ...ContentPart) *ReactLoopData`

### ReactLoop (AgentLoop Implementation)

```go
type ReactLoop struct {
    systemPrompt      string
    model             Model
    format            TextOutputFormat
    toolChain         ToolChain
    termination       Termination
    thinkingSection   TextOutputSection
    observationPrefix string
    errorPrefix       string
}
```

Constructor:
```go
func NewReactLoop(model Model) *ReactLoop
```

Defaults:
- Format: `format.NewXML()`
- ToolChain: `toolchain.NewYAML()`
- Termination: `termination.NewText()`
- ObservationPrefix: `"Tool results:\n"`
- ErrorPrefix: `"Tool error:\n"`

Builder methods:
- `WithSystemPrompt(string) *ReactLoop`
- `WithFormat(TextOutputFormat) *ReactLoop`
- `WithToolChain(ToolChain) *ReactLoop`
- `WithTermination(Termination) *ReactLoop`
- `WithThinking(prompt string) *ReactLoop`
- `WithThinkingSection(TextOutputSection) *ReactLoop`
- `RegisterTool(tool any) *ReactLoop`

### Iterate() Logic

```go
func (r *ReactLoop) Iterate(ctx context.Context, data LoopData) *AgentLoopResult
```

Steps:
1. Build output sections: [thinking (optional), toolchain, termination]
2. Generate output prompt: `r.format.Describe(sections)`
3. Build messages: system + user input + previous iterations
4. Call model: `r.model.GenerateContent(ctx, messages)`
5. Parse response: `r.format.Parse(responseContent)`
6. Check termination: if answer section present, call `termination.ShouldTerminate()`
7. Execute tools: if action section present, call `toolChain.Execute()`
8. Build observation from tool results
9. Create IterationInfo with assistant message + observation
10. Add to data, return LAContinue with observation

### Helper Methods

```go
func (r *ReactLoop) buildOutputSections() []TextOutputSection
func (r *ReactLoop) buildMessages(data LoopData, outputPrompt string) []llms.MessageContent
func (r *ReactLoop) executeToolCalls(ctx context.Context, contents []string) string
func (r *ReactLoop) buildIterationInfo(response, observation string) *IterationInfo
```

### simpleSection (for thinking)

```go
type simpleSection struct {
    name   string
    prompt string
}

func (s *simpleSection) Name() string { return s.name }
func (s *simpleSection) Prompt() string { return s.prompt }
func (s *simpleSection) ParseSection(content string) (any, error) { return content, nil }
```

## Message Structure

For model calls, messages are constructed as:

1. **System Message**: SystemPrompt + OutputPrompt (format description)
2. **User Message**: Original input from `data.GetOriginalInput()`
3. **Previous Iterations**: For each iteration:
   - Assistant message: LLM's full response
   - User message: Observation (tool results)

## Observation Format

Tool results:
```
Tool results:
[tool_name] result content

Tool results:
[another_tool] another result
```

Tool errors:
```
Tool error:
tool_name: error message
```

## Error Handling

Per CLAUDE.md - never swallow errors:
- Model call error → Terminate with error in result
- Parse error → Check if raw content is termination, else terminate with error
- Tool error → Include in observation for LLM to adapt

## Testing Strategy

### Unit Tests

1. `TestReactLoopData_*` - Test LoopData implementation methods
2. `TestReactLoop_BuildOutputSections` - Verify sections are built correctly
3. `TestReactLoop_BuildMessages` - Verify message construction
4. `TestReactLoop_Iterate_Termination` - Test termination path
5. `TestReactLoop_Iterate_ToolExecution` - Test tool execution path
6. `TestReactLoop_Iterate_MultipleTools` - Test multiple tool calls
7. `TestReactLoop_Iterate_ToolError` - Test error handling
8. `TestReactLoop_Iterate_ParseError` - Test parse error handling

### Mock Requirements

- Mock Model that returns predefined responses
- Mock tools for testing execution

## Example Usage

```go
// Create ReAct loop
loop := gent.NewReactLoop(model).
    WithSystemPrompt("You are a helpful assistant.").
    WithThinking("Think step by step.").
    RegisterTool(searchTool).
    RegisterTool(calculatorTool)

// Create loop data
data := gent.NewReactLoopData(llms.TextContent{Text: "What is 2+2?"})

// Run iteration
result := loop.Iterate(ctx, data)
// result.Action == LAContinue (with observation) or LATerminate (with answer)
```

## Verification

1. Run tests: `go test ./... -v`
2. Run vet: `go vet ./...`
3. Verify line length ≤100 chars
4. Test with mock model that returns:
   - Direct answer (termination path)
   - Tool call (execution path)
   - Multiple tool calls
   - Invalid response (error path)
