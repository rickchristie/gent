// Package toolchain provides native tool definition, validation, execution, and recovery.
//
// Registry advertises registered active tools. SharedLocalSearch advertises search, execute_tool,
// and pinned tools; search returns current definitions without promoting them to native functions.
// JsToolChainWrapper exposes the local JavaScript runtime as run_code. Configure chains before
// sharing them between runs. Search visibility does not enforce business permissions.
//
// Native JSON arguments are checked against the selected tool schema and converted to its Go
// input type. Before-call hooks can change that input; Gent validates the effective value again.
// Conversion supports time.Time and time.Duration strings. Data values preserve exact integers.
//
// Every call has a saved plan. Gent saves effective input before an effect, then saves the accepted
// outcome, prepared multipart reply, diagnostics, and statistics before dependent work. Tool Go
// errors use safe model guidance. Recovery reports never repeat a business action automatically.
// An interrupted JavaScript program returns saved and recovered evidence without restarting it.
//
// Scripts receive the common ModelToolCallResponse view. Text, data, URLs, and instructions appear
// automatically. Byte parts require same-script tool.forward and otherwise produce an omission
// notice. Saved original parts remain complete. There is no tool-result deduplication.
package toolchain
