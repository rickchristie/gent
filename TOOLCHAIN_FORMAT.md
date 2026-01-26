# ToolChain Formatting Design

## Problem Statement

Currently there are two separate section standards in the codebase:

1. **TextOutputFormat** - Defines how the LLM should structure its output (markdown `# Section` or XML `<section>`)
2. **ToolChain observations** - Uses hardcoded `[tool_name]` brackets in `agents/react/agent.go`

This creates inconsistency:
- The LLM sees two different section conventions in the same conversation
- The `[tool_name]` format is hardcoded, not configurable
- If using XML TextOutputFormat, observations still use brackets

Additionally, there's a responsibility violation:
- `ToolResult` currently contains `[]ContentPart` (formatted content)
- But formatting should be ToolChain's responsibility, not Tool's

## Design Goals

1. **Single section standard** - Use TextFormat for both LLM output parsing and observation formatting
2. **Clear responsibility split** - Tool returns raw data, ToolChain formats
3. **Multimodal support** - Clean separation of text and media (images, future audio)
4. **Simple API** - Custom loop writers don't need to wire formatting manually

## Solution Overview

### Rename TextOutputFormat to TextFormat

The interface handles both directions:
- Parsing LLM output (existing)
- Formatting sections for LLM input (new)

```go
type TextFormat interface {
    // Existing - for parsing LLM output
    RegisterSection(section TextSection) TextFormat
    Parse(execCtx ExecutionContext, output string) (map[string][]string, error)
    DescribeStructure() string

    // New - for formatting input to LLM
    FormatSection(name string, content string) string
    FormatSections(sections []FormattedSection) string
}

type FormattedSection struct {
    Name    string
    Content string
}
```

### Tool Interface Changes

Rename generic type `O` to `TextOutput` for clarity:

```go
type Tool[I, TextOutput any] interface {
    Name() string
    Description() string
    Call(ctx context.Context, input I) (*ToolResult[TextOutput], error)
}

type ToolResult[TextOutput any] struct {
    Text  TextOutput     // Raw typed output - ToolChain will format this
    Media []ContentPart  // Images, audio, etc. - passed through or transformed
}
```

### ToolChain Interface Changes

Pass TextFormat to Execute:

```go
type ToolChain interface {
    Execute(
        ctx ExecutionContext,
        content string,
        textFormat TextFormat,
    ) (*ToolChainResult, error)

    // ... other methods unchanged
}

type ToolChainResult struct {
    Text  string              // Formatted text with section separators
    Media []ContentPart       // Images, audio, etc.
    Raw   RawToolChainResult  // For programmatic access
}

// Helper for building LLM messages
func (r *ToolChainResult) AsContentParts() []ContentPart {
    parts := []ContentPart{llms.TextContent{Text: r.Text}}
    return append(parts, r.Media...)
}

// Current ToolChainResult renamed, with Result field removed from ToolCallResult
type RawToolChainResult struct {
    Calls   []*ToolCall
    Results []*RawToolCallResult
    Errors  []error
}

type RawToolCallResult struct {
    Name   string
    Output any  // Raw typed output from Tool
}
```

## Responsibility Split

| Component | Responsibility |
|-----------|----------------|
| **Tool** | Execute operation, return raw typed data (`TextOutput`) and media |
| **ToolChain** | Format raw data into text (JSON/YAML/etc.), apply section formatting via TextFormat, optionally transform media |
| **TextFormat** | Define section structure (headers, separators), format and parse sections |

## Data Flow

```
┌─────────────────────────────────────────────────────────────────────────────┐
│ Tool.Call(ctx, input)                                                       │
│   → ToolResult[TextOutput]{                                                 │
│       Text:  CustomerInfo{ID: "C001", Name: "John"},  // Raw struct         │
│       Media: []ContentPart{...},                       // Optional images   │
│     }                                                                       │
└─────────────────────────────────────────────────────────────────────────────┘
                                      │
                                      ▼
┌─────────────────────────────────────────────────────────────────────────────┐
│ ToolChain.Execute(ctx, content, textFormat)                                 │
│                                                                             │
│   1. Parse tool calls from content                                          │
│   2. For each tool call:                                                    │
│      a. Execute Tool.Call()                                                 │
│      b. Format TextOutput as JSON/YAML → string                             │
│      c. Apply textFormat.FormatSection(toolName, jsonString)                │
│      d. Collect media ContentParts                                          │
│   3. Combine all sections via textFormat.FormatSections()                   │
│                                                                             │
│   → ToolChainResult{                                                        │
│       Text:  "# get_customer_info\n{\"id\":\"C001\"...}\n\n# get_flight..." │
│       Media: []ContentPart{...},                                            │
│       Raw:   RawToolChainResult{...},                                       │
│     }                                                                       │
└─────────────────────────────────────────────────────────────────────────────┘
                                      │
                                      ▼
┌─────────────────────────────────────────────────────────────────────────────┐
│ Agent Loop (e.g., ReAct)                                                    │
│                                                                             │
│   // Build observation message for LLM                                      │
│   observation := toolChainResult.Text                                       │
│   media := toolChainResult.Media                                            │
│                                                                             │
│   // Or use helper if needed                                                │
│   contentParts := toolChainResult.AsContentParts()                          │
└─────────────────────────────────────────────────────────────────────────────┘
```

## TextFormat Implementations

### Markdown Format

```go
func (m *MarkdownFormat) FormatSection(name string, content string) string {
    return fmt.Sprintf("# %s\n%s", name, content)
}

func (m *MarkdownFormat) FormatSections(sections []FormattedSection) string {
    var sb strings.Builder
    for i, section := range sections {
        if i > 0 {
            sb.WriteString("\n\n")
        }
        sb.WriteString(m.FormatSection(section.Name, section.Content))
    }
    return sb.String()
}
```

### XML Format

```go
func (x *XMLFormat) FormatSection(name string, content string) string {
    return fmt.Sprintf("<%s>\n%s\n</%s>", name, content, name)
}

func (x *XMLFormat) FormatSections(sections []FormattedSection) string {
    var sb strings.Builder
    for _, section := range sections {
        sb.WriteString(x.FormatSection(section.Name, section.Content))
        sb.WriteString("\n")
    }
    return sb.String()
}
```

## Error Handling

Errors appear in both places:
- **In Text** (formatted): So the LLM sees the error
- **In Raw.Errors** (programmatic): For agent logic to inspect

```go
// In ToolChain.Execute:
if err != nil {
    errorText := fmt.Sprintf("Error: %v", err)
    section := textFormat.FormatSection(toolCall.Name, errorText)
    // ... append to text
    raw.Errors[i] = err
}
```

## Media Handling

Tools that produce images return them in `ToolResult.Media`:

```go
func (t *ScreenshotTool) Call(ctx context.Context, input ScreenshotInput) (*ToolResult[ScreenshotOutput], error) {
    imgBytes, err := takeScreenshot()
    if err != nil {
        return nil, err
    }

    return &ToolResult[ScreenshotOutput]{
        Text: ScreenshotOutput{
            Width:  1920,
            Height: 1080,
            Format: "png",
        },
        Media: []ContentPart{
            llms.ImageContent{
                Data:      imgBytes,
                MediaType: "image/png",
            },
        },
    }, nil
}
```

ToolChain passes media through (or optionally transforms, e.g., resizing):

```go
// In ToolChain.Execute:
for _, toolResult := range results {
    // Format text output
    jsonBytes, _ := json.Marshal(toolResult.Text)
    section := textFormat.FormatSection(toolName, string(jsonBytes))
    textBuilder.WriteString(section)

    // Collect media (pass through or transform)
    allMedia = append(allMedia, toolResult.Media...)
}
```

## Migration Path

1. **Add new methods to TextOutputFormat**, rename to TextFormat
2. **Update ToolResult** - rename `Output` to `Text`, add `Media` field, remove `Result`
3. **Update ToolChain.Execute** signature to accept TextFormat
4. **Update ToolChain implementations** (JSON, YAML) to use TextFormat for section formatting
5. **Update agent loops** to use new ToolChainResult structure
6. **Remove hardcoded `[tool_name]` formatting** from agent.go

## Example: Before and After

### Before (Current)

```go
// In agent.go - hardcoded format
fmt.Fprintf(&resultText, "[%s]\n", toolResult.Name)
for _, part := range toolResult.Result {
    if tc, ok := part.(llms.TextContent); ok {
        resultText.WriteString(tc.Text)
    }
}
```

### After (New Design)

```go
// In agent.go - uses ToolChainResult directly
result, err := toolChain.Execute(ctx, content, textFormat)
if err != nil {
    return err
}

// Text is already formatted with proper sections
observation := result.Text
media := result.Media
```

## Design Decisions

1. **nil TextFormat** - If `nil` is passed to Execute, panic. TextFormat is required.

2. **result.Text includes full structure** - The returned `Text` includes both section formatting AND the outer wrapper (e.g., `<observation>` tags for XML). Agent loops just use `result.Text` directly with no additional work.

3. **Empty results** - If no tools executed successfully, `result.Text` should be an empty string (not an empty wrapper).
