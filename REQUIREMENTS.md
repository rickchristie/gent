  ---
  The wrapper ToolChain approach is sound. It composes cleanly with the existing architecture. Here's why, and the issues I see:

  Why it works

  The key insight is that ToolChain is a TextSection — it owns Name(), Guidance(), ParseSection(), and Execute(). Your wrapper doesn't break any contracts:

  - Guidance() — The wrapper returns a combined guidance showing two modes (direct call vs code). The TextFormat sees one section name (e.g. "action"), but the
  content inside has sub-structure. This is fine — the format extracts raw text, the toolchain interprets it.
  - Execute() — Receives the raw content string, decides which mode it is, dispatches accordingly. Direct calls go to the wrapped toolchain. Code blocks go to
  Sobek. Results from both are formatted back.
  - AvailableToolsPrompt() — Delegates to the wrapped toolchain, possibly appending info about available JS libraries/builtins.

  Potential issues

  1. Sub-section parsing is fragile. The <direct_call> and <code> distinction lives inside the action section content. The TextFormat already parsed the outer
  <action> tag. Now your wrapper must parse inner tags from raw text — but you can't use the TextFormat for this because it operates on the full LLM response,
  not a subsection. You'd need a lightweight inner parser. This is doable but it's a second layer of parsing with its own error modes.

  Consider: what if the LLM outputs both <direct_call> and <code> in the same action section? What if it outputs neither? What if it nests them wrong? Each edge
   case needs a steering error message.

  2. The LLM might mix modes unpredictably. Even with clear guidance, LLMs sometimes output a <code> block that's basically just a single tool call wrapper
  (await lookup_customer("C001")). That's wasteful — it adds Sobek overhead for no benefit. You may want to detect trivial code blocks and route them as direct
  calls, or just accept the small overhead.

  3. Tool function binding in Sobek needs careful design. You need to convert registered tools into JS-callable async functions. Each tool has Name(),
  ParameterSchema(), and Call(). You'd generate something like:

  async function lookup_customer(args) { /* bound to Go */ }

  But the schema validation that the wrapped toolchain does (JSON Schema check before calling Tool.Call()) — do you replicate that in the Sobek path, or let the
   Go function handle it? I'd say: validate in Go before calling, same as the direct path. The JS just dispatches.

  4. Stats and event publishing. The wrapped toolchain's Execute() already publishes BeforeToolCallEvent/AfterToolCallEvent. For code execution, tool calls
  happen inside Sobek — you need to publish those same events from the Go bindings. Otherwise stats (SCToolCalls, SCToolCallsFor, error gauges) won't track
  code-invoked tool calls. This is solvable — your Go-bound functions can call execCtx.PublishBeforeToolCall() / PublishAfterToolCall() — but it needs to be
  explicit in the design.

  5. Error reporting back to the LLM. For direct calls, errors are formatted per-tool in the observation. For code execution, you have two kinds of errors: JS
  runtime errors (syntax, TypeError, etc.) and tool call errors (tool returned an error). The code can try/catch tool errors — that's a feature, the LLM can
  write resilient code. But JS runtime errors need to be caught by the Sobek host and reported back clearly.

  One alternative worth considering

  Instead of sub-sections inside <action>, you could use two separate top-level sections: keep <action> for direct calls and add a new <code> section. The ReAct
   agent in buildOutputSections() registers sections from the toolchain — you could have the wrapper register both. But this would require changes to the ReAct
  agent since it currently only checks parsed[r.toolChain.Name()] for one section name. The wrapper approach avoids this by keeping a single section name and
  handling dispatch internally. I think internal dispatch is the better choice — it's self-contained and doesn't require agent changes.

  Summary

  The JsToolChainWrapper design works. The main complexity lives in:
  1. Inner sub-section parsing (keep it simple — just look for the two markers)
  2. Sobek tool bindings that properly publish stats events
  3. Error message formatting for both JS runtime errors and tool call errors

  The benefits are real — the LLM can write loops, conditionals, data transforms, parallel calls — all within a single iteration instead of burning 5-10
  iterations on sequential tool calls. And since Sobek is pure Go, each PTC execution is just a goroutine with a sobek.Runtime — no containers, no IPC.

✻ Sautéed for 55s

❯ Re #1 - Why can't JsToolChainWrapper just use its own TextFormat? The user injects a pristine TextFormat when initializing the wrapper. TextFormat should     
   work with any type of text, right? Correct me if I'm wrong. On the complexity, yes agreed - this is why I mentioned that JsToolChainWrapper must handle  
  all the edge cases when parsing. Once this is done though, PTC is basically available for us to use in all types of AgentLoop.                                
                                                                                                                                                            
  Re #2 - Just accept the small overhead for now. I believe that's the problem of the userland. We can set up the wrapper to have customizable prompts so       
  they can write prompts that works well with the LLM that they choose.                                                                                       
                                                                                                                                                                
  Re #3 - I'm thinking. The Go module can just have "tool.call(request)" and "tool.parallelCall([]request) function, the request object here mirrors the        
  json object that the LLM will write for the direct tool call. So it's the same schema, so the wrapper just passes it through the ToolChain, maybe a           
  little JSON.marshal overhead. The big thing here since it's just passing through to the wrapped ToolChain, all stats, events, etc works as intended. What     
   do you think?                                                                                                                                                
                                                                                                                                                                
  Re #4 - The PTC execution doesn't register as BeforeToolCall/AfterToolCall event - it's just pass through to the wrapped ToolChain, as the request object     
   is the exact same. It instead publishes its own custom stats.                                                                                                
                                                                                                                                                                
  Re #5 - Can we investigate Sobek's error message? My gut feel is that Sobek should have good error message that we can just pass through back to the LLM,     
   if not, then we have to figure out how to give clearer error message, one that contains stack trace with chunks of code instead of just line numbers,        
  considering there isn't a line - a "developer-friendly error view" - if you will, just like the exception views of vite and nextjs dev server - but this      
  is text-based and for LLM, not humans.                                                                                                                        
                                                                                                                                                                
  What do you think?                                                                                                                                            
                                                                                                                                                                
● Good points across the board. Let me address each, and investigate Sobek's errors.                                                                            
                                                                                                                                                                
● Agent(Research Sobek error types)                                                                                                                             
  ⎿  Done (23 tool uses · 32.7k tokens · 3m 25s)                                                                                                                
  (ctrl+o to expand)                                                                                                                                            
                                                                                                                  
● Great research. Now to your points:                                                                                                                           
                                                            
  Re #1 — Using a pristine TextFormat for inner parsing. You're right, I overcomplicated it. TextFormat parses arbitrary text into sections by tag/header names.
   You inject a fresh format.NewXML() (or whatever), register direct_call and code as sections, and call Parse() on the raw content string that Execute()       
  receives. That works cleanly — no custom parser needed. The wrapper's ParseSection() and Execute() just use this internal TextFormat. Good call.              
                                                                                                                                                                
  Re #2 — Agreed. Customizable prompts is the right approach. Users tune guidance for their chosen model.                                                       
                                                                                                                                                                
  Re #3 — Routing through the wrapped ToolChain is the right design. The JS code calls tool.call({tool: "lookup_customer", args: {id: "C001"}}), you            
  JSON.marshal that on the Go side, and pass it straight into wrappedToolChain.Execute(). This means:
                                                                                                                                                                
  - Schema validation happens in the wrapped toolchain, not duplicated                                                                                          
  - BeforeToolCallEvent / AfterToolCallEvent are published by the wrapped toolchain                                                                             
  - All existing stats (SCToolCalls, SCToolCallsFor, error gauges) work unchanged
  - tool.parallelCall([...]) just calls Execute() for each request concurrently (or sequentially — same semantics as the wrapped toolchain handling multiple
  calls)

  This is elegant because the wrapper truly wraps — it doesn't reimplement tool dispatch. The only new responsibility is the Sobek runtime lifecycle.

  Re #4 — Agreed. PTC gets its own stats (e.g. code execution count, code execution errors, maybe execution duration). Tool-level stats flow through the wrapped
   toolchain naturally.

  Re #5 — Sobek error messages. Here's what the research found:

  Runtime errors (*sobek.Exception) are solid — full stack traces with line:column per frame:
  TypeError: Cannot read property 'foo' of undefined
      at myFunc (script.js:10:5(42))
      at <anonymous> (script.js:3:1(7))
  ex.Error() gives a one-liner (first frame only), ex.String() gives the full stack.

  Syntax errors (*sobek.CompilerSyntaxError) give position but are terse:
  SyntaxError: Unexpected token } at script.js:5:12

  What Sobek does NOT provide: source code context. No "here's the offending line with a caret." You get line:column numbers but not the actual code around the
  error.

  Since you have the original source (the LLM wrote it), building a developer-friendly error view is straightforward. Something like:

  SyntaxError: Unexpected token } at line 5, column 12

    3 | const result = await tool.call({
    4 |   tool: "lookup_customer",
    5 |   args: { id: "C001", }
      |            ^ Unexpected token }
    6 | });

  Or for runtime exceptions, walk ex.Stack() and annotate each frame with source context. Since the code is typically short (LLM-generated scripts, not full
  applications), you could even include the entire script in the error message with the error line highlighted. LLMs are good at reading code with error
  annotations — this gives them everything they need to self-correct.

  The implementation would be a small utility: take the source string + file.Position (from StackFrame.Position() or CompilerError.File.Position(offset)),
  extract surrounding lines, and format. Minimal code.

  Overall — I think the design is clean. The wrapper is genuinely a wrapper: inner TextFormat for sub-section parsing, pass-through to wrapped ToolChain for
  tool dispatch, Sobek for code execution, and a source-annotated error formatter for LLM-friendly diagnostics. All existing stats/events/limits work without
  changes to the core framework.