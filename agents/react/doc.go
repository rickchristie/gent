// Package react implements the ReAct (Reasoning and Acting) agent loop pattern.
//
// # Overview
//
// The ReAct pattern alternates between thinking (reasoning) and acting (tool execution)
// to solve complex tasks. Each iteration follows the cycle: Think -> Act -> Observe -> Repeat.
//
// # Agent Loop Behavior
//
// The agent loop follows these behaviors in order of priority:
//
// ## 1. Actions Take Priority Over Termination
//
// When the model outputs both an action (tool call) and an answer (termination) in the
// same response, the action takes priority. The agent executes the tool calls and continues
// the loop, discarding the answer for that iteration.
//
// Rationale: Tool calls may fail or produce unexpected results. Terminating before
// executing tools would provide an answer based on assumptions rather than actual results.
// The next iteration will provide the answer after observing tool results.
//
// Example: If the LLM responds with:
//
//	<thinking>I'll reschedule the booking and send confirmation</thinking>
//	<action>
//	- tool: reschedule_booking
//	  args: {booking_id: "BK001", new_flight: "AA101"}
//	</action>
//	<answer>Your booking has been rescheduled successfully!</answer>
//
// The agent will:
//  1. Execute the reschedule_booking tool
//  2. Continue the loop with the tool observation
//  3. Discard the premature answer
//  4. Allow the next iteration to provide an answer based on actual results
//
// ## 2. Parse Error Handling
//
// Parse errors are only raised if there are no actions to execute and no valid termination.
// This allows the agent to gracefully handle malformed responses when possible.
//
// ## 3. Empty Response Handling
//
// If the model response contains neither actions nor a valid termination signal, the agent
// continues the loop with an empty observation. This allows the model to recover in the
// next iteration.
//
// # Configuration
//
// The agent can be configured with:
//   - WithBehaviorAndContext: Custom behavior instructions
//   - WithCriticalRules: Critical rules the agent must follow
//   - WithFormat: Custom output format (default: XML)
//   - WithToolChain: Custom tool chain (default: YAML)
//   - WithTermination: Custom termination handler (default: Text)
//   - WithThinking: Enable thinking section
//   - WithStreaming: Enable streaming responses
//   - WithSystemTemplate: Custom system prompt template
//   - WithTimeProvider: Custom time provider for templates
//
// # Templates
//
// The system prompt is a Go text/template with access to:
//   - Time provider functions: {{.Time.Today}}, {{.Time.Weekday}}, {{.Time.Format "layout"}}
//   - Behavior context: {{.BehaviorAndContext}}
//   - Critical rules: {{.CriticalRules}}
//   - Output format: {{.OutputPrompt}}
//   - Tools description: {{.ToolsPrompt}}
package react
