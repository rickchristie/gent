// Package react implements a native tool agent loop.
//
// NewAgent sends the current ToolChain definitions on each request. A complete model response
// either selects native tools or supplies a final answer to the configured Termination handler.
// Tool calls take priority over text in the same response: their results must be observed before
// the agent can finish. Incomplete calls and answers produce retry feedback without tool effects.
//
// TextFormat renders system prompts and tool results. It does not parse model tool calls.
// Before-system-prompt and before-model-call hooks can change each request. Before-tool-call
// hooks can change effective typed input; that input is saved before the tool starts.
//
// Registry, SearchChain, and JsToolChainWrapper share the native ToolChain contract. Search selects
// definitions from active run history. JavaScript is exposed as run_code and uses the same tool
// validation, hooks, limits, and checkpoints for inner calls.
//
// Set WithRecoveryVersion when a run must resume on a new worker. This version identifies
// application callbacks and implementation behavior that cannot be derived from schemas.
// Recovery reuses completed results. Uncertain outcomes produce guidance for a new model plan.
// Interrupted JavaScript returns partial evidence without restarting the script.
package react
