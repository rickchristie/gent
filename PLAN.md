# Plan: TextOutputFormat and TextOutputSection Implementation

## Overview

Implement the output formatting system that allows AgentLoops to define how LLM output
should be structured into sections, with each section having its own parsing logic.

## Core Interfaces

### TextOutputSection (`section.go`)

```go
// TextOutputSection defines a section within the LLM's text output.
// Each section knows how to describe itself and parse its content.
type TextOutputSection interface {
    // Name returns the section identifier (e.g., "thinking", "action", "answer")
    Name() string

    // Prompt returns instructions for what should go in this section.
    // This is included in the LLM prompt.
    Prompt() string

    // ParseSection parses the raw text content extracted for this section.
    // Returns the parsed result or an error if parsing fails.
    ParseSection(content string) (any, error)
}
```

### TextOutputFormat (`format.go`)

```go
// TextOutputFormat defines how sections are structured in the LLM output.
// It handles the "envelope" - how sections are delimited and extracted.
type TextOutputFormat interface {
    // Describe generates the prompt section explaining the output format.
    // It combines section prompts with format-specific structure instructions.
    Describe(sections []TextOutputSection) string

    // Parse extracts raw content for each section from the LLM output.
    // Returns map of section name -> slice of content strings (supports multiple instances).
    // Sections not present in output will not appear in the map.
    Parse(output string) (map[string][]string, error)
}
```

## Package Structure

```
/home/ricky/Personal/gent/
├── section.go                    # TextOutputSection interface
├── format.go                     # TextOutputFormat interface
├── format_markdown.go            # MarkdownFormat implementation
├── format_markdown_test.go
├── format_xml.go                 # XMLFormat implementation
├── format_xml_test.go
├── tool.go                       # Tool interface
├── toolchain.go                  # ToolChain interface (extends TextOutputSection)
├── toolchain_json.go             # JSONToolChain
├── toolchain_json_test.go
├── toolchain_yaml.go             # YAMLToolChain
├── toolchain_yaml_test.go
├── termination.go                # Termination interface (extends TextOutputSection)
├── termination_text.go           # TextTermination
├── termination_text_test.go
├── termination_json.go           # JSONTermination with reflection
├── termination_json_test.go
```

## Implementation Details

### 1. MarkdownFormat (`format_markdown.go`)

Uses markdown headers to delimit sections:

```
# Thinking
I need to search for the weather...

# Action
{"tool": "search", "args": {"query": "weather"}}

# Answer
The weather is sunny today.
```

**Key behaviors:**
- Section headers are `# {SectionName}` (case-insensitive matching)
- Content is everything between one header and the next (or end of output)
- Trims leading/trailing whitespace from section content
- Supports multiple instances of same section (returns all in order)
- Ignores content before first recognized header

**Implementation:**
```go
type MarkdownFormat struct{}

func NewMarkdownFormat() *MarkdownFormat

func (f *MarkdownFormat) Describe(sections []TextOutputSection) string
func (f *MarkdownFormat) Parse(output string) (map[string][]string, error)
```

### 2. XMLFormat (`format_xml.go`)

Uses XML-style tags to delimit sections:

```
<thinking>
I need to search for the weather...
</thinking>

<action>
{"tool": "search", "args": {"query": "weather"}}
</action>

<answer>
The weather is sunny today.
</answer>
```

**Key behaviors:**
- Section tags are `<name>...</name>` (case-insensitive matching)
- Content is everything between opening and closing tag
- Trims leading/trailing whitespace from section content
- Supports multiple instances of same section
- Tags can be on same line or span multiple lines
- Ignores content outside recognized tags
- Does NOT validate as proper XML (just pattern matching)

**Implementation:**
```go
type XMLFormat struct{}

func NewXMLFormat() *XMLFormat

func (f *XMLFormat) Describe(sections []TextOutputSection) string
func (f *XMLFormat) Parse(output string) (map[string][]string, error)
```

### 3. Tool Interface (`tool.go`)

```go
// Tool represents a single callable tool available to the agent.
type Tool interface {
    // Name returns the tool's identifier used in tool calls.
    Name() string

    // Description returns a human-readable description for the LLM.
    Description() string

    // ParameterSchema returns the JSON Schema for the tool's parameters.
    // Returns nil if the tool takes no parameters.
    ParameterSchema() map[string]any

    // Execute runs the tool with the given arguments.
    // args is the parsed arguments (map[string]any from JSON/YAML).
    // Returns the tool output as a string, or an error.
    Execute(ctx context.Context, args map[string]any) (string, error)
}
```

**Helper for creating tools:**
```go
// ToolFunc is a convenience type for creating tools from functions.
type ToolFunc struct {
    name        string
    description string
    schema      map[string]any
    fn          func(ctx context.Context, args map[string]any) (string, error)
}

func NewToolFunc(
    name, description string,
    schema map[string]any,
    fn func(ctx context.Context, args map[string]any) (string, error),
) *ToolFunc
```

### 4. ToolChain Interface (`toolchain.go`)

```go
// ToolCall represents a parsed tool invocation from LLM output.
type ToolCall struct {
    Name string
    Args map[string]any
}

// ToolChainResult is the result of parsing and optionally executing tool calls.
type ToolChainResult struct {
    Calls   []ToolCall  // Parsed tool calls
    Results []string    // Execution results (if executed)
    Errors  []error     // Execution errors (if any)
}

// ToolChain manages a collection of tools and implements TextOutputSection.
// It handles describing tools to the LLM and parsing tool calls from output.
type ToolChain interface {
    TextOutputSection

    // Tools returns all registered tools.
    Tools() []Tool

    // RegisterTool adds a tool to the chain. Returns self for chaining.
    RegisterTool(tool Tool) ToolChain

    // Execute parses tool calls from content and executes them.
    // Returns results for each tool call.
    Execute(ctx context.Context, content string) (*ToolChainResult, error)
}
```

### 5. JSONToolChain (`toolchain_json.go`)

Expects tool calls in JSON format:

**Single tool call:**
```json
{"tool": "search", "args": {"query": "weather"}}
```

**Multiple tool calls (parallel):**
```json
[
  {"tool": "search", "args": {"query": "weather"}},
  {"tool": "calendar", "args": {"date": "today"}}
]
```

**Implementation:**
```go
type JSONToolChain struct {
    tools       []Tool
    sectionName string  // configurable, default "action"
}

func NewJSONToolChain() *JSONToolChain
func (c *JSONToolChain) WithSectionName(name string) *JSONToolChain

// TextOutputSection implementation
func (c *JSONToolChain) Name() string
func (c *JSONToolChain) Prompt() string      // Describes JSON format + lists tools
func (c *JSONToolChain) ParseSection(content string) (any, error)  // Returns []ToolCall

// ToolChain implementation
func (c *JSONToolChain) Tools() []Tool
func (c *JSONToolChain) RegisterTool(tool Tool) ToolChain
func (c *JSONToolChain) Execute(ctx context.Context, content string) (*ToolChainResult, error)
```

### 6. YAMLToolChain (`toolchain_yaml.go`)

Expects tool calls in YAML format with block scalars for readability:

**Single tool call:**
```yaml
tool: search
args:
  query: weather in tokyo
```

**Multiple tool calls:**
```yaml
- tool: search
  args:
    query: weather
- tool: calendar
  args:
    date: today
```

**Implementation:**
```go
type YAMLToolChain struct {
    tools       []Tool
    sectionName string  // configurable, default "action"
}

func NewYAMLToolChain() *YAMLToolChain
func (c *YAMLToolChain) WithSectionName(name string) *YAMLToolChain

// Same interface as JSONToolChain
```

**Dependency:** Add `gopkg.in/yaml.v3` to go.mod

### 7. Termination Interface (`termination.go`)

```go
// Termination is a TextOutputSection that signals the agent should stop.
// The parsed result represents the final output of the agent.
type Termination interface {
    TextOutputSection
}
```

### 8. TextTermination (`termination_text.go`)

Simply returns the raw text content as the final answer.

```go
type TextTermination struct {
    sectionName string  // configurable, default "answer"
    prompt      string  // configurable instructions
}

func NewTextTermination() *TextTermination
func (t *TextTermination) WithSectionName(name string) *TextTermination
func (t *TextTermination) WithPrompt(prompt string) *TextTermination

func (t *TextTermination) Name() string
func (t *TextTermination) Prompt() string
func (t *TextTermination) ParseSection(content string) (any, error)  // Returns string
```

### 9. JSONTermination (`termination_json.go`)

Parses JSON into a user-defined struct using reflection.

```go
// JSONTermination[T] parses JSON content into type T.
// Supports: primitives, pointers, structs, slices, maps, time.Time, time.Duration.
type JSONTermination[T any] struct {
    sectionName string
    prompt      string
    example     T  // Used to generate example in prompt
}

func NewJSONTermination[T any]() *JSONTermination[T]
func (t *JSONTermination[T]) WithSectionName(name string) *JSONTermination[T]
func (t *JSONTermination[T]) WithPrompt(prompt string) *JSONTermination[T]
func (t *JSONTermination[T]) WithExample(example T) *JSONTermination[T]

func (t *JSONTermination[T]) Name() string
func (t *JSONTermination[T]) Prompt() string  // Includes JSON schema derived from T
func (t *JSONTermination[T]) ParseSection(content string) (any, error)  // Returns T
```

**Type support via reflection:**
- Primitives: string, int, float, bool
- Pointers: *T (nil if JSON null)
- Structs: nested structs with json tags
- Slices: []T
- Maps: map[string]T
- time.Time: RFC3339 string parsing
- time.Duration: duration string parsing (e.g., "1h30m")

**Schema generation:**
- Uses reflection to generate JSON schema from T
- Includes field descriptions from struct tags if present
- Generates example JSON from the example field if provided

## Error Handling

All errors are returned, never swallowed (per CLAUDE.md):

```go
// Parse errors
var (
    ErrNoSectionsFound    = errors.New("no recognized sections found in output")
    ErrInvalidJSON        = errors.New("invalid JSON in section content")
    ErrInvalidYAML        = errors.New("invalid YAML in section content")
    ErrMissingToolName    = errors.New("tool call missing 'tool' field")
    ErrUnknownTool        = errors.New("unknown tool")
    ErrInvalidToolArgs    = errors.New("invalid tool arguments")
)

// Wrap errors with context
fmt.Errorf("failed to parse %s section: %w", sectionName, err)
```

## Testing Strategy

### Unit Tests

Each implementation gets thorough unit tests:

**format_markdown_test.go:**
- Single section parsing
- Multiple sections parsing
- Multiple instances of same section
- Case-insensitive header matching
- Content before first header (ignored)
- Empty sections
- Whitespace trimming
- Describe() output format

**format_xml_test.go:**
- Same test cases as markdown
- Self-closing tags (if supported)
- Nested content (not XML-parsed, just extracted)
- Tags on same line vs multiline

**toolchain_json_test.go:**
- Single tool call parsing
- Multiple tool calls (array)
- Missing tool name error
- Invalid JSON error
- Unknown tool error
- Tool execution success
- Tool execution error propagation
- Prompt generation with multiple tools

**toolchain_yaml_test.go:**
- Same test cases as JSON
- Block scalar content
- Multi-line string arguments

**termination_text_test.go:**
- Returns trimmed content
- Empty content
- Multi-line content

**termination_json_test.go:**
- Simple struct parsing
- Nested struct parsing
- Pointer fields (nil and non-nil)
- time.Time parsing
- time.Duration parsing
- Slice fields
- Map fields
- Invalid JSON error
- Type mismatch error
- Schema generation accuracy

### Integration Test

Create `integration_test.go` that tests the full flow:
- Create sections (JSONToolChain + TextTermination)
- Create format (XMLFormat)
- Generate describe output
- Parse sample LLM output
- Execute tool calls

## File-by-File Implementation Order

1. **section.go** - TextOutputSection interface (simple, foundational)
2. **format.go** - TextOutputFormat interface + errors
3. **format_markdown.go** + tests - First format implementation
4. **format_xml.go** + tests - Second format implementation
5. **tool.go** - Tool interface + ToolFunc helper
6. **toolchain.go** - ToolChain interface + types
7. **toolchain_json.go** + tests - JSON tool chain
8. **toolchain_yaml.go** + tests - YAML tool chain (add yaml dependency)
9. **termination.go** - Termination interface
10. **termination_text.go** + tests - Text termination
11. **termination_json.go** + tests - JSON termination with reflection

## Dependencies to Add

```
gopkg.in/yaml.v3  # For YAML parsing
```

## Verification

After implementation:

1. **Run all tests:**
   ```bash
   go test ./... -v
   ```

2. **Check for lint issues:**
   ```bash
   go vet ./...
   ```

3. **Verify line length compliance:**
   All lines must be ≤100 characters per CLAUDE.md

4. **Manual integration test:**
   Create a simple program that:
   - Defines a mock tool
   - Creates XMLFormat with JSONToolChain + TextTermination
   - Generates prompt
   - Parses sample output
   - Executes tool call

## Example Usage (Post-Implementation)

```go
// Define a tool
searchTool := gent.NewToolFunc(
    "search",
    "Search the web for information",
    map[string]any{
        "type": "object",
        "properties": map[string]any{
            "query": map[string]any{"type": "string", "description": "Search query"},
        },
        "required": []string{"query"},
    },
    func(ctx context.Context, args map[string]any) (string, error) {
        query := args["query"].(string)
        return fmt.Sprintf("Results for: %s", query), nil
    },
)

// Create sections
toolChain := gent.NewJSONToolChain().RegisterTool(searchTool)
termination := gent.NewTextTermination()

// Create format
format := gent.NewXMLFormat()

// Generate prompt section
prompt := format.Describe([]gent.TextOutputSection{toolChain, termination})

// Later, parse LLM output
output := `<action>{"tool": "search", "args": {"query": "weather"}}</action>`
sections, _ := format.Parse(output)

if content, ok := sections["action"]; ok {
    result, _ := toolChain.Execute(ctx, content[0])
    fmt.Println(result.Results[0])  // "Results for: weather"
}
```
