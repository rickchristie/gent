package section

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	"github.com/rickchristie/gent"
)

// JSON implements [gent.TextSection] for structured JSON content.
//
// Use JSON sections when you need the LLM to output structured data within
// the agent loop (as opposed to the final answer). The section automatically
// generates a JSON Schema from the type parameter T.
//
// Supports: primitives, pointers, structs, slices, maps, time.Time, time.Duration.
//
// # Use Cases
//
//   - Intermediate analysis that the agent loop processes
//   - Structured plans or decisions within reasoning
//   - Any section requiring typed data extraction
//
// # Creating and Configuring
//
//	type Analysis struct {
//	    Sentiment string   `json:"sentiment" description:"positive, negative, or neutral"`
//	    Topics    []string `json:"topics" description:"main topics discussed"`
//	    Score     float64  `json:"score" description:"confidence score 0-1"`
//	}
//
//	// Create with type parameter
//	analysis := section.NewJSON[Analysis]("analysis")
//
//	// Add guidance and example
//	analysis := section.NewJSON[Analysis]("analysis").
//	    WithGuidance("Analyze the sentiment and topics of the text.").
//	    WithExample(Analysis{
//	        Sentiment: "positive",
//	        Topics:    []string{"technology", "innovation"},
//	        Score:     0.85,
//	    })
//
// # Struct Tags
//
// Use struct tags to customize the generated JSON Schema:
//
//	type Response struct {
//	    Name    string  `json:"name"`                        // Required field
//	    Age     int     `json:"age,omitempty"`               // Optional (omitempty)
//	    Email   *string `json:"email"`                       // Optional (pointer)
//	    Country string  `json:"country" description:"ISO country code"`
//	}
//
// # Parse Error Handling
//
// When JSON parsing fails, a [gent.ParseErrorEvent] is published and the error
// is returned. The framework tracks consecutive parse errors via
// [gent.SGSectionParseErrorConsecutive] for limit enforcement.
type JSON[T any] struct {
	sectionName string
	guidance    string
	example     *T
}

// NewJSON creates a new JSON section with the given name.
func NewJSON[T any](name string) *JSON[T] {
	return &JSON[T]{
		sectionName: name,
		guidance:    "",
	}
}

// WithGuidance sets the guidance text for this section. The guidance appears at the
// beginning of the section content when TextOutputFormat.DescribeStructure() generates
// the format prompt, followed by the JSON schema.
//
// This can be instructions (e.g., "Analyze the provided text") or additional context.
func (j *JSON[T]) WithGuidance(guidance string) *JSON[T] {
	j.guidance = guidance
	return j
}

// WithExample sets an example value to include in the guidance.
// The example is serialized to JSON and appended after the schema.
func (j *JSON[T]) WithExample(example T) *JSON[T] {
	j.example = &example
	return j
}

// Name returns the section identifier.
func (j *JSON[T]) Name() string {
	return j.sectionName
}

// Guidance returns the full guidance text including JSON schema derived from T.
func (j *JSON[T]) Guidance() string {
	var sb strings.Builder

	if j.guidance != "" {
		sb.WriteString(j.guidance)
		sb.WriteString("\n\n")
	}

	sb.WriteString(j.sectionName)
	sb.WriteString(" content must be valid JSON matching this schema:\n")

	var zero T
	schema := GenerateJSONSchema(reflect.TypeOf(zero))
	schemaJSON, err := json.MarshalIndent(schema, "", "  ")
	if err == nil {
		sb.Write(schemaJSON)
	}

	if j.example != nil {
		sb.WriteString("\n\nExample:\n")
		exampleJSON, err := json.MarshalIndent(j.example, "", "  ")
		if err == nil {
			sb.Write(exampleJSON)
		}
	}

	return sb.String()
}

// ParseSection parses the JSON content into type T.
func (j *JSON[T]) ParseSection(execCtx *gent.ExecutionContext, content string) (any, error) {
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
			execCtx.PublishParseError(gent.ParseErrorTypeSection, content, parseErr)
		}
		return nil, parseErr
	}

	// Successful parse - reset consecutive error gauge
	if execCtx != nil {
		execCtx.Stats().ResetGauge(gent.SGSectionParseErrorConsecutive)
	}

	return result, nil
}

// Compile-time check that JSON implements gent.TextOutputSection.
var _ gent.TextOutputSection = (*JSON[any])(nil)
