// Package section provides implementations for parsing specific output sections.
//
// # Overview
//
// A TextSection defines a named portion of the LLM's output that can be
// independently parsed. Sections are registered with a [gent.TextFormat]
// to define the expected output structure.
//
// # Available Sections
//
//   - [Text]: Free-form text content, no parsing
//   - [JSON]: Structured JSON parsed into a Go type
//   - [YAML]: Structured YAML parsed into a Go type
//
// # Choosing a Section Type
//
// Use [Text] when:
//   - Content format doesn't matter (thinking, explanations)
//   - The section is for model reasoning, not data extraction
//   - You want the simplest possible section
//
// Use [JSON] when:
//   - You need strongly typed data from a section
//   - The content will be processed programmatically
//   - Working with models that output JSON well
//
// Use [YAML] when:
//   - You need strongly typed data from a section
//   - The content may include multiline strings
//   - You prefer YAML's simpler syntax for LLM output
//
// # Sections vs Terminations
//
// Sections ([Text], [JSON], [YAML]) parse content during the agent loop
// for intermediate processing. Terminations ([termination.Text], [termination.JSON])
// determine when the loop should end and return the final answer.
//
// Use sections for:
//   - Thinking/reasoning that guides the agent
//   - Intermediate structured data
//   - Plans or decisions within the loop
//
// Use terminations for:
//   - Final answers to return to the user
//   - Structured results that end the loop
//
// # Example Usage
//
//	// Text section for reasoning
//	thinking := section.NewText("thinking").
//	    WithGuidance("Think through the problem step by step.")
//
//	// JSON section for structured intermediate data
//	type Classification struct {
//	    Category   string  `json:"category"`
//	    Confidence float64 `json:"confidence"`
//	}
//	classify := section.NewJSON[Classification]("classify").
//	    WithGuidance("Classify the input.").
//	    WithExample(Classification{Category: "support", Confidence: 0.95})
//
//	// Register sections with format
//	textFormat := format.NewXML().
//	    RegisterSection(thinking).
//	    RegisterSection(classify).
//	    RegisterSection(toolchain).
//	    RegisterSection(termination)
//
// # Schema Generation
//
// The [JSON] and [YAML] sections use [GenerateJSONSchema] to automatically
// create JSON Schema from Go types. This schema is included in the guidance
// to help the model produce correctly structured output.
//
// Supported struct tags:
//   - json/yaml: Field naming (e.g., `json:"field_name"`)
//   - omitempty: Marks field as optional
//   - description: Adds description to schema (e.g., `description:"helpful text"`)
package section
