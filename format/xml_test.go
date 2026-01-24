package format

import (
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

			var sections []gent.TextOutputSection
			for _, name := range tt.input.sections {
				sections = append(sections, &mockSection{name: name, prompt: ""})
			}
			format.DescribeStructure(sections)

			result, err := format.Parse(tt.input.output)

			assert.ErrorIs(t, err, tt.expected.err)
			assert.Equal(t, tt.expected.sections, result)
		})
	}
}

func TestXML_DescribeStructure(t *testing.T) {
	type input struct {
		sections []string
	}

	type expected struct {
		output string
	}

	tests := []struct {
		name     string
		input    input
		expected expected
	}{
		{
			name: "empty sections returns empty string",
			input: input{
				sections: nil,
			},
			expected: expected{
				output: "",
			},
		},
		{
			name: "single section",
			input: input{
				sections: []string{"answer"},
			},
			expected: expected{
				output: "Format your response using XML-style tags for each section:\n\n" +
					"<answer>\n" +
					"... answer content here ...\n" +
					"</answer>\n",
			},
		},
		{
			name: "multiple sections ignores prompts",
			input: input{
				sections: []string{"thinking", "action"},
			},
			expected: expected{
				output: "Format your response using XML-style tags for each section:\n\n" +
					"<thinking>\n" +
					"... thinking content here ...\n" +
					"</thinking>\n" +
					"<action>\n" +
					"... action content here ...\n" +
					"</action>\n",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			format := NewXML()

			var sections []gent.TextOutputSection
			for _, name := range tt.input.sections {
				sections = append(sections, &mockSection{
					name:   name,
					prompt: "This prompt should be ignored",
				})
			}

			result := format.DescribeStructure(sections)

			assert.Equal(t, tt.expected.output, result)
		})
	}
}
