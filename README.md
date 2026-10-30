# Gent

Gent is a Go framework for building LLM agents that can be adapted to your own agent loop,
tool selection, retrieval strategy, limits, and observability requirements.

The default ReAct agent is ready to use, but Gent is intentionally not a closed agent product.
It is a set of small interfaces and production-oriented building blocks for teams that want to
own their agent behavior in Go.

## Philosophy

Gent is built around a few design choices.

- Library first. Your application owns the process, storage, models, tools, and deployment.
- Custom loops are normal. ReAct is included, but `Loop[T]` is the core abstraction.
- Tools stay typed. Tools receive typed Go input and return typed Go output.
- Use native APIs when they provide the requested behavior. Local output validation always runs.
- Tools use native API calls. A single contract covers direct tools, search, and JavaScript.
- Code stays simple. Remove unused code and interfaces instead of keeping compatibility paths.
- Limits are first-class. Iterations, tokens, tool calls, tool errors, and output rejections are tracked.
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

## Quick Start: Native Agent With A Typed Tool

Create a provider model, register typed tools, and run the executor.

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
    "github.com/rickchristie/gent/output"
    "github.com/rickchristie/gent/toolchain"
)

type LookupOrderInput struct {
    OrderID string `json:"order_id"`
}

type OrderStatus struct {
    ID     string `json:"id"`
    Status string `json:"status"`
}

func runAgent(ctx context.Context, apiKey, modelName string) error {
    model := models.NewOpenAI(apiKey, modelName)

    lookupOrder, err := gent.DefineTool("lookup_order",
        func(ctx context.Context, input *LookupOrderInput) (*OrderStatus, error) {
            return &OrderStatus{ID: input.OrderID, Status: "shipped"}, nil
        }).Description("Look up order status by order ID.").
        Parameters(schema.Field("order_id").Description("Order ID, for example ORD-123")).
        Build()
    if err != nil {
        return err
    }

    tc := toolchain.New().RegisterTool(lookupOrder)
    out, err := output.NewJsonOutput[OrderStatus]()
    if err != nil {
        return err
    }
    agent := react.NewWithOutput(model, out).WithToolChain(tc)
    task := &gent.Task{Messages: []*gent.ModelMessage{
        gent.TextParts(gent.ChatMessageTypeSystem,
            "You are a careful customer support agent. " + out.Instructions()),
        gent.TextParts(gent.ChatMessageTypeHuman, "Where is order ORD-123?"),
    }}
    data := gent.NewBasicLoopData(task)
    execCtx := gent.NewExecutionContext(ctx, "support", data)
    execCtx.SetLimits([]gent.Limit{
        {Type: gent.LimitExactKey, Key: gent.SCIterations.Self(), MaxValue: 8},
        {Type: gent.LimitExactKey, Key: gent.SCInputTokens, MaxValue: 50000},
        {Type: gent.LimitExactKey, Key: gent.SCOutputTokens, MaxValue: 20000},
    })

    runner := executor.NewRunner(agent, executor.DefaultConfig())
    order, result, err := runner.Execute(execCtx)
    if err != nil {
        return err
    }
    if result.TerminationReason != gent.TerminationSuccess {
        return fmt.Errorf("agent ended with %s", result.TerminationReason)
    }
    fmt.Println(order.ID, order.Status)
    return nil
}
```

`NewWithOutput` infers `OrderStatus` as the result type. `NewAgent` uses `TextOutput` and returns
`string`. The caller writes the complete system and user messages. Include `Instructions()` when
useful; Gent does not append it. See [output and prompt ownership](docs/output.md),
[provider adapters](docs/models.md), and [native calls and recovery](docs/native-tools.md).

## Main Concepts

### Executor

`executor.Runner[T]` owns the lifecycle:

```text
BeforeExecution -> [BeforeIteration -> Loop.Next -> AfterIteration]* -> AfterExecution
```

It stops when the loop returns `LATerminate`, a limit is exceeded, the context is canceled, or an
error occurs.

### Loop

`gent.Loop[T]` is the core interface:

```go
type Loop[T any] interface {
    Next(execCtx *gent.ExecutionContext) (*gent.AgentLoopResult, error)
    Output() gent.Output[T]
}
```

Use `agents/react` when a ReAct loop fits. Implement `Loop[T]` yourself when you want a planner,
router, graph, multi-agent coordinator, deterministic workflow, or a domain-specific loop.

Use `executor.NewRunner(loop, config)`; Go infers `T` from the loop. `Execute` and `Resume` return
`(T, *gent.ExecutionResult, error)`. Check the terminal reason before using the value: refusal,
cancellation, and a run limit do not return an accepted value.

### LoopData

`gent.BasicLoopData` supplies the task. `ExecutionContext` owns immutable history and an ordered
selection of `*HistoryEntry` records for working context. Use `AppendRecord`, `UpdateHistory`, and
`Scratchpad` for those operations. Embed the basic data in custom data when your loop needs state;
implement `CheckpointState` to make that state recoverable:

```go
type SessionData struct {
    gent.BasicLoopData
    UserID string
}
```

### History and continuation

`ExecutionContext` accepts typed history, activity, and checkpoints together. Supply application
storage with `gent.WithRecorder` and attach a `trace.Sequencer` with `gent.WithObserver`. Use
`CodecRegistry.Marshal`/`Unmarshal` for saved runs and `Runner.Resume` for continuation.
Completed tools and saved model decisions survive
worker replacement. Uncertain tool outcomes require an explicit recovery decision.

See [durable history and continuation](docs/history.md) and the runnable
[Go example](examples/history/main.go). Run it with `go run ./examples/history`.

For an interactive guide, open the [Gent quick start](docs/quickstart/dist/gent-quick-start.html)
in your browser. It works offline and covers a simple agent, a saved streaming API, a React client,
and run recovery. See the [source and build instructions](docs/quickstart/README.md).

### Model

Gent models stream content through `GenerateContentStream`. OpenAI and Grok use direct Responses
adapters that preserve provider continuation items:

```go
model := models.NewOpenAI(apiKey, modelName)
```

You can implement `gent.Model` directly if you use another provider SDK. Provider text streams as
ordered `MetaContent` entries (`intermediary`, `reasoning`, `commentary`, or a new string type).
`ContentChoice.CandidateContent` is the candidate that the agent must check. The executor publishes
one complete `StreamChunk.Content` after answer acceptance and the terminal save. Source message IDs
let a frontend replace the provisional answer. See the [streaming
contract](docs/native-tools.md#frontend-and-storage-contract).

### Tools

Use `gent.DefineTool` to build a tool from a typed function or a bound service method:

```go
tool, err := gent.DefineTool("search_orders", service.SearchOrders).
    Description("Search orders by customer email.").
    Parameters(schema.Field("email").Description("Customer email address").Format("email")).
    Policy("Only search by the exact email supplied by the current user.").
    Domain("Orders").Categories("lookup").Keywords("email", "orders").
    Build()
if err != nil {
    return err
}
```

The builder uses Go types and `json` field names for the parameter schema. Fields are required by
default; use `schema.Field("name").Optional()` to permit an absent field. `omitempty` does not make
an input optional. `Build` checks schemas and copies configuration. `MustBuild` is available for
fixed definitions where a configuration error should stop application startup.

Set `Recovery(handler)`, `DeduplicateSummary(handler)`, `SyntheticQueries(...)`, `Strict(bool)`, or
`OutputSchema(raw)` before building. `ParameterSchema(raw)` supplies a complete schema when type
inference does not fit your input. It cannot be combined with `Parameters(...)`. For a tool with
its own fields, media, or dynamic instructions, implement the full `gent.Tool[I, O]` interface.
See [tool definitions](docs/native-tools.md#tool-definitions) for the complete contract.

### ToolChain

Toolchains expose native definitions and execute saved call plans:

```go
tc := toolchain.New().RegisterTool(tool)
```

Use `toolchain.NewSearch` for a large catalog or wrap a chain with `NewJsToolChainWrapper` for
programmatic calls. Custom chains can embed `Registry` and override selection. All chains use native
calls, hooks, typed inputs, and recovery. Each tool's `Recover` method returns a decision, optional
steering, and the recovered output when complete. The builder's default is unresolved. Gent saves
that decision before using it; saved results always take precedence.

### Output

`Output[T]` configures native generation and processes a complete candidate. The four built-ins
are `TextOutput`, `ImageOutput`, `JsonOutput[T]`, and `SchemaOutput[T]`.

```go
out, err := output.NewJsonOutput[*OrderStatus](
    schema.Field("status").Enum("pending", "shipped", "delivered"),
)
if err != nil {
    return err
}
agent := react.NewWithOutput(model, out)
```

Local checks always run, followed by an optional typed custom validator. Rejection returns explicit
steering and retains the candidate in history. Correction uses normal run limits. Native refusal
ends the run separately. A successful checkpoint saves the typed value and delivery content before
publishing accepted output. See [Output](docs/output.md) for validators, images, and custom processors.

### Limits And Stats

Limits are configured on `ExecutionContext`, not the executor. They can cap iterations, token
usage, tool calls, tool argument errors, tool errors, and output rejections.

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
tc := toolchain.NewSearch(toolchain.SearchHintDomainCategories).
    RegisterEngine(searcher)

tc.RegisterTool(indexableTool)
if err := tc.Initialize(); err != nil {
    return err
}

agent := react.NewAgent(model).WithToolChain(tc)
```

Search metadata is part of every `gent.Tool[I, O]`. Set the builder's domain, categories, keywords,
and example queries to help the search engine. `gent.IndexableTool` is the metadata view used by
search engines; a separate wrapper is not needed.

## Customization Guide

Start with the default ReAct stack, then replace only the layer you need.

- Replace the model when you change providers or want a custom SDK integration.
- Replace tool selection when you need search, custom visibility, or programmatic tool calling.
- Select an `Output[T]` for text, images, typed JSON, or a custom contract.
- Compose system instructions and user attachments in `Task.Messages`.
- Replace `Loop[T]` when ReAct is not the required control flow.
- Add limits before production traffic.
- Subscribe to events for logging, metrics, tracing, and debugging.

The smallest useful customization is usually a typed tool plus a few precise system instructions.
The largest customization is a fully custom `Loop[T]` that still reuses Gent's executor, context,
stats, limits, toolchains, and model wrappers.

Compose time-dependent prompt text in the caller. `TimeProviderFunc` and `MockTimeProvider` can
supply a custom or fixed clock to that code. Use a request hook for changing context.
Gent does not expand prompt templates. Ordinary execution does not require `WithRecoveryVersion`;
set it before the original run when callback or tool behavior needs a stable recovery identity.

## Production Notes

- Always pass real `context.Context` values; Gent propagates cancellation to models and tools.
- Keep tool descriptions short and concrete. Put usage rules in `Tool.Policy()`.
- Add exact limits for iterations and tokens before deploying an agent loop.
- Prefer `SCIterations.Self()` for per-agent iteration limits.
- Use searchable toolchains when the catalog grows beyond what should be in every prompt.
- Use `GENT_ORT_LIB` in containers when ONNX Runtime is installed outside `~/.gent/lib/`.
- Call `Close()` on embedders when the application shuts down.

## Repository Map

- `agent.go`: core `Loop[T]`, `LoopData`, task, and iteration types.
- `context.go`: execution context, events, stats, limits, streams, and children.
- `executor/`: executor lifecycle implementation.
- `agents/react/`: default ReAct agent.
- `toolchain/`: native registry, search, and JavaScript toolchains.
- `output/`: text, image, and typed JSON output processing.
- `models/`: ten native provider and local runtime adapters.
- `search/`: ONNX embeddings, vector search, BM25, fusion, and chunking.
- `cmd/gent/`: setup and model download CLI.

## More Detail

- Local embedding setup and search details: `search/README.md`
- Executor and limit testing standards: `agents/standard-executor-test.md`
