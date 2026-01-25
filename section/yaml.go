package section

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/rickchristie/gent"
	"gopkg.in/yaml.v3"
)

// YAML is a TextOutputSection that parses YAML content into type T.
// Supports: primitives, pointers, structs, slices, maps, time.Time, time.Duration.
//
// Use this for sections where you need the LLM to output structured YAML data.
// YAML is often easier for LLMs to generate than JSON due to less strict syntax.
//
// Example:
//
//	type Plan struct {
//	    Goal  string   `yaml:"goal" description:"the main objective"`
//	    Steps []string `yaml:"steps" description:"ordered list of steps"`
//	}
//
//	section := section.NewYAML[Plan]().
//	    WithSectionName("plan").
//	    WithPrompt("Create a plan to achieve the user's goal.")
type YAML[T any] struct {
	sectionName string
	prompt      string
	example     *T
}

// NewYAML creates a new YAML section with default section name "data".
func NewYAML[T any]() *YAML[T] {
	return &YAML[T]{
		sectionName: "data",
		prompt:      "",
	}
}

// WithSectionName sets the section name for this section.
func (y *YAML[T]) WithSectionName(name string) *YAML[T] {
	y.sectionName = name
	return y
}

// WithPrompt sets the prompt instructions for this section.
func (y *YAML[T]) WithPrompt(prompt string) *YAML[T] {
	y.prompt = prompt
	return y
}

// WithExample sets an example value to include in the prompt.
func (y *YAML[T]) WithExample(example T) *YAML[T] {
	y.example = &example
	return y
}

// Name returns the section identifier.
func (y *YAML[T]) Name() string {
	return y.sectionName
}

// Prompt returns the instructions including YAML schema derived from T.
func (y *YAML[T]) Prompt() string {
	var sb strings.Builder

	if y.prompt != "" {
		sb.WriteString(y.prompt)
		sb.WriteString("\n\n")
	}

	sb.WriteString("Respond with valid YAML matching this schema:\n")

	var zero T
	schema := GenerateJSONSchema(reflect.TypeOf(zero))
	schemaYAML, err := yaml.Marshal(schema)
	if err == nil {
		sb.Write(schemaYAML)
	}

	if y.example != nil {
		sb.WriteString("\nExample:\n")
		exampleYAML, err := yaml.Marshal(y.example)
		if err == nil {
			sb.Write(exampleYAML)
		}
	}

	return sb.String()
}

// ParseSection parses the YAML content into type T.
func (y *YAML[T]) ParseSection(execCtx *gent.ExecutionContext, content string) (any, error) {
	content = strings.TrimSpace(content)
	if content == "" {
		var zero T
		return zero, nil
	}

	var result T
	if err := yaml.Unmarshal([]byte(content), &result); err != nil {
		parseErr := fmt.Errorf("%w: %v", gent.ErrInvalidYAML, err)
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

// Compile-time check that YAML implements gent.TextOutputSection.
var _ gent.TextOutputSection = (*YAML[any])(nil)
