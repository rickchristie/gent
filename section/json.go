package section

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	"github.com/rickchristie/gent"
)

// JSON is a TextOutputSection that parses JSON content into type T.
// Supports: primitives, pointers, structs, slices, maps, time.Time, time.Duration.
//
// Use this for sections where you need the LLM to output structured JSON data.
// The section automatically generates a JSON Schema prompt from the type T.
//
// Example:
//
//	type Analysis struct {
//	    Sentiment string   `json:"sentiment" description:"positive, negative, or neutral"`
//	    Topics    []string `json:"topics" description:"main topics discussed"`
//	    Score     float64  `json:"score" description:"confidence score 0-1"`
//	}
//
//	section := section.NewJSON[Analysis]("Analysis").
//	    WithPrompt("Analyze the provided text.")
type JSON[T any] struct {
	sectionName string
	prompt      string
	example     *T
}

// NewJSON creates a new JSON section with the given name.
func NewJSON[T any](name string) *JSON[T] {
	return &JSON[T]{
		sectionName: name,
		prompt:      "",
	}
}

// WithPrompt sets the prompt instructions for this section.
func (j *JSON[T]) WithPrompt(prompt string) *JSON[T] {
	j.prompt = prompt
	return j
}

// WithExample sets an example value to include in the prompt.
func (j *JSON[T]) WithExample(example T) *JSON[T] {
	j.example = &example
	return j
}

// Name returns the section identifier.
func (j *JSON[T]) Name() string {
	return j.sectionName
}

// Prompt returns the instructions including JSON schema derived from T.
func (j *JSON[T]) Prompt() string {
	var sb strings.Builder

	if j.prompt != "" {
		sb.WriteString(j.prompt)
		sb.WriteString("\n\n")
	}

	sb.WriteString("Respond with valid JSON matching this schema:\n")

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
		// Trace parse error (auto-updates stats)
		if execCtx != nil {
			execCtx.Trace(gent.ParseErrorTrace{
				ErrorType:  "section",
				RawContent: content,
				Error:      parseErr,
			})
		}
		return nil, parseErr
	}

	// Successful parse - reset consecutive error counter
	if execCtx != nil {
		execCtx.Stats().ResetCounter(gent.KeySectionParseErrorConsecutive)
	}

	return result, nil
}

// Compile-time check that JSON implements gent.TextOutputSection.
var _ gent.TextOutputSection = (*JSON[any])(nil)
