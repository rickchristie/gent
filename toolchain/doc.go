// Package toolchain provides implementations for parsing and executing tool calls.
//
// # Overview
//
// A ToolChain is responsible for:
//  1. Explaining to the model what tools are available and their expected inputs
//  2. Parsing the model's output to extract tool calls
//  3. Validating tool arguments against JSON Schema
//  4. Converting intermediary types to Go types and executing tools
//
// # Type Conversion Flow
//
// When processing tool arguments, the toolchain follows this flow:
//
//	Model Output (YAML/JSON) -> Parse -> Intermediary Types -> Validate -> Convert -> Go Types
//
// ## Intermediary Types
//
// JSON Schema is used to define expected inputs. The parser produces intermediary types:
//   - JSON: All values are parsed as JSON types (strings, numbers, booleans, etc.)
//   - YAML: Uses schema-aware parsing; fields with "type": "string" stay as strings
//
// ## Type Conversion
//
// The toolchain automatically converts intermediary types to Go types based on the
// tool's input struct. Supported conversions:
//
//   - string -> time.Time: Parses common date/time formats:
//   - RFC3339: "2006-01-02T15:04:05Z07:00"
//   - RFC3339Nano: "2006-01-02T15:04:05.999999999Z07:00"
//   - ISO without timezone: "2006-01-02T15:04:05"
//   - Date only: "2006-01-02"
//   - Datetime with space: "2006-01-02 15:04:05"
//
//   - string -> time.Duration: Parses using time.ParseDuration:
//   - "1h30m" -> 1 hour 30 minutes
//   - "2h45m30s" -> 2 hours 45 minutes 30 seconds
//   - "500ms" -> 500 milliseconds
//
//   - If target field is `any` or `map[string]any`, intermediary value is used as-is
//
// # Example Usage
//
// Define a tool with time.Time and time.Duration fields:
//
//	type ScheduleInput struct {
//	    EventName string        `json:"event_name"`
//	    StartTime time.Time     `json:"start_time"`
//	    Duration  time.Duration `json:"duration"`
//	}
//
//	tool := gent.NewToolFunc(
//	    "schedule_event",
//	    "Schedule an event",
//	    schema.Object(map[string]*schema.Property{
//	        "event_name": schema.String("Name of the event"),
//	        "start_time": schema.String("Start time (ISO 8601 format)"),
//	        "duration":   schema.String("Duration (e.g., '1h30m')"),
//	    }, "event_name", "start_time", "duration"),
//	    func(ctx context.Context, input ScheduleInput) (string, error) {
//	        // input.StartTime is time.Time
//	        // input.Duration is time.Duration
//	        return "scheduled", nil
//	    },
//	    nil,
//	)
//
// The model provides string values, which are automatically converted:
//
//	args:
//	  event_name: Team Meeting
//	  start_time: 2026-01-20T10:00:00Z
//	  duration: 1h30m
//
// # Available ToolChains
//
//   - [YAML]: Parses YAML-formatted tool calls with schema-aware type handling
//   - [JSON]: Parses JSON-formatted tool calls
package toolchain
