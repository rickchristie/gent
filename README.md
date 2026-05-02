# Gent

Gent is a Go framework for building LLM agents that can be adapted to your own agent loop,
tool format, retrieval strategy, limits, and observability requirements.

The default ReAct agent is ready to use, but Gent is intentionally not a closed agent product.
It is a set of small interfaces and production-oriented building blocks for teams that want to
own their agent behavior in Go.

## Philosophy

Gent is built around a few design choices.

- Library first. Your application owns the process, storage, models, tools, and deployment.
- Custom loops are normal. ReAct is included, but `AgentLoop` is the core abstraction.
- Tools stay typed. Tools receive typed Go input and return typed Go output.
- Formatting is separate. Toolchains decide how LLM text becomes typed tool calls.
- Limits are first-class. Iterations, tokens, tool calls, parse errors, and validators are tracked.
- Streaming is always on. Model chunks can be observed in real time.
- Retrieval is local-friendly. Optional ONNX embedding support runs on CPU.
- Observability is built in. Execution contexts publish lifecycle, model, tool, limit, and diff
  events.

The intent is to make custom agents boring to operate: explicit state, explicit limits, typed
tools, visible events, and code you can debug.

## Install

Use the library in your application:

```bash
go get github.com/rickchristie/gent@v0.1.0
```

Install the optional CLI when you want local ONNX embedding models:

```bash
go install github.com/rickchristie/gent/cmd/gent@v0.1.0
printf '\nexport PATH="%s:$PATH"\n' "$(go env GOPATH)/bin" >> ~/.bashrc
source ~/.bashrc
```

`go install` writes the `gent` binary to `$(go env GOPATH)/bin`. Add that directory to your
shell profile once so future terminals can run `gent`. Use `~/.zshrc` instead of `~/.bashrc` if
you use zsh.

## Quick Start: ReAct Agent With A Typed Tool

Create a provider model with LangChainGo, wrap it with Gent, register typed tools, and run the
executor.

```go
package main

import (
    "context"
    "fmt"

    "github.com/rickchristie/gent"
    "github.com/rickchristie/gent/agents/react"
    "github.com/rickchristie/gent/executor"
    "github.com/rickchristie/gent/models"
    "github.com/rickchristie/gent/schema"
    "github.com/rickchristie/gent/termination"
    "github.com/rickchristie/gent/toolchain"
    "github.com/tmc/langchaingo/llms"
)

type LookupOrderInput struct {
    OrderID string `json:"order_id"`
}

type OrderStatus struct {
    ID     string `json:"id"`
    Status string `json:"status"`
}

func runAgent(ctx context.Context, llm llms.Model) error {
    model := models.NewLCGWrapper(llm).WithModelName("support-model")

    lookupOrder := gent.NewToolFunc(
        "lookup_order",
        "Look up order status by order ID.",
        schema.Object(map[string]*schema.Property{
            "order_id": schema.String("Order ID, for example ORD-123"),
        }, "order_id"),
        func(ctx context.Context, input LookupOrderInput) (OrderStatus, error) {
            return OrderStatus{ID: input.OrderID, Status: "shipped"}, nil
        },
    )

    tc := toolchain.NewYAML().RegisterTool(lookupOrder)
    term := termination.NewText("answer").
        WithGuidance("Write a concise answer for the user.")

    agent := react.NewAgent(model).
        WithBehaviorAndContext("You are a careful customer support agent.").
        WithToolChain(tc).
        WithTermination(term)

    data := gent.NewBasicLoopData(&gent.Task{Text: "Where is order ORD-123?"})
    execCtx := gent.NewExecutionContext(ctx, "support", data)
    execCtx.SetLimits([]gent.Limit{
        {Type: gent.LimitExactKey, Key: gent.SCIterations.Self(), MaxValue: 8},
        {Type: gent.LimitExactKey, Key: gent.SCInputTokens, MaxValue: 50000},
        {Type: gent.LimitExactKey, Key: gent.SCOutputTokens, MaxValue: 20000},
    })

    exec := executor.New[*gent.BasicLoopData](agent, executor.DefaultConfig())
    exec.Execute(execCtx)

    if err := execCtx.Error(); err != nil {
        return err
    }
    if execCtx.TerminationReason() != gent.TerminationSuccess {
        return fmt.Errorf("agent ended with %s", execCtx.TerminationReason())
    }

    for _, part := range execCtx.FinalResult() {
        if text, ok := part.(llms.TextContent); ok {
            fmt.Println(text.Text)
        }
    }
    return nil
}
```

The `llm` value can be any LangChainGo `llms.Model`, such as an OpenAI, Anthropic, Google,
Ollama, or GitHub Models client. Gent's `models.LCGWrapper` adapts it to Gent's streaming model
interface and normalizes model-call stats.

## Main Concepts

### Executor

`executor.Executor` owns the lifecycle:

```text
BeforeExecution -> [BeforeIteration -> AgentLoop.Next -> AfterIteration]* -> AfterExecution
```

It stops when the loop returns `LATerminate`, a limit is exceeded, the context is canceled, or an
error occurs.

### AgentLoop

`gent.AgentLoop` is the core interface:

```go
type AgentLoop[Data gent.LoopData] interface {
    Next(execCtx *gent.ExecutionContext) (*gent.AgentLoopResult, error)
}
```

Use `agents/react` when a ReAct loop fits. Implement `AgentLoop` yourself when you want a planner,
router, graph, multi-agent coordinator, deterministic workflow, or a domain-specific loop.

### LoopData

`gent.BasicLoopData` stores the original task, full iteration history, and scratchpad. Embed it in
your own struct when your loop needs more state:

```go
type SessionData struct {
    gent.BasicLoopData
    UserID string
}
```

### Model

Gent models stream content through `GenerateContentStream`. The LangChainGo wrapper is the common
starting point:

```go
model := models.NewLCGWrapper(llm).WithModelName("gpt-4.1")
```

You can implement `gent.Model` directly if you use another provider SDK.

### Tools

Tools are plain Go business logic with JSON Schema parameters:

```go
tool := gent.NewToolFunc(
    "search_orders",
    "Search orders by customer email.",
    schema.Object(map[string]*schema.Property{
        "email": schema.String("Customer email address"),
    }, "email"),
    func(ctx context.Context, input SearchOrdersInput) (SearchOrdersResult, error) {
        return searchOrders(ctx, input.Email)
    },
)
```

Add `WithPolicy` when the model needs dynamic usage rules:

```go
tool = tool.WithPolicy("Only search by the exact email supplied by the current user.")
```

### ToolChain

Toolchains define how model text becomes tool calls. The default ReAct agent uses YAML tool calls,
which are forgiving for LLMs:

```go
tc := toolchain.NewYAML().RegisterTool(tool)
```

Use JSON toolchains when you want stricter syntax or `SearchJSON` when the tool catalog is too
large to show in every prompt.

### Termination

Termination decides when the model has produced a final answer:

```go
term := termination.NewText("answer").
    WithGuidance("Write the final answer here.")
```

Use `termination.NewJSON[T]` when the final answer must be typed JSON, and attach validators when
answers need business approval before acceptance.

### Limits And Stats

Limits are configured on `ExecutionContext`, not the executor. They can cap iterations, token
usage, tool calls, parse errors, tool errors, and validator rejections.

```go
execCtx.SetLimits([]gent.Limit{
    {Type: gent.LimitExactKey, Key: gent.SCIterations.Self(), MaxValue: 10},
    {Type: gent.LimitExactKey, Key: gent.SCInputTokens, MaxValue: 100000},
    {Type: gent.LimitKeyPrefix, Key: gent.SCToolCallsFor, MaxValue: 20},
})
```

Counters propagate to parent contexts. Use `StatKey.Self()` for per-context limits.

## Local Embeddings And Vector Search

Gent can run semantic search locally on CPU with ONNX Runtime. This is useful for policy search,
tool search, small knowledge bases, and applications that should not depend on a hosted vector
service.

### 1. Install Native Dependencies

```bash
go install github.com/rickchristie/gent/cmd/gent@v0.1.0
printf '\nexport PATH="%s:$PATH"\n' "$(go env GOPATH)/bin" >> ~/.bashrc
source ~/.bashrc
gent setup onnx
source ~/.bashrc  # or ~/.zshrc, then restart terminal
```

`gent setup onnx` installs `libtokenizers` and ONNX Runtime into `~/.gent/lib/`. When you accept
the profile update prompt, it appends these ONNX library paths to `~/.bashrc`:

```bash
export CGO_LDFLAGS="-L$HOME/.gent/lib"
export LD_LIBRARY_PATH="$HOME/.gent/lib:$LD_LIBRARY_PATH"
```

If you decline the prompt, add those lines manually, then run `source ~/.bashrc` or restart your
terminal. Use `~/.zshrc` instead of `~/.bashrc` if you use zsh.

### 2. Pick And Download A Model

```bash
gent model list
gent model download multilingual-e5-small
```

`multilingual-e5-small` is the recommended default. It is compact enough for local CPU use and
works across many languages.

### 3. Create An Embedder

```go
cfg := common.FindConfig("multilingual-e5-small")
if cfg == nil {
    return fmt.Errorf("unknown embedding config")
}

dir, err := common.ModelDir(cfg.Model.Name)
if err != nil {
    return err
}

embedder, err := search.NewOnnxEmbedder(*cfg, search.OnnxOptions{
    ModelPath:      filepath.Join(dir, cfg.Model.ModelFile),
    TokenizerPath:  filepath.Join(dir, "tokenizer.json"),
    NumThreads:     4,
    MaxConcurrency: 4,
})
if err != nil {
    return err
}
defer embedder.Close()
```

The `gent model download <model-name>` command prints this snippet for each supported runtime
configuration.

### 4. Use Vector Search Directly

Create a `ChunkAdapter` for your document type, then index and search.

```go
type Article struct {
    Title string
    Body  string
}

type ArticleAdapter struct{}

func (ArticleAdapter) Chunks(
    article Article,
    counter search.TokenCounter,
    _ int,
) ([]search.Chunk, error) {
    chunker := &search.MarkdownChunker{
        ChunkSize:  384,
        TokenCount: counter.TokenCount,
    }
    return chunker.Chunk("# " + article.Title + "\n\n" + article.Body), nil
}

index := search.NewFlatIndex[Article](ArticleAdapter{}, embedder)

err = index.Swap(ctx, map[string]Article{
    "refund-policy": {
        Title: "Refund Policy",
        Body:  "Refunds are processed within 5-7 business days.",
    },
})
if err != nil {
    return err
}

results, err := index.Search(ctx, "how long do refunds take?", 5)
if err != nil {
    return err
}
```

For larger or keyword-sensitive corpora, combine `FlatIndex` with `BleveIndex` through
`FusedIndex`. BM25 handles exact terms and identifiers; semantic search handles paraphrases.

### 5. Use Search For Tool Discovery

For large tool catalogs, avoid placing every tool schema in every prompt. Use searchable tools:

```go
searcher := toolchain.NewFusedToolSearcher(embedder)
tc := toolchain.NewSearchJSON(toolchain.SearchHintDomainCategories).
    RegisterEngine(searcher)

tc.RegisterTool(indexableTool)
if err := tc.Initialize(); err != nil {
    return err
}

agent := react.NewAgent(model).WithToolChain(tc)
```

Tools registered with `SearchJSON` must implement both `gent.Tool[I, O]` and
`gent.IndexableTool`. The extra metadata lets Gent index the tool by domain, category, keyword,
description, and synthetic example queries.

## Customization Guide

Start with the default ReAct stack, then replace only the layer you need.

- Replace the model when you change providers or want a custom SDK integration.
- Replace the toolchain when you want YAML, JSON, searchable tools, or programmatic tool calling.
- Replace termination when the final output should be plain text, typed JSON, or validator-gated.
- Replace the system prompt builder when your domain needs a different prompt structure.
- Replace `AgentLoop` when ReAct is not the right control flow.
- Add limits before production traffic.
- Subscribe to events for logging, metrics, tracing, and debugging.

The smallest useful customization is usually a typed tool plus a few precise system instructions.
The largest customization is a fully custom `AgentLoop` that still reuses Gent's executor, context,
stats, limits, toolchains, and model wrappers.

## Production Notes

- Always pass real `context.Context` values; Gent propagates cancellation to models and tools.
- Keep tool descriptions short and concrete. Put usage rules in `Tool.Policy()`.
- Add exact limits for iterations and tokens before deploying an agent loop.
- Prefer `SCIterations.Self()` for per-agent iteration limits.
- Use searchable toolchains when the catalog grows beyond what should be in every prompt.
- Use `GENT_ORT_LIB` in containers when ONNX Runtime is installed outside `~/.gent/lib/`.
- Call `Close()` on embedders when the application shuts down.

## Repository Map

- `agent.go`: core `AgentLoop`, `LoopData`, task, and iteration types.
- `context.go`: execution context, events, stats, limits, streams, and children.
- `executor/`: executor lifecycle implementation.
- `agents/react/`: default ReAct agent.
- `toolchain/`: YAML, JSON, searchable, and programmatic toolchains.
- `termination/`: text and JSON answer termination.
- `models/`: LangChainGo model wrapper.
- `search/`: ONNX embeddings, vector search, BM25, fusion, and chunking.
- `cmd/gent/`: setup and model download CLI.

## More Detail

- Local embedding setup and search details: `search/README.md`
- Executor and limit testing standards: `agents/standard-executor-test.md`
