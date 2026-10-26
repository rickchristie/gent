// Package toolchain provides native tool definition, validation, and execution.
//
// Registry exposes all registered tools. SearchChain exposes search and pinned tools, then adds
// discovered definitions on later requests. JsToolChainWrapper exposes the local JavaScript
// runtime as run_code. Configure chains before sharing them between runs.
//
// Native JSON arguments are checked against the tool's schema and converted to its Go input type.
// Conversion supports time.Time strings in common ISO formats and time.Duration strings such as
// "1h30m". Values for any or map[string]any inputs keep their JSON representation.
//
// Every call uses a saved plan. Before-tool hooks run before effective typed input is saved;
// result text, media, errors, and deduplication summaries are saved after execution. Recovery uses
// saved evidence and optional application callbacks. Unknown outcomes do not cause blind replay.
// An interrupted JavaScript program returns its partial results and script for a new model plan.
//
// Custom chains can embed Registry to retain these execution controls and override NativeTools
// to select definitions. Selection controls visibility; application code must check access rights.
package toolchain
