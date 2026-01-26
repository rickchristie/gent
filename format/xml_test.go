package format

import (
	"context"
	"testing"

	"github.com/rickchristie/gent"
	"github.com/stretchr/testify/assert"
)

func TestXML_Parse(t *testing.T) {
	type input struct {
		sections []string
		output   string
		strict   bool
	}

	type expected struct {
		sections map[string][]string
		err      error
	}

	tests := []struct {
		name     string
		input    input
		expected expected
	}{
		{
			name: "single section",
			input: input{
				sections: []string{"answer"},
				output: `<answer>
The weather is sunny today.
</answer>`,
			},
			expected: expected{
				sections: map[string][]string{
					"answer": {"The weather is sunny today."},
				},
				err: nil,
			},
		},
		{
			name: "multiple sections",
			input: input{
				sections: []string{"thinking", "action", "answer"},
				output: `<thinking>
I need to search for weather information.
</thinking>

<action>
{"tool": "search", "args": {"query": "weather"}}
</action>

<answer>
The weather is sunny.
</answer>`,
			},
			expected: expected{
				sections: map[string][]string{
					"thinking": {"I need to search for weather information."},
					"action":   {`{"tool": "search", "args": {"query": "weather"}}`},
					"answer":   {"The weather is sunny."},
				},
				err: nil,
			},
		},
		{
			name: "multiple same section",
			input: input{
				sections: []string{"action"},
				output: `<action>
First action content.
</action>

<action>
Second action content.
</action>`,
			},
			expected: expected{
				sections: map[string][]string{
					"action": {"First action content.", "Second action content."},
				},
				err: nil,
			},
		},
		{
			name: "case insensitive",
			input: input{
				sections: []string{"thinking"},
				output: `<THINKING>
Case insensitive content.
</THINKING>`,
			},
			expected: expected{
				sections: map[string][]string{
					"thinking": {"Case insensitive content."},
				},
				err: nil,
			},
		},
		{
			name: "content outside tags ignored",
			input: input{
				sections: []string{"answer"},
				output: `Some content before tags that should be ignored.

<answer>
The actual answer.
</answer>

Some content after tags.`,
			},
			expected: expected{
				sections: map[string][]string{
					"answer": {"The actual answer."},
				},
				err: nil,
			},
		},
		{
			name: "empty section excluded from results",
			input: input{
				sections: []string{"thinking", "answer"},
				output: `<thinking></thinking>
<answer>The answer.</answer>`,
			},
			expected: expected{
				sections: map[string][]string{
					"answer": {"The answer."},
				},
				err: nil,
			},
		},
		{
			name: "empty action tag with answer should terminate not loop",
			input: input{
				sections: []string{"thinking", "action", "answer"},
				output: `<thinking>
The customer is referring to their "number," likely meaning booking reference number, phone number, or customer ID, based on the context of rescheduling. From the message history, no booking ID, customer ID, email, or other identifiers have been provided by the user. I cannot access or retrieve any personal booking information without these details due to privacy and security policies. Tools like get_customer_info or get_booking_info require customer_id, email, or booking_id, none of which are available.

To proceed with rescheduling AA100 for tomorrow (2026-01-25), I must follow the rescheduling steps: verify identity/booking first. No tools can be called yet without identifiers.

I should respond politely, explain the need for verification for security reasons, and reiterate the required information. Also, offer to look up general flight info for AA100 if helpful, but do not assume or invent details.

No tool actions needed at this stage, as I lack parameters. Proceed to final polite response asking for details.
</thinking>
<action>
</action>
<answer>
Hello! I apologize for any inconvenience. For security and privacy reasons, I don't have access to your personal booking details without verification. This ensures we protect your information and make changes only to the correct booking.

To assist with rescheduling your AA100 flight for tomorrow (January 25, 2026), please provide:
- Your booking reference number (e.g., BK001)
- Your email address or customer ID
- Preferred new travel date and airports (or alternative flight preferences)

Once verified, I'll check availability, our change policy, and any fees right away. Thank you for your understanding!
</answer>`,
			},
			expected: expected{
				sections: map[string][]string{
					"thinking": {
						`The customer is referring to their "number," likely meaning booking ` +
							`reference number, phone number, or customer ID, based on the context ` +
							`of rescheduling. From the message history, no booking ID, customer ` +
							`ID, email, or other identifiers have been provided by the user. I ` +
							`cannot access or retrieve any personal booking information without ` +
							`these details due to privacy and security policies. Tools like ` +
							`get_customer_info or get_booking_info require customer_id, email, ` +
							`or booking_id, none of which are available.

To proceed with rescheduling AA100 for tomorrow (2026-01-25), I must follow the ` +
							`rescheduling steps: verify identity/booking first. No tools can be ` +
							`called yet without identifiers.

I should respond politely, explain the need for verification for security reasons, ` +
							`and reiterate the required information. Also, offer to look up ` +
							`general flight info for AA100 if helpful, but do not assume or ` +
							`invent details.

No tool actions needed at this stage, as I lack parameters. Proceed to final ` +
							`polite response asking for details.`,
					},
					"answer": {
						`Hello! I apologize for any inconvenience. For security and privacy ` +
							`reasons, I don't have access to your personal booking details ` +
							`without verification. This ensures we protect your information and ` +
							`make changes only to the correct booking.

To assist with rescheduling your AA100 flight for tomorrow (January 25, 2026), ` +
							`please provide:
- Your booking reference number (e.g., BK001)
- Your email address or customer ID
- Preferred new travel date and airports (or alternative flight preferences)

Once verified, I'll check availability, our change policy, and any fees right ` +
							`away. Thank you for your understanding!`,
					},
				},
				err: nil,
			},
		},
		{
			name: "same line tag",
			input: input{
				sections: []string{"answer"},
				output:   `<answer>Short answer.</answer>`,
			},
			expected: expected{
				sections: map[string][]string{
					"answer": {"Short answer."},
				},
				err: nil,
			},
		},
		{
			name: "inline multiple sections",
			input: input{
				sections: []string{"thinking", "action", "answer"},
				output:   `<thinking>Quick thought.</thinking><action>do_something</action><answer>Done.</answer>`,
			},
			expected: expected{
				sections: map[string][]string{
					"thinking": {"Quick thought."},
					"action":   {"do_something"},
					"answer":   {"Done."},
				},
				err: nil,
			},
		},
		{
			name: "whitespace trimming",
			input: input{
				sections: []string{"answer"},
				output: `<answer>

   Content with whitespace around it.

</answer>`,
			},
			expected: expected{
				sections: map[string][]string{
					"answer": {"Content with whitespace around it."},
				},
				err: nil,
			},
		},
		{
			name: "no recognized tags returns error",
			input: input{
				sections: []string{"answer"},
				output:   `<unknown>Some content.</unknown>`,
			},
			expected: expected{
				sections: nil,
				err:      gent.ErrNoSectionsFound,
			},
		},
		{
			name: "no tags returns error",
			input: input{
				sections: []string{"answer"},
				output:   `Just some plain text without any tags.`,
			},
			expected: expected{
				sections: nil,
				err:      gent.ErrNoSectionsFound,
			},
		},
		{
			name: "nested content with literal angle brackets",
			input: input{
				sections: []string{"code"},
				output: `<code>
func main() {
    fmt.Println("<hello>")
}
</code>`,
			},
			expected: expected{
				sections: map[string][]string{
					"code": {"func main() {\n    fmt.Println(\"<hello>\")\n}"},
				},
				err: nil,
			},
		},
		{
			name: "LLM writes observation section with text outside tags",
			input: input{
				sections: []string{"thinking", "action", "answer"},
				output: `<thinking>
Reschedule succeeded: BK001 now on AA102 with seat 12A, $0 charge.
Flight details confirmed: AA102 JFK-LAX 2026-01-25 20:00-23:30 UTC.
Parallel tool calls now. After observation, provide <answer>.
</thinking>
<action>
- tool: get_booking_info
  args:
    booking_id: BK001
- tool: send_notification
  args:
    customer_id: C001
    method: email
    subject: "SkyWings Booking BK001 Rescheduled"
    message: "Dear John Smith, your booking has been rescheduled."
</action>
<observation>
[get_booking_info] {"booking_id":"BK001","customer_id":"C001","flight_number":"AA102"}
[send_notification] {"success":true,"method":"email"}
</observation>

A: Tool calls successful.

get_booking_info: BK001 now AA102, JFK-LAX 2026-01-25.

Final answer: Polite confirmation to customer.

Be professional.<answer>
Hello John,

Your booking **BK001** has been successfully rescheduled to **AA102**.

Best regards,
SkyWings Airlines Customer Service
</answer>`,
			},
			expected: expected{
				sections: map[string][]string{
					"thinking": {
						"Reschedule succeeded: BK001 now on AA102 with seat 12A, $0 charge.\n" +
							"Flight details confirmed: AA102 JFK-LAX 2026-01-25 20:00-23:30 UTC.\n" +
							"Parallel tool calls now. After observation, provide <answer>.",
					},
					"action": {
						"- tool: get_booking_info\n" +
							"  args:\n" +
							"    booking_id: BK001\n" +
							"- tool: send_notification\n" +
							"  args:\n" +
							"    customer_id: C001\n" +
							"    method: email\n" +
							"    subject: \"SkyWings Booking BK001 Rescheduled\"\n" +
							"    message: \"Dear John Smith, your booking has been rescheduled.\"",
					},
					"answer": {
						"Hello John,\n\n" +
							"Your booking **BK001** has been successfully rescheduled to **AA102**.\n\n" +
							"Best regards,\n" +
							"SkyWings Airlines Customer Service",
					},
				},
				err: nil,
			},
		},
		{
			name: "strict mode returns error on ambiguity",
			input: input{
				sections: []string{"thinking", "answer"},
				strict:   true,
				output: `<thinking>
After observation, provide <answer>.
</thinking>
<answer>
The actual answer.
</answer>`,
			},
			expected: expected{
				sections: nil,
				err:      ErrAmbiguousTags,
			},
		},
		{
			name: "non-strict mode handles ambiguity",
			input: input{
				sections: []string{"thinking", "answer"},
				strict:   false,
				output: `<thinking>
After observation, provide <answer>.
</thinking>
<answer>
The actual answer.
</answer>`,
			},
			expected: expected{
				sections: map[string][]string{
					"thinking": {"After observation, provide <answer>."},
					"answer":   {"The actual answer."},
				},
				err: nil,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			format := NewXML()
			if tt.input.strict {
				format = format.WithStrict(true)
			}

			for _, name := range tt.input.sections {
				format.RegisterSection(&mockSection{name: name, prompt: ""})
			}

			result, err := format.Parse(nil, tt.input.output)

			assert.ErrorIs(t, err, tt.expected.err)
			assert.Equal(t, tt.expected.sections, result)
		})
	}
}

func TestXML_DescribeStructure(t *testing.T) {
	type input struct {
		name   string
		prompt string
	}

	type expected struct {
		output string
	}

	tests := []struct {
		name     string
		input    []input
		expected expected
	}{
		{
			name:  "empty sections returns empty string",
			input: nil,
			expected: expected{
				output: "",
			},
		},
		{
			name: "single section includes prompt",
			input: []input{
				{name: "answer", prompt: "Write your final answer here."},
			},
			expected: expected{
				output: "Format your response using XML-style tags for each section:\n\n" +
					"<answer>\n" +
					"Write your final answer here.\n" +
					"</answer>\n",
			},
		},
		{
			name: "multiple sections include prompts",
			input: []input{
				{name: "thinking", prompt: "Think through the problem."},
				{name: "action", prompt: "Call a tool to take action."},
			},
			expected: expected{
				output: "Format your response using XML-style tags for each section:\n\n" +
					"<thinking>\n" +
					"Think through the problem.\n" +
					"</thinking>\n" +
					"<action>\n" +
					"Call a tool to take action.\n" +
					"</action>\n",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			format := NewXML()

			for _, s := range tt.input {
				format.RegisterSection(&mockSection{
					name:   s.name,
					prompt: s.prompt,
				})
			}

			result := format.DescribeStructure()

			assert.Equal(t, tt.expected.output, result)
		})
	}
}

func TestXML_Parse_TracesErrors(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected struct {
			shouldError           bool
			formatErrorTotal      int64
			formatErrorConsec     int64
			formatErrorAtIter     int64
		}
	}{
		{
			name:  "parse error traces ParseErrorTrace",
			input: "no sections here",
			expected: struct {
				shouldError           bool
				formatErrorTotal      int64
				formatErrorConsec     int64
				formatErrorAtIter     int64
			}{
				shouldError:       true,
				formatErrorTotal:  1,
				formatErrorConsec: 1,
				formatErrorAtIter: 1,
			},
		},
		{
			name:  "successful parse resets consecutive counter",
			input: "<answer>hello</answer>",
			expected: struct {
				shouldError           bool
				formatErrorTotal      int64
				formatErrorConsec     int64
				formatErrorAtIter     int64
			}{
				shouldError:       false,
				formatErrorTotal:  0,
				formatErrorConsec: 0,
				formatErrorAtIter: 0,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			format := NewXML()
			format.RegisterSection(&mockSection{name: "answer", prompt: "Answer here"})

			// Create execution context with iteration 1
			execCtx := gent.NewExecutionContext(context.Background(), "test", nil)
			execCtx.StartIteration()

			// If we expect success, first set consecutive to 1 to verify reset
			if !tt.expected.shouldError {
				execCtx.Stats().IncrCounter(gent.KeyFormatParseErrorConsecutive, 1)
			}

			_, err := format.Parse(execCtx, tt.input)

			if tt.expected.shouldError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}

			stats := execCtx.Stats()
			assert.Equal(t, tt.expected.formatErrorTotal,
				stats.GetCounter(gent.KeyFormatParseErrorTotal),
				"format error total mismatch")
			assert.Equal(t, tt.expected.formatErrorConsec,
				stats.GetCounter(gent.KeyFormatParseErrorConsecutive),
				"format error consecutive mismatch")
			assert.Equal(t, tt.expected.formatErrorAtIter,
				stats.GetCounter(gent.KeyFormatParseErrorAt+"1"),
				"format error at iteration mismatch")
		})
	}
}

func TestXML_RegisterSection(t *testing.T) {
	type input struct {
		name   string
		prompt string
	}

	type expected struct {
		output string
	}

	tests := []struct {
		name     string
		sections []input
		expected expected
	}{
		{
			name: "single section",
			sections: []input{
				{name: "answer", prompt: "Write your answer."},
			},
			expected: expected{
				output: "Format your response using XML-style tags for each section:\n\n" +
					"<answer>\n" +
					"Write your answer.\n" +
					"</answer>\n",
			},
		},
		{
			name: "multiple sections",
			sections: []input{
				{name: "thinking", prompt: "Think here."},
				{name: "action", prompt: "Act here."},
			},
			expected: expected{
				output: "Format your response using XML-style tags for each section:\n\n" +
					"<thinking>\n" +
					"Think here.\n" +
					"</thinking>\n" +
					"<action>\n" +
					"Act here.\n" +
					"</action>\n",
			},
		},
		{
			name: "idempotent registration",
			sections: []input{
				{name: "answer", prompt: "Write your answer."},
				{name: "answer", prompt: "Different prompt."},
				{name: "answer", prompt: "Another prompt."},
			},
			expected: expected{
				output: "Format your response using XML-style tags for each section:\n\n" +
					"<answer>\n" +
					"Write your answer.\n" +
					"</answer>\n",
			},
		},
		{
			name: "case insensitive idempotency",
			sections: []input{
				{name: "Answer", prompt: "First prompt."},
				{name: "answer", prompt: "Second prompt."},
				{name: "ANSWER", prompt: "Third prompt."},
			},
			expected: expected{
				output: "Format your response using XML-style tags for each section:\n\n" +
					"<Answer>\n" +
					"First prompt.\n" +
					"</Answer>\n",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			format := NewXML()

			for _, s := range tt.sections {
				format.RegisterSection(&mockSection{name: s.name, prompt: s.prompt})
			}

			result := format.DescribeStructure()

			assert.Equal(t, tt.expected.output, result)
		})
	}
}
