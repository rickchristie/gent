package termination

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/rickchristie/gent"
	"github.com/tmc/langchaingo/llms"
)

// JSON implements [gent.Termination] for structured JSON answers.
//
// Use JSON termination when you need the agent to return structured data that
// can be programmatically processed. The type parameter T defines the expected
// structure, and a JSON Schema is automatically generated from it.
//
// Supports: primitives, pointers, structs, slices, maps, time.Time, time.Duration.
//
// # Creating and Configuring
//
//	// Define your response structure
//	type OrderResponse struct {
//	    OrderID   string   `json:"order_id"`
//	    Status    string   `json:"status"`
//	    Items     []string `json:"items"`
//	    Total     float64  `json:"total"`
//	}
//
//	// Create JSON termination
//	term := termination.NewJSON[OrderResponse]("answer")
//
//	// Add guidance and example
//	term := termination.NewJSON[OrderResponse]("answer").
//	    WithGuidance("Provide the order details in JSON format.").
//	    WithExample(OrderResponse{
//	        OrderID: "ORD-12345",
//	        Status:  "confirmed",
//	        Items:   []string{"Widget", "Gadget"},
//	        Total:   99.99,
//	    })
//
// # Using with Agent
//
//	agent := react.NewAgent(model).
//	    WithTermination(termination.NewJSON[OrderResponse]("answer").
//	        WithGuidance("Return the order details."))
//
// # Struct Tags
//
// Use struct tags to customize the JSON Schema:
//
//	type Response struct {
//	    Name    string  `json:"name"`                        // Required field
//	    Age     int     `json:"age,omitempty"`               // Optional (omitempty)
//	    Email   *string `json:"email"`                       // Optional (pointer)
//	    Country string  `json:"country" description:"ISO country code"` // With description
//	}
//
// # Adding Validation
//
// You can add an [gent.AnswerValidator] to validate the parsed struct:
//
//	term := termination.NewJSON[OrderResponse]("answer")
//	term.SetValidator(&orderValidator{})  // Receives OrderResponse
//
// # Termination Behavior
//
//   - Empty content: Returns [gent.TerminationContinue]
//   - Invalid JSON: Returns [gent.TerminationContinue] (agent should try again)
//   - Valid JSON with validation failure: Returns [gent.TerminationAnswerRejected]
//   - Valid JSON passing validation: Returns [gent.TerminationAnswerAccepted]
type JSON[T any] struct {
	sectionName string
	guidance    string
	example     *T
	validator   gent.AnswerValidator
}

// NewJSON creates a new JSON termination with the given name.
func NewJSON[T any](name string) *JSON[T] {
	return &JSON[T]{
		sectionName: name,
		guidance:    "Write your final answer here.",
	}
}

// WithGuidance sets the guidance text for this termination. The guidance appears at the
// beginning of the section content when TextOutputFormat.DescribeStructure() generates
// the format prompt, followed by the JSON schema.
//
// This can be instructions (e.g., "Provide your final answer") or additional context.
func (t *JSON[T]) WithGuidance(guidance string) *JSON[T] {
	t.guidance = guidance
	return t
}

// WithExample sets an example value to include in the guidance.
// The example is serialized to JSON and appended after the schema.
func (t *JSON[T]) WithExample(example T) *JSON[T] {
	t.example = &example
	return t
}

// Name returns the section identifier.
func (t *JSON[T]) Name() string {
	return t.sectionName
}

// Guidance returns the full guidance text including JSON schema derived from T.
func (t *JSON[T]) Guidance() string {
	var sb strings.Builder

	if t.guidance != "" {
		sb.WriteString(t.guidance)
		sb.WriteString("\n\n")
	}

	sb.WriteString("Respond with valid JSON matching this schema:\n")

	var zero T
	schema := generateJSONSchema(reflect.TypeOf(zero))
	schemaJSON, err := json.MarshalIndent(schema, "", "  ")
	if err == nil {
		sb.Write(schemaJSON)
	}

	if t.example != nil {
		sb.WriteString("\n\nExample:\n")
		exampleJSON, err := json.MarshalIndent(t.example, "", "  ")
		if err == nil {
			sb.Write(exampleJSON)
		}
	}

	return sb.String()
}

// ParseSection parses the JSON content into type T.
func (t *JSON[T]) ParseSection(execCtx *gent.ExecutionContext, content string) (any, error) {
	content = strings.TrimSpace(content)
	if content == "" {
		var zero T
		return zero, nil
	}

	var result T
	if err := json.Unmarshal([]byte(content), &result); err != nil {
		parseErr := fmt.Errorf("%w: %v", gent.ErrInvalidJSON, err)
		// Publish parse error event (auto-updates stats)
		if execCtx != nil {
			execCtx.PublishParseError(gent.ParseErrorTypeTermination, content, parseErr)
		}
		return nil, parseErr
	}

	// Successful parse - reset consecutive error counter
	if execCtx != nil {
		execCtx.Stats().ResetCounter(gent.KeyTerminationParseErrorConsecutive)
	}

	return result, nil
}

// SetValidator sets the validator to run on parsed answers before acceptance.
func (t *JSON[T]) SetValidator(validator gent.AnswerValidator) {
	t.validator = validator
}

// ShouldTerminate checks if the content indicates termination.
// For JSON termination, valid JSON that parses into T triggers termination (after validation).
// The result is returned as a TextContent containing the re-serialized JSON.
// Panics if execCtx is nil.
//
// When a validator is set, this method publishes:
//   - ValidatorCalledEvent: When the validator is invoked
//   - ValidatorResultEvent: When the validator returns (accepted or rejected)
func (t *JSON[T]) ShouldTerminate(
	execCtx *gent.ExecutionContext,
	content string,
) *gent.TerminationResult {
	if execCtx == nil {
		panic("termination: ShouldTerminate called with nil ExecutionContext")
	}

	content = strings.TrimSpace(content)
	if content == "" {
		return &gent.TerminationResult{Status: gent.TerminationContinue}
	}

	var result T
	if err := json.Unmarshal([]byte(content), &result); err != nil {
		return &gent.TerminationResult{Status: gent.TerminationContinue}
	}

	// Run validator if set
	if t.validator != nil {
		validatorName := t.validator.Name()

		// Publish validator called event
		execCtx.PublishValidatorCalled(validatorName, result)

		validationResult := t.validator.Validate(execCtx, result)
		if !validationResult.Accepted {
			// Publish validator result (rejection) - updates stats automatically
			execCtx.PublishValidatorResult(validatorName, result, false, validationResult.Feedback)

			// Convert feedback to ContentPart
			var feedback []gent.ContentPart
			for _, section := range validationResult.Feedback {
				formatted := "<" + section.Name + ">\n" + section.Content + "\n</" + section.Name + ">"
				feedback = append(feedback, llms.TextContent{Text: formatted})
			}

			return &gent.TerminationResult{
				Status:  gent.TerminationAnswerRejected,
				Content: feedback,
			}
		}

		// Publish validator result (acceptance)
		execCtx.PublishValidatorResult(validatorName, result, true, nil)
	}

	// Re-serialize to ensure consistent formatting
	formatted, err := json.Marshal(result)
	if err != nil {
		return &gent.TerminationResult{Status: gent.TerminationContinue}
	}

	return &gent.TerminationResult{
		Status:  gent.TerminationAnswerAccepted,
		Content: []gent.ContentPart{llms.TextContent{Text: string(formatted)}},
	}
}

// generateJSONSchema creates a JSON Schema from a Go type using reflection.
func generateJSONSchema(t reflect.Type) map[string]any {
	if t == nil {
		return map[string]any{"type": "null"}
	}

	// Handle pointer types
	if t.Kind() == reflect.Ptr {
		schema := generateJSONSchema(t.Elem())
		// Pointers are nullable
		if typeVal, ok := schema["type"].(string); ok {
			schema["type"] = []string{typeVal, "null"}
		}
		return schema
	}

	// Handle special types
	if t == reflect.TypeFor[time.Time]() {
		return map[string]any{
			"type":   "string",
			"format": "date-time",
		}
	}

	if t == reflect.TypeFor[time.Duration]() {
		return map[string]any{
			"type":        "string",
			"description": "Duration string (e.g., '1h30m', '2s')",
		}
	}

	switch t.Kind() {
	case reflect.String:
		return map[string]any{"type": "string"}

	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return map[string]any{"type": "integer"}

	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return map[string]any{"type": "integer"}

	case reflect.Float32, reflect.Float64:
		return map[string]any{"type": "number"}

	case reflect.Bool:
		return map[string]any{"type": "boolean"}

	case reflect.Slice, reflect.Array:
		return map[string]any{
			"type":  "array",
			"items": generateJSONSchema(t.Elem()),
		}

	case reflect.Map:
		return map[string]any{
			"type":                 "object",
			"additionalProperties": generateJSONSchema(t.Elem()),
		}

	case reflect.Struct:
		properties := make(map[string]any)
		required := make([]string, 0)

		for i := 0; i < t.NumField(); i++ {
			field := t.Field(i)

			// Skip unexported fields
			if !field.IsExported() {
				continue
			}

			// Get JSON tag
			jsonTag := field.Tag.Get("json")
			if jsonTag == "-" {
				continue
			}

			fieldName := field.Name
			omitempty := false

			if jsonTag != "" {
				parts := strings.Split(jsonTag, ",")
				if parts[0] != "" {
					fieldName = parts[0]
				}
				for _, part := range parts[1:] {
					if part == "omitempty" {
						omitempty = true
					}
				}
			}

			fieldSchema := generateJSONSchema(field.Type)

			// Add description from struct tag if present
			if desc := field.Tag.Get("description"); desc != "" {
				fieldSchema["description"] = desc
			}

			properties[fieldName] = fieldSchema

			// Required if not omitempty and not a pointer
			if !omitempty && field.Type.Kind() != reflect.Ptr {
				required = append(required, fieldName)
			}
		}

		schema := map[string]any{
			"type":       "object",
			"properties": properties,
		}

		if len(required) > 0 {
			schema["required"] = required
		}

		return schema

	default:
		return map[string]any{}
	}
}
