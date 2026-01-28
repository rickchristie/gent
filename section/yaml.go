package section

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/rickchristie/gent"
	"gopkg.in/yaml.v3"
)

// YAML implements [gent.TextSection] for structured YAML content.
//
// Use YAML sections when you need the LLM to output structured data within
// the agent loop. YAML is often easier for LLMs to generate than JSON due
// to less strict syntax (no quotes required, simpler multiline strings).
//
// Supports: primitives, pointers, structs, slices, maps, time.Time, time.Duration.
//
// # Use Cases
//
//   - Intermediate structured data within reasoning
//   - Plans or configuration-like content
//   - Any section requiring typed data with multiline strings
//
// # Creating and Configuring
//
//	type Plan struct {
//	    Goal  string   `yaml:"goal" description:"the main objective"`
//	    Steps []string `yaml:"steps" description:"ordered list of steps"`
//	}
//
//	// Create with type parameter
//	plan := section.NewYAML[Plan]("plan")
//
//	// Add guidance and example
//	plan := section.NewYAML[Plan]("plan").
//	    WithGuidance("Create a plan to achieve the user's goal.").
//	    WithExample(Plan{
//	        Goal:  "Deploy the application",
//	        Steps: []string{"Build", "Test", "Deploy"},
//	    })
//
// # Struct Tags
//
// Use yaml struct tags for field naming:
//
//	type Config struct {
//	    Name     string `yaml:"name"`
//	    Timeout  int    `yaml:"timeout,omitempty"`
//	    Enabled  bool   `yaml:"enabled"`
//	}
//
// # Parse Error Handling
//
// When YAML parsing fails, a [gent.ParseErrorEvent] is published and the error
// is returned. The framework tracks consecutive parse errors via
// [gent.KeySectionParseErrorConsecutive] for limit enforcement.
type YAML[T any] struct {
	sectionName string
	guidance    string
	example     *T
}

// NewYAML creates a new YAML section with the given name.
func NewYAML[T any](name string) *YAML[T] {
	return &YAML[T]{
		sectionName: name,
		guidance:    "",
	}
}

// WithGuidance sets the guidance text for this section. The guidance appears at the
// beginning of the section content when TextOutputFormat.DescribeStructure() generates
// the format prompt, followed by the YAML schema.
//
// This can be instructions (e.g., "Create a plan to achieve the goal") or additional context.
func (y *YAML[T]) WithGuidance(guidance string) *YAML[T] {
	y.guidance = guidance
	return y
}

// WithExample sets an example value to include in the guidance.
// The example is serialized to YAML and appended after the schema.
func (y *YAML[T]) WithExample(example T) *YAML[T] {
	y.example = &example
	return y
}

// Name returns the section identifier.
func (y *YAML[T]) Name() string {
	return y.sectionName
}

// Guidance returns the full guidance text including YAML schema derived from T.
func (y *YAML[T]) Guidance() string {
	var sb strings.Builder

	if y.guidance != "" {
		sb.WriteString(y.guidance)
		sb.WriteString("\n\n")
	}

	sb.WriteString(y.sectionName)
	sb.WriteString(" content must be valid YAML matching this schema:\n")

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
		// Publish parse error event (auto-updates stats)
		if execCtx != nil {
			execCtx.PublishParseError(gent.ParseErrorTypeSection, content, parseErr)
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
