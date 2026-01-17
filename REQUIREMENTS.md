The interface needs to provide:
* A way for the user to select the LLM (using LangChainGo's llm.Model)
* A way for user to define system prompt.
   * System prompt and the original input is never going to be summarized, ever.
   * Long loops might be summarized, but these stays.




* A way for user to define tool functions.
   * There are Tool, and Toolchain abstraction.
   * Both Tool and Toolchain has a .Describe() function that lets the user control how the Tools are
     described in the prompt. ToolChain describe template can loop through Tool.Describe().
   * If the Tool failed to parse the input, Tool may provide a response text. This response prompt 
     will be added as the result, and the loop continues.
   * ToolChain.Evaluate(..) is always called, and it has responsibility to:
     * Parse the LLM output - determine whether there is a tool call or not.
     * Determines which tool were called.
     * If there's a tool call, prepare the input for the tool, call the tool, and return the tool 
       output as the result.
     * Construct the prompt that explains tools to the LLM.
   * Tool has the responsiblity for:
     * Describe itself (to be used in the ToolChain).
     * Execute the tool logic.
   * This means we can create default implementation of ToolChain that uses JSON, uses text, YAML, 
     or other formats.
   * ToolChain might also be implemented to trigger ANOTHER AgentLoop specifically to call tools and
     provide outputs to the AgentLoop.
* A way to define the termination strategy.
   * TerminationStrategy has Describe() function that lets user control how to describe to the agent
     how to terminate in the prompt.
   * These can be:
     * Specific tool call.
     * Specific text in the output.
   * TerminationStrategy.ShouldTerminate(...) is evaluated after each generation.
   * Responsibility of the Termination strategy is also to extract the final output from the 
     generation.
* A way to define how the Agent should format its output.
  * How would the Agent say which tool to call, format the arguments to that tool call?
  * How would the Agent say it's terminating, and provide the final output?
  * How would the Agent say what it's thinking?
  * This can be done by simply parsing, or through JSON, YAML, etc.
  * The AgentLoop is the one responsible to provide the prompt, so it has the responsibility to
    define how the output should be formatted.
  * The question is whether the formatting rule and parsing can be separated with the actual loop?
  * Separating ToolChain, Termination, FinalOutput formatting from one another makes it difficult
    to coordinate the formatting output of the Agent.
  * What if ToolChain cannot describe itself, because the formatting is defined by the AgentLoop?
  * We eliminate TerminationStrategy, as this is part of AgentLoop's responsibility to define how
    to terminate.
  * We create OutputFormat interface that accepts Tools slice, and the Termination criteria changed
    to be part of Executor configuration, where the user can define when to terminate based on
    limitation rules on total/per-model-key input and output tokens spent.

  * Formatter can be an optional tool that users can utilize in their AgentLoop prompt. It makes
    giving instructions on outputs and parsing outputs easier.
  * I'm thinking.. really what we want to do is to provide instruction for the agent to format their output into sections. What     
  if each section have their own instructions and parser? The Formatter is responsible for the overall structure and how to       
  separate the sections, but ToolChain abstraction can still work, because ToolChain implements FormattedSection (interface:      
  Prompt() and ParseSection()) - what do you think? This way we don't have to force everything into one big json output



  1. Section ordering - Does the LLM need instructions like "always thinking first, then tool_call or answer"? Who owns           
  that—Formatter or AgentLoop?                                                                                                    
  2. Optional vs required sections - Some outputs have tool_call, others have answer. How do you express "one of these, not both"?
  3. Multiple instances - Can there be multiple tool_calls in one response? The []string in Parse handles this, but does Prompt   
  need to explain it?                                                                                                             
  4. Parsing flow - I assume AgentLoop does:                                                                                      
  rawSections, _ := formatter.Parse(output)                                                                                       
  for name, contents := range rawSections {                                                                                       
      section := sectionsByName[name]                                                                                             
      for _, content := range contents {                                                                                          
          parsed, _ := section.ParseSection(content)                                                                              
          // handle parsed result                                                                                                 
      }                                                                                                                           
  }     

AgentLoop owns the overall prompt - so this means AgentLoop and the Formatter is tightly coupled (ReActJsonLoop with pure json  
  formatter cannot be swapped out with ReActSectionLoop), however the Formatter itself can be reused across multiple AgentLoop,   
  so we don't have to recode everything when we need to create new AgentLoop, we can focus on the fun part, experimenting with    
  the loop.                                                                                                                       
                                                                                                                                  
  What do you think?                                                                                                              
  1. AgentLoop owns this, Formatter must be able to separate sections in whatever order that comes. It then passes the separated  
  string to each FormattedSection (maybe each has Load() method?)                                                                 
  2. For optional ones, the FormattedSection raw string would just not exist at all in the output map.                            
  3. That is up to the ToolChain to explain, whatever goes into each section is up to each FormattedSection implementer. The      
  ToolChain implementation might support multiple tool calls, parallel tool calls, Formatter just passes the raw response to the  
  ToolChain.                                                                                                                      
  4. Yes, sort of like that.


Yep, I think we can name it to TextOutputFormat interface instead of Formatter, and also have TextOutputSection interface instead of FormattedSection.                                                                                                    
                                                                                                                                  
  Can you implement it for me?                                                                                                    
  - Create two implementations of the formatter:                                                                                  
  - One utilizing markdown style header, e.g. "# Thinking", "# Action", "# Observation"                                           
  - One utilizing xml style sectioning, e.g. "<thinking>...</thinking>, "<action>{could be json}</action>,                        
  "<observation></observation>"                                                                                                   
  - Create ToolChain implementations (both can receive multi actions at the same time):                                           
  - One utilizing JSON                                                                                                            
  - One utilizing YAML with block scalars                                                                                         
  - Create Termination implementations:                                                                                           
  - One accepts just a text.                                                                                                      
  - One accepts valid JSON and generic struct, uses reflection to create the struct and fill everything to that struct            
  (supports all types that might be useful, like pointers to struct, time.Time, etc)                                              
                                                                                                                                  
  Think hard about code architecture, maintainability, testability.                                                               
  DON'T code first, create a comprehensive, detailed /PLAN.md first so we don't lose context.


* Example of custom AgentLoops that we want to create:
  * ReActAgentLoop - simple and to the point.
  * SceneBuilderLoop - generates image, but then critiques the image, and regenerates until it
    fulfills all the criteria.
  * CustomerServiceLoop - ReAct pattern, but with additional loop at the end to ensure final output
    does not misrepresent company policies, etc.
  * A loop where each concern is handled by separate smaller LLMCalls, each constructing the final
    prompt for the main LLM call
  * StoryTellerLoop - specific thinking patterns where the agent considers the setting, character
    traits/agenda, plot points, environment, relative location of characters and objects, and
    then validates the response against character development arcs, plot consistency, and
    thematic elements.

* A way to define the complete input structure.
   * User can customize the input structure, and add prefixes/suffixes.
   * Default is:
      * {AgentLoop.SystemPrompt()} --> AgentLoop's system prompt is also a template that may include
        user input.
      * {UserInput}
      * {Toolchain.Describe()}
      * {TerminationStrategy.Describe()}
      * {LoopText}
   * The AgentLoop contains the SystemPrompt.
* A way to determine compaction/summarization strategy and when to trigger.
   * The {LoopText} is going to be compacted.
   * A strategy can be used, an interface that can be implemented.
   * CMIIW, but the compaction will 99.9% be done by AI, so we can just determine another Agent loop
     to do the compaction.
   * The CompactionStrategy.ShouldCompact(...) is called with parameters that the strategy can
     inspect, so users can implement, based on tokens, cost, length, iterations, etc.
* A way to define the AgentLoop:
  * AgentLoop's responsibilities are:
    * Construct the prompt to be sent to the LLM.
    * Call the LLM.
    * Use ToolChain, TerminationStrategy, CompactionStrategy to execute the loop.
  * This library is not opinionated on how AgentLoop should be defined. AgentLoop might call other
    AgentLoop in the middle of execution, etc.
  * This allows users to experiment with different patterns.
  * There is default implementation of ReActAgentLoop that implements ReAct pattern:
    * First call LLM for thought the action.
    * Process with TerminationStrategy.
    * Process with ToolChain in case there's an action.
    * Call LLM again with observation.
    * Either return final output, or continue the loop with the next prompt.
* There's also hooks for before start, after termination, before/after each generation, before/after
  each tool call, before/after each llm model call, so users can implement logging, metrics, and 
  additional processes etc.
  * Example additional processing: After termination, user may want to run another AgentLoop do a 
    final polish of the output, to run fact checking AgentLoop, check against company terms & 
    conditions, etc.
  * What I'm very interested in, is implementing another AgentLoop that inspects entire loop 
    generation and writes important thing to remember in next iteration.
* A way to define configuration:
   * Maximum loop before it's terminated with errors - can be set to zero.
* A way of saving all the loop debug/trace information, so each generation can be inspected and 
  troubleshooted, also saving the cost of each Node in detail, logging all llm calls on each node
  execution.

The executor executes everything.

The idea is this tool allows people to fully experiment everything, I don't want to impose on a 
specific way for termination, tool call, etc. The entire agent loop prompt should just be a blank 
canvas that the user can customize and experiment to the fullest extent. We just want a skeleton 
that makes it easy for people to try things out.

Is my thinking correct? Would there be issues or patterns that wouldn't be supported with this?