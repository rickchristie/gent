The interface needs to provide:
* A way for the user to select the LLM (using LangChainGo's llm.Model)
* A way for user to define system prompt.
   * System prompt and the original input is never going to be summarized, ever.
   * Long loops might be summarized, but these stays.
* A way for user to define tool functions.
   * There are Tool, and Toolchain abstraction.
   * Both Tool and Toolchain has a .Describe() function that lets the user control how the Tools are described in the prompt. ToolChain describe template can loop through Tool.Describe().
   * If the Tool failed to parse the input, Tool may provide a response text. This response prompt will be added as the result, and the loop continues.
   * ToolChain.Evaluate(..) is always called, and it has responsibility to:
     * Parse the LLM output - determine whether there is a tool call or not.
     * Determines which tool were called.
     * If there's a tool call, prepare the input for the tool, call the tool, and return the tool output as the result.
     * Construct the prompt that explains tools to the LLM.
   * Tool has the responsiblity for:
     * Describe itself (to be used in the ToolChain).
     * Execute the tool logic.
   * This means we can create default implementation of ToolChain that uses JSON, uses text, YAML, or other formats.
   * ToolChain might also be implemented to trigger ANOTHER AgentLoop specifically to call tools and provide outputs to the AgentLoop.
* A way to define the termination strategy.
   * TerminationStrategy has Describe() function that lets user control how to describe to the agent how to terminate in the prompt.
   * These can be:
     * Specific tool call.
     * Specific text in the output.
   * TerminationStrategy.ShouldTerminate(...) is evaluated after each generation.
   * Responsibility of the Termination strategy is also to extract the final output from the generation.
* A way to define the complete input structure.
   * User can customize the input structure, and add prefixes/suffixes.
   * Default is:
      * {AgentLoop.SystemPrompt()} --> AgentLoop's system prompt is also a template that may include user input.
      * {UserInput}
      * {Toolchain.Describe()}
      * {TerminationStrategy.Describe()}
      * {LoopText}
   * The AgentLoop contains the SystemPrompt.
* A way to determine compaction/summarization strategy and when to trigger.
   * The {LoopText} is going to be compacted.
   * A strategy can be used, an interface that can be implemented.
   * CMIIW, but the compaction will 99.9% be done by AI, so we can just determine another Agent loop to do the compaction.
   * The CompactionStrategy.ShouldCompact(...) is called with parameters that the strategy can inspect, so users can implement, based on tokens, cost, length, iterations, etc.
* A way to define the AgentLoop:
  * AgentLoop's responsibilities are:
    * Construct the prompt to be sent to the LLM.
    * Call the LLM.
    * Use ToolChain, TerminationStrategy, CompactionStrategy to execute the loop.
  * This library is not opinionated on how AgentLoop should be defined. AgentLoop might call other AgentLoop in the middle of execution, etc.
  * This allows users to experiment with different patterns.
  * There is default implementation of ReActAgentLoop that implements ReAct pattern:
    * First call LLM for thought the action.
    * Process with TerminationStrategy.
    * Process with ToolChain in case there's an action.
    * Call LLM again with observation.
    * Either return final output, or continue the loop with the next prompt.
* There's also hooks for before start, after termination, before/after each generation, before/after each tool call, before/after each llm model call, so users can implement logging, metrics, and additional processes etc.
  * Example additional processing: After termination, user may want to run another AgentLoop do a final polish of the output, to run fact checking AgentLoop, check against company terms & conditions, etc.
  * What I'm very interested in, is implementing another AgentLoop that inspects entire loop generation and writes important thing to remember in next iteration.
* A way to define configuration:
   * Maximum loop before it's terminated with errors - can be set to zero.
* A way of saving all the loop debug/trace information, so each generation can be inspected and troubleshooted, also saving the cost of each Node in detail, logging all llm calls on each node execution.

The executor executes everything.

The idea is this tool allows people to fully experiment everything, I don't want to impose on a specific way for termination, tool call, etc. The entire agent loop prompt should just be a blank canvas that the user can customize and experiment to the fullest extent. We just want a skeleton that makes it easy for people to try things out.

Is my thinking correct? Would there be issues or patterns that wouldn't be supported with this?