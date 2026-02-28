# ToolSearchToolChain Implementation Plan

## Context

LLM accuracy degrades beyond 20-30 tools with static loading. ToolSearchToolChain solves this
by hiding all registered tools behind a single built-in "Tool Registry Search" tool. The LLM
searches for tools on demand, gets full definitions back, then calls discovered tools. This
follows Anthropic's Tool Search Tool pattern (85% token reduction, accuracy jump from 49% to 74%).

---

## File Structure

| File | Purpose |
|------|---------|
| `tool_search.go` | `IndexableTool` + `SearchEngine` interfaces (root package) |
| `toolchain/search.go` | `SearchJSON` struct, constructor, RegisterTool, Initialize, Execute |
| `toolchain/search_prompt.go` | Domain summary builder, search tool prompt builder |
| `toolchain/search_engine_bm25.go` | `BM25SearchEngine` using Bleve `NewMemOnly()` |
| `toolchain/search_engine_regex.go` | `RegexSearchEngine` using Go `regexp` |
| `toolchain/search_test.go` | SearchJSON tests |
| `toolchain/search_engine_bm25_test.go` | BM25 engine tests |
| `toolchain/search_engine_regex_test.go` | Regex engine tests |

---

## 1. Interfaces (`tool_search.go`)

### IndexableTool

```go
type IndexableTool interface {
    Name() string
    Description() string
    Domain() string
    Categories() []string
    Keywords() []string
    SyntheticQueries() []string
}
```

Same struct implements both `Tool[I,O]` and `IndexableTool`. The `Name()`/`Description()` methods
are literally the same method on the same receiver.

### SearchEngine

```go
type SearchEngine interface {
    Id() string
    SearchGuidance() string
    IndexAll(tools []IndexableTool) error
    Search(ctx context.Context, query string) ([]string, error)
}
```

- `Search` returns ALL matching tool names ranked by relevance
- `ToolSearchToolChain` handles pagination over results
- Error messages from `Search` are surfaced to the LLM

---

## 2. SearchJSON (`toolchain/search.go`)

### Struct

```go
type SearchJSON struct {
    mu sync.RWMutex

    // TextSection
    sectionName string // default "action"

    // Tool registry (same pattern as JSON toolchain)
    tools     []any
    toolMap   map[string]any
    schemaMap map[string]*schema.Schema

    // IndexableTool metadata for search
    indexableTools []gent.IndexableTool

    // Search engines
    engines   []gent.SearchEngine
    engineMap map[string]gent.SearchEngine

    // Config
    pageSize         int    // default 3
    noResultsMessage string // configurable

    // Computed by Initialize()
    initialized          bool
    searchToolPrompt     string
    searchToolSchema     map[string]any
    compiledSearchSchema *schema.Schema
}
```

### Constructor + Builders

```go
func NewSearchJSON() *SearchJSON
func (c *SearchJSON) WithSectionName(name string) *SearchJSON
func (c *SearchJSON) WithPageSize(size int) *SearchJSON
func (c *SearchJSON) WithNoResultsMessage(msg string) *SearchJSON
func (c *SearchJSON) RegisterEngine(engine gent.SearchEngine) *SearchJSON
```

### RegisterTool

1. Extract `Tool[I,O]` metadata via `GetToolMeta()` (reflection) — panic on failure
2. Type-assert to `gent.IndexableTool` — panic if not implemented
3. Check duplicate name — panic if exists
4. Store in `tools`, `toolMap`, `schemaMap`, `indexableTools`
5. Return self for chaining

Key difference from `JSON.RegisterTool`: panics on invalid tools instead of silently ignoring.
This is a programmer error (not implementing `IndexableTool`), detected at startup.

### Initialize() error

Takes write lock. Steps:
1. Validate at least one engine registered
2. Call `engine.IndexAll(indexableTools)` for each engine
3. Build domain summary (aggregate Domain → Categories → count)
4. Build dynamic search tool schema (enum from engine IDs)
5. Compile search tool schema
6. Build full search tool prompt
7. Set `initialized = true`

Can be called multiple times — re-indexes everything, rebuilds state.
Domain order is deterministic (insertion order of first tool per domain).

### AvailableToolsPrompt()

Takes read lock. Returns pre-computed `searchToolPrompt` from Initialize().
Shows ONLY the "Tool Registry Search" tool with:
- Domain summary (domain, categories, tool count)
- Search guidance per engine
- Full parameter schema with dynamic `query_type` enum

### Name(), Guidance(), ParseSection()

- `Name()` → `sectionName` (default "action")
- `Guidance()` → identical to `JSON.Guidance()` (JSON format instructions)
- `ParseSection()` → identical logic to `JSON.ParseSection()` (parse JSON, publish events, reset gauges)

### Execute()

Takes read lock. Handles two kinds of tool calls:

**Built-in "Tool Registry Search":**
1. Validate args against compiled search tool schema
2. Extract `query`, `query_type`, `page` (default 1)
3. Publish `BeforeToolCall`
4. Look up engine by `query_type`
5. Call `engine.Search(ctx, query)`
6. Paginate: `results[(page-1)*pageSize : page*pageSize]`
7. Format matched tools as full definitions (name, description, policy, schema)
8. Append pagination info: "Showing page X of Y (N total results)"
9. If no results for page, return `noResultsMessage`
10. Reset consecutive error gauges on success
11. Publish `AfterToolCall`

**Regular registered tools:**
Identical flow to `JSON.Execute()`:
lookup → schema validate → TransformArgsReflect → PublishBeforeToolCall →
CallToolWithTypedInputReflect → format result → PublishAfterToolCall

Both types can appear in the same JSON array (parallel execution).

### Code Reuse Strategy

The regular tool execution path in `Execute()` and the `ParseSection`/`doParse` logic are
identical to `JSON`. Following the existing pattern (YAML and JSON each have their own copy),
we copy the code. The duplication is ~30 lines for parsing and ~80 lines for the tool execution
loop. This avoids coupling and matches the project's established pattern.

The `formatToolDefinitions()` helper reuses `GetToolMeta()` from `reflect.go` and formats
output identically to `JSON.AvailableToolsPrompt()`.

---

## 3. Prompt Generation (`toolchain/search_prompt.go`)

### buildDomainSummary()

Iterates `indexableTools`, groups by `Domain()`, collects unique `Categories()`, counts tools.
Maintains insertion order for deterministic output.

Output:
```
- Customer (tenant, landlord) - 25 tools
- Communication (email, SMS, WhatsApp) - 12 tools
```

### buildSearchToolSchema()

Generates JSON Schema with dynamic `query_type` enum from registered engine IDs:
```json
{
  "type": "object",
  "properties": {
    "query": {"type": "string", "description": "Search query"},
    "query_type": {"type": "string", "enum": ["bm25", "regex"], "description": "..."},
    "page": {"type": "integer", "default": 1, "description": "..."}
  },
  "required": ["query", "query_type"]
}
```

### buildSearchToolPrompt()

Combines: tool name + description + domain summary + per-engine guidance + parameter schema.
Same format as `JSON.AvailableToolsPrompt()` output.

---

## 4. BM25 Search Engine (`toolchain/search_engine_bm25.go`)

Uses `github.com/blevesearch/bleve/v2` with `NewMemOnly()`.

### IndexAll

- Close existing index if present (supports re-indexing)
- Create custom mapping with field-specific boosts:
  - **High (3.0)**: name, description
  - **Medium (2.0)**: keywords, categories
  - **Lower (1.0)**: domain, synthetic_queries
- Slice fields (Keywords, Categories, SyntheticQueries) joined with space for indexing
- Index each tool using `tool.Name()` as document ID

### Search

- `bleve.NewMatchQuery(query)` with large result size (return all matches)
- `SearchInContext(ctx, request)` for cancellation support
- Returns tool names from `hit.ID` in relevance order

### Dependency

```
go get github.com/blevesearch/bleve/v2
```

---

## 5. Regex Search Engine (`toolchain/search_engine_regex.go`)

### IndexAll

Stores each tool's searchable texts as flat string slices:
Name, Description, Domain + Keywords + Categories + SyntheticQueries.

### Search

- Compile query as case-insensitive regex: `(?i)` + query
- Count matches per tool across all searchable texts
- Sort by match count descending
- Return tool names
- Invalid regex returns descriptive error (surfaced to LLM)

---

## 6. Mutex Strategy

- `Initialize()` — write lock (rebuilds all state)
- `AvailableToolsPrompt()` — read lock
- `Execute()` — read lock (search + tool calls are read-only after init)
- `RegisterTool()` — no lock (called during single-goroutine setup before Initialize)

---

## 7. Implementation Order

1. `tool_search.go` — interfaces
2. `toolchain/search_engine_regex.go` + `search_engine_regex_test.go` — simpler engine first
3. Add Bleve dependency: `go get github.com/blevesearch/bleve/v2`
4. `toolchain/search_engine_bm25.go` + `search_engine_bm25_test.go`
5. `toolchain/search_prompt.go` — prompt builders
6. `toolchain/search.go` — main implementation
7. `toolchain/search_test.go` — comprehensive tests

---

## 8. Tests

### 8a. `search_engine_regex_test.go`

| Test | Scenarios |
|------|-----------|
| `TestRegex_Id` | Returns "regex" |
| `TestRegex_SearchGuidance` | Returns non-empty guidance with examples |
| `TestRegex_IndexAll` | Single tool; multiple tools; re-index (call twice); empty list |
| `TestRegex_Search` | Simple pattern; complex regex (dot-star); match across fields; ranking by match count; case insensitive; no matches → empty slice; invalid regex → descriptive error; match in name; match in keywords; match in synthetic queries; match in description; match in domain; match in categories |

### 8b. `search_engine_bm25_test.go`

| Test | Scenarios |
|------|-----------|
| `TestBM25_Id` | Returns "bm25" |
| `TestBM25_SearchGuidance` | Returns non-empty guidance with examples |
| `TestBM25_IndexAll` | Single tool; multiple tools; re-index (call twice); empty list |
| `TestBM25_Search` | Match by name; match by description; match by keywords; match by categories; match by domain; match by synthetic queries; no matches → empty slice; not initialized → error; multi-word query |

### 8c. `search_test.go`

**Constructor / Config:**

| Test | Scenarios |
|------|-----------|
| `TestSearchJSON_Name` | Default "action"; custom section name |
| `TestSearchJSON_Guidance` | Returns JSON format instructions |
| `TestSearchJSON_Config` | Default page size (3); custom page size; default no-results message; custom no-results message |

**RegisterTool:**

| Test | Scenarios |
|------|-----------|
| `TestSearchJSON_RegisterTool` | Valid indexable tool succeeds; panics on non-IndexableTool; panics on non-Tool (invalid type); panics on duplicate name; method chaining works; multiple tools registered correctly |

**Initialize:**

| Test | Scenarios |
|------|-----------|
| `TestSearchJSON_Initialize` | Success with one engine; success with multiple engines; error when no engines; error when engine IndexAll fails; re-initialize (call twice) updates state; computes correct domain summary with categories and counts |

**AvailableToolsPrompt:**

| Test | Scenarios |
|------|-----------|
| `TestSearchJSON_AvailableToolsPrompt` | Shows only search tool (not registered tools); includes domain summary; includes all engine IDs in query_type enum; includes per-engine search guidance; includes correct total tool count; schema has query, query_type, page properties |

**ParseSection:**

| Test | Scenarios |
|------|-----------|
| `TestSearchJSON_ParseSection` | Valid single JSON call; valid array of calls; invalid JSON; missing tool name; empty content returns empty slice; parse error publishes event and increments stats; successful parse resets consecutive gauge |

**Execute — Search Tool:**

| Test | Scenarios |
|------|-----------|
| `TestSearchJSON_Execute_Search` | Search returns results with full tool definitions (name, desc, policy, schema); pagination page 1 shows first N results; pagination page 2 shows next N; page beyond results returns no-results message; zero results returns configured no-results message; invalid query_type returns error; engine returns error (surfaced to LLM); schema validation error on search args (missing required field); multiple parallel searches in array |

**Execute — Regular Tools:**

| Test | Scenarios |
|------|-----------|
| `TestSearchJSON_Execute_RegularTool` | Tool call succeeds with typed result; unknown tool returns error; schema validation fails; tool Call returns error; tool with instructions creates nested sections; tool with media collects media |

**Execute — Mixed Parallel Calls:**

| Test | Scenarios |
|------|-----------|
| `TestSearchJSON_Execute_Mixed` | Search + regular tool in same array; two searches + regular tool; regular tool alongside search tool all succeed |

**Execute — Events & Stats:**

| Test | Scenarios |
|------|-----------|
| `TestSearchJSON_Execute_Stats` | Search call increments SCToolCalls and SCToolCallsFor; regular tool call increments correct SCToolCalls and SCToolCallsFor; parse error increments toolchain parse error counter and gauge; search error increments tool error stats; successful search resets consecutive error gauges; successful regular tool resets consecutive error gauges |

**Execute — Pagination Details:**

| Test | Scenarios |
|------|-----------|
| `TestSearchJSON_Execute_Pagination` | Page 1 of multi-page results shows correct subset + pagination info; page 2 shows next subset; last page shows remaining tools (partial page); page 0 treated as page 1; page beyond total shows no-results; pagination info format: "Showing page X of Y (N total results)" |

**Thread Safety:**

| Test | Scenarios |
|------|-----------|
| `TestSearchJSON_ConcurrentAccess` | Concurrent Execute calls succeed; concurrent Initialize + AvailableToolsPrompt don't race (use -race flag) |

---

## 9. Verification

1. `go test ./toolchain/... -v -run TestRegex` — regex engine tests
2. `go test ./toolchain/... -v -run TestBM25` — BM25 engine tests
3. `go test ./toolchain/... -v -run TestSearchJSON` — main toolchain tests
4. `go test ./toolchain/... -race` — race detector for concurrency
5. `go vet ./...` — static analysis
6. `go build ./...` — clean build
