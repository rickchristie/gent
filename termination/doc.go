// Package termination provides implementations for determining when an agent should stop.
//
// # Overview
//
// A Termination is responsible for:
//  1. Parsing the answer section content into a structured format
//  2. Running optional validators to check answer quality
//  3. Deciding whether to accept, reject, or continue based on the content
//
// # Available Terminations
//
//   - [Text]: Plain text answers - any non-empty text terminates
//   - [JSON]: Structured JSON answers - validates against a Go type
//
// # Choosing a Termination Type
//
// Use [Text] when:
//   - The agent should respond with free-form natural language
//   - You don't need to parse the response programmatically
//   - The response format is flexible
//
// Use [JSON] when:
//   - You need structured data (orders, tickets, API responses)
//   - The response will be processed by code
//   - You want automatic schema validation
//
// # Validators
//
// Both termination types support optional validators via SetValidator:
//
//	type OrderValidator struct{}
//
//	func (v *OrderValidator) Name() string { return "order_validator" }
//
//	func (v *OrderValidator) Validate(
//	    execCtx *gent.ExecutionContext,
//	    answer any,
//	) *gent.ValidationResult {
//	    order := answer.(OrderResponse)
//	    if order.Total <= 0 {
//	        return &gent.ValidationResult{
//	            Accepted: false,
//	            Feedback: []gent.FormattedSection{
//	                {Name: "error", Content: "Order total must be positive"},
//	            },
//	        }
//	    }
//	    return &gent.ValidationResult{Accepted: true}
//	}
//
//	term := termination.NewJSON[OrderResponse]("answer")
//	term.SetValidator(&OrderValidator{})
//
// When a validator rejects an answer, the agent receives feedback and can retry.
// The framework tracks rejection counts via [gent.SCAnswerRejectedTotal] and
// [gent.SCAnswerRejectedBy] stats, allowing limits to be set on retries.
//
// # Example Usage
//
//	// Text termination for conversational agent
//	chatAgent := react.NewAgent(model).
//	    WithTermination(termination.NewText("response").
//	        WithGuidance("Respond naturally to the user."))
//
//	// JSON termination for data extraction
//	type ExtractedData struct {
//	    Name    string `json:"name"`
//	    Email   string `json:"email"`
//	    Company string `json:"company,omitempty"`
//	}
//
//	extractAgent := react.NewAgent(model).
//	    WithTermination(termination.NewJSON[ExtractedData]("result").
//	        WithGuidance("Extract contact information from the text.").
//	        WithExample(ExtractedData{
//	            Name:    "John Doe",
//	            Email:   "john@example.com",
//	            Company: "Acme Inc",
//	        }))
package termination
