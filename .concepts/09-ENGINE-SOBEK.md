# Sobek's ES module system: a complete technical deep dive

**Grafana's Sobek fork of Goja does support ES Modules — and ESM was the primary reason for the fork.** The engine implements spec-compliant `import`/`export` through a set of experimental Go APIs centered on `ParseModule()`, `HostResolveImportedModuleFunc`, and a four-phase lifecycle (Parse → Link → Instantiate → Evaluate). CommonJS `require()` is also available through the companion `sobek_nodejs/require` package, a direct fork of `dop251/goja_nodejs`. Critically, ESM evaluation returns a `*Promise` and **requires an event loop** for modules with top-level await — Sobek intentionally does not provide one. The API is explicitly marked experimental and will change in breaking ways; the only official documentation is the test files `modules_test.go` and `modules_integration_test.go`.

---

## ESM was the fork's raison d'être

The k6 v0.52.0 release notes (June 2024) state it plainly: *"To accelerate the development speed and bring ECMAScript Modules (ESM) support to k6 earlier (#3265), we have decided to create a fork of the goja project under the Grafana GitHub organization, named sobek."* Before the fork, k6 relied on **Babel.js transpilation** to convert ESM `import`/`export` statements into CommonJS `require()` calls — a workaround that caused performance and compatibility problems documented in k6 issues #824 and #2168.

**Upstream Goja (`dop251/goja`) has zero ESM support.** It only provides CommonJS through the external `goja_nodejs/require` package. Sobek adds an entire ESM layer at the engine level while maintaining full backward compatibility with Goja through regular upstream merges. The primary developer, **@mstoykov** (Mihail Stoykov), authored nearly all module-related PRs including #61 (array destructuring in exports), #41 (default class export fix), and #12 (ambiguous import error improvements). Open issues track remaining work: #74 (JSON modules), #73 (import attributes), and #38 (namespace object comparison bug).

The Sobek README acknowledges the trade-offs directly: *"ESM was implemented and merged in this fork to facilitate the needs of k6 as such some compromises were taken in favor of time. In general the API is very close to the specification, which unfortunately does not translate greatly to actual usage."*

---

## The complete ESM API surface

Sobek's ESM implementation follows the ECMAScript specification closely, exposing a hierarchy of interfaces and concrete types. Here is every module-related public API element:

### Core interfaces

**`ModuleRecord`** is the base interface matching the spec's abstract Module Record:

```go
type ModuleRecord interface {
    GetExportedNames(callback func([]string), resolveset ...ModuleRecord) bool
    ResolveExport(exportName string, resolveset ...ResolveSetElement) (*ResolvedBinding, bool)
    Link() error
    Evaluate(*Runtime) *Promise
}
```

**`CyclicModuleRecord`** extends it for ESM-style modules with circular dependency support:

```go
type CyclicModuleRecord interface {
    ModuleRecord
    RequestedModules() []string
    InitializeEnvironment() error
    Instantiate(rt *Runtime) (CyclicModuleInstance, error)
}
```

**`ModuleInstance`** and **`CyclicModuleInstance`** represent instantiated modules in a specific runtime:

```go
type ModuleInstance interface {
    GetBindingValue(string) Value
}

type CyclicModuleInstance interface {
    ModuleInstance
    HasTLA() bool
    ExecuteModule(rt *Runtime, res, rej func(interface{}) error) (CyclicModuleInstance, error)
}
```

### The host resolution callback — the key extension point

**`HostResolveImportedModuleFunc`** is the single most important type for embedders. Sobek calls this function every time it encounters an `import` statement during linking:

```go
type HostResolveImportedModuleFunc func(
    referencingScriptOrModule interface{},
    specifier string,
) (ModuleRecord, error)
```

The first argument is the `ModuleRecord` of the importing module (or `nil` for the entry point). The second is the raw specifier string from the `import` statement. Your implementation must return a `ModuleRecord` — typically by parsing additional source files with `ParseModule()` and caching the results.

### Construction functions

```go
func ParseModule(name, sourceText string, resolveModule HostResolveImportedModuleFunc,
    opts ...parser.Option) (*SourceTextModuleRecord, error)

func ModuleFromAST(body *ast.Program,
    resolveModule HostResolveImportedModuleFunc) (*SourceTextModuleRecord, error)
```

### Runtime methods for module lifecycle

```go
func (r *Runtime) CyclicModuleRecordEvaluate(c CyclicModuleRecord,
    resolve HostResolveImportedModuleFunc) *Promise
func (r *Runtime) GetModuleInstance(m ModuleRecord) ModuleInstance
func (r *Runtime) NamespaceObjectFor(m ModuleRecord) *Object
func (r *Runtime) GetActiveScriptOrModule() interface{}
func (r *Runtime) SetImportModuleDynamically(callback ImportModuleDynamicallyCallback)
func (r *Runtime) FinishLoadingImportModule(referrer, specifier, payload interface{},
    result ModuleRecord, err error)
func (r *Runtime) SetGetImportMetaProperties(fn func(ModuleRecord) []MetaProperty)
func (r *Runtime) SetFinalImportMeta(fn func(*Object, ModuleRecord))
```

**Dynamic `import()` expressions**, **`import.meta` properties**, and **top-level await** are all supported through these runtime hooks.

---

## The four-phase module lifecycle in practice

Loading an ES module in Sobek requires four distinct phases. Here is a complete working example that demonstrates a multi-module setup with dependency resolution:

```go
package main

import (
    "fmt"
    "log"
    "github.com/grafana/sobek"
)

// Virtual filesystem of ES modules
var sources = map[string]string{
    "math.mjs": `
        export function square(x) { return x * x; }
        export const PI = 3.14159265;
    `,
    "app.mjs": `
        import { square, PI } from "math.mjs";
        export const area = PI * square(5);
    `,
}

var cache = map[string]*sobek.SourceTextModuleRecord{}

// resolveModule is called by Sobek whenever it encounters an import statement
func resolveModule(referencingScriptOrModule interface{}, specifier string) (sobek.ModuleRecord, error) {
    if mod, ok := cache[specifier]; ok {
        return mod, nil  // Return cached module to handle circular deps
    }
    src, ok := sources[specifier]
    if !ok {
        return nil, fmt.Errorf("module %q not found", specifier)
    }
    // PHASE 1: PARSE — converts source text to a SourceTextModuleRecord
    mod, err := sobek.ParseModule(specifier, src, resolveModule)
    if err != nil {
        return nil, err
    }
    cache[specifier] = mod
    return mod, nil
}

func main() {
    rt := sobek.New()

    entry, _ := sobek.ParseModule("app.mjs", sources["app.mjs"], resolveModule)
    cache["app.mjs"] = entry

    // PHASE 2: LINK — resolves all import/export bindings across the graph
    if err := entry.Link(); err != nil {
        log.Fatal("Link:", err)
    }

    // PHASE 3: INSTANTIATE — creates environment records in the runtime
    if _, err := entry.Instantiate(rt); err != nil {
        log.Fatal("Instantiate:", err)
    }

    // PHASE 4: EVALUATE — executes module code, returns a Promise
    promise := entry.Evaluate(rt)
    if promise.State() != sobek.PromiseStateFulfilled {
        log.Fatal("Evaluate:", promise.Result())
    }

    // Access exports via namespace object
    ns := rt.NamespaceObjectFor(entry)
    fmt.Println("area:", ns.Get("area"))  // ~78.54
}
```

**The critical insight**: your `HostResolveImportedModuleFunc` is called recursively during `Link()`. When Sobek links `app.mjs` and encounters `import { square, PI } from "math.mjs"`, it calls your resolver, which parses `math.mjs` and returns its `ModuleRecord`. Caching is essential both for performance and to correctly handle circular dependencies.

---

## Loading external JavaScript libraries like lodash

There are two practical approaches for making third-party libraries available to Sobek-hosted code.

### Approach 1: Bundle to ESM and register as a virtual module

Bundle the library into a single ESM file using esbuild or Rollup, then embed it in your Go binary:

```go
import _ "embed"

//go:embed lodash.bundle.mjs  // produced by: esbuild --bundle --format=esm lodash-es
var lodashSource string

var sources = map[string]string{
    "lodash": lodashSource,
}

// Then use the same resolveModule pattern shown above
```

JavaScript code can then `import { chunk, uniq } from "lodash"` naturally. This is the approach most aligned with Sobek's ESM system.

### Approach 2: CommonJS via sobek_nodejs/require

For CommonJS-format libraries, use the `sobek_nodejs/require` package — Sobek's fork of `dop251/goja_nodejs/require`:

```go
import (
    "github.com/grafana/sobek"
    "github.com/grafana/sobek_nodejs/require"
)

registry := require.NewRegistry(
    require.WithLoader(func(path string) ([]byte, error) {
        // Return source bytes for CommonJS modules
        if src, ok := myModules[path]; ok {
            return []byte(src), nil
        }
        return nil, require.ModuleFileDoesNotExistError
    }),
)

rt := sobek.New()
registry.Enable(rt)  // Adds require() to the runtime's global scope

rt.RunString(`var _ = require("./lodash.cjs"); var result = _.chunk([1,2,3,4], 2);`)
```

### Approach 3: Go-native modules via RegisterNativeModule

For functionality implemented in Go, register it as a native CommonJS module:

```go
registry.RegisterNativeModule("my-utils", func(runtime *sobek.Runtime, module *sobek.Object) {
    exports := module.Get("exports").(*sobek.Object)
    exports.Set("multiply", func(call sobek.FunctionCall) sobek.Value {
        a := call.Argument(0).ToFloat()
        b := call.Argument(1).ToFloat()
        return runtime.ToValue(a * b)
    })
})
```

---

## How k6 builds its module system on Sobek

k6 is the reference implementation for building a full module system on top of Sobek, implemented primarily in `js/modules/modules.go` and `js/modules/resolution.go`. Its architecture provides the blueprint for any serious Sobek embedding.

### The module resolution stack

k6 defines a **`ModuleResolver`** that wraps Sobek's raw `HostResolveImportedModuleFunc` with a layered resolution strategy. When Sobek encounters an `import` statement, k6's resolver checks these sources in order:

- **Go modules** — A `map[string]any` of import specifier → Go module (e.g., `"k6/http"` → the HTTP testing module). Go modules implement the `modules.Module` interface and are wrapped as Sobek `CyclicModuleRecord` objects via internal `wrappedGoModule` types.
- **Local file imports** — Relative paths like `./helpers.js` resolved against the importing module's URL using `url.URL`. Source loaded via a `FileLoader` callback from the filesystem.
- **Remote URL imports** — Full URLs like `https://jslib.k6.io/k6-utils/1.2.0/index.js` fetched over HTTP and cached.

The `ModuleResolver` constructor takes all these components:

```go
func NewModuleResolver(
    goModules map[string]any,
    loadCJS FileLoader,         // func(*url.URL, string) ([]byte, error)
    c *compiler.Compiler,       // esbuild-based compiler
    base *url.URL,
    u *usage.Usage,
    logger logrus.FieldLogger,
) *ModuleResolver
```

### Per-VU module instances

k6 creates a **`ModuleSystem`** per virtual user (VU), wrapping the shared `ModuleResolver`:

```go
func NewModuleSystem(resolver *ModuleResolver, vu VU) *ModuleSystem
```

Each `ModuleSystem` maintains its own module instance cache. The `VU` interface gives module instances access to the Sobek `Runtime`, context, state, and a callback registration mechanism for async operations.

### The Go module interface pattern

k6 extensions implement this interface hierarchy:

```go
type Module interface {
    NewModuleInstance(VU) Instance  // Factory: called once per VU
}

type Instance interface {
    Exports() Exports  // Returns the module's exports
}

type Exports struct {
    Default interface{}            // default export
    Named   map[string]interface{} // named exports
}
```

Extension registration happens at init time: `modules.Register("k6/x/myext", &RootModule{})`. The `"k6/x/"` prefix is enforced for third-party extensions.

### ESM is native, not transpiled

Since k6 v0.52.0, **`import`/`export` statements are handled natively by Sobek** — no Babel transpilation. k6 uses esbuild only for TypeScript type-stripping and modern syntax downleveling. The resolver provides Sobek's `HostResolveImportedModuleFunc`, which Sobek calls during the Link phase. CommonJS `require()` remains supported through `ModuleSystem.Require()` for backward compatibility, but mixing CJS and ESM in the same file is explicitly deprecated.

---

## The sobek_nodejs/require package for CommonJS

Sobek maintains `github.com/grafana/sobek_nodejs` as a direct fork of `dop251/goja_nodejs`, with all references updated to use `grafana/sobek` types. The `require` sub-package provides a full Node.js-compatible CommonJS implementation.

The central type is **`Registry`**, which is thread-safe and can be shared across multiple runtimes:

```go
registry := require.NewRegistry(
    require.WithLoader(sourceLoader),           // custom source loader
    require.WithPathResolver(pathResolver),      // custom path resolution
    require.WithGlobalFolders("/usr/lib/node"), // additional search paths
)
```

**`SourceLoader`** (`func(path string) ([]byte, error)`) controls where module source code comes from — the filesystem, embedded files, an in-memory map, or any other source. **`ModuleLoader`** (`func(*sobek.Runtime, *sobek.Object)`) implements Go-native modules that set properties on the `module.exports` object.

Internally, `require()` wraps each source file in the standard Node.js module wrapper `(function(exports, require, module, __filename, __dirname) { ... })`, compiles it, caches the compiled `*Program` in the shared Registry, and maintains per-runtime instance caches to prevent re-execution. JSON files are automatically parsed via `JSON.parse()`.

---

## What Sobek adds that Goja lacks

The differences are entirely in the ESM domain. Here is a precise comparison of module-related capabilities:

| Capability | Goja (`dop251/goja`) | Sobek (`grafana/sobek`) |
|---|---|---|
| **ESM `import`/`export`** | Not supported | Full experimental support |
| **`ParseModule()`** | Does not exist | Core function for ESM parsing |
| **`SourceTextModuleRecord`** | Does not exist | Full spec-compliant implementation |
| **`CyclicModuleRecord` interface** | Does not exist | Complete with cycle detection |
| **Dynamic `import()`** | Not supported | Via `ImportModuleDynamicallyCallback` |
| **`import.meta`** | Not supported | Via `MetaProperty` + runtime hooks |
| **Top-level await** | Not supported | Supported via `HasTLA()` + Promise |
| **Module namespace objects** | Not applicable | `NamespaceObjectFor()` |
| **CommonJS `require()`** | Via `goja_nodejs/require` | Via `sobek_nodejs/require` (equivalent fork) |
| **Host module resolution** | `SourceLoader`/`PathResolver` in require pkg | ESM: `HostResolveImportedModuleFunc`; CJS: same pattern |

Sobek regularly merges upstream Goja changes (PRs #68, #67, #56, #54, #52, #50, #46, #45, #44, #43, #42, #40 are all titled "Update goja"), so it inherits all Goja improvements while adding the ESM layer. The minimum Go version is **1.21** (vs Goja's 1.20).

---

## Conclusion

Sobek's ESM support is real, functional, and spec-compliant — not vaporware. The **`HostResolveImportedModuleFunc` callback** is the single most important API for embedders: it gives you complete control over module resolution, letting you map import specifiers to virtual filesystems, embedded bundles, Go-native modules, or remote URLs. The four-phase lifecycle (`ParseModule` → `Link` → `Instantiate` → `Evaluate`) closely mirrors the ECMAScript specification, which makes the API predictable but verbose.

For Go developers building LLM-powered code execution: **bundle third-party libraries to ESM format with esbuild, embed them via `//go:embed`, and register them in your `HostResolveImportedModuleFunc`**. This gives LLM-generated code natural `import` syntax. For Go-native utilities, either implement the `ModuleRecord` interface directly or use the simpler `sobek_nodejs/require` package with `RegisterNativeModule`. The k6 codebase in `js/modules/resolution.go` remains the definitive reference implementation for building production module systems on Sobek — study its `ModuleResolver` and `wrappedGoModule` patterns for the most battle-tested approach.