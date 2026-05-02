package search

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func wordCount(s string) int { return len(strings.Fields(s)) }

// ============================================================================
// Core mechanics — plain text fallback (no Markdown headings)
// ============================================================================

func TestMarkdownChunker_PlainTextFallback(t *testing.T) {
	type input struct {
		text      string
		chunkSize int
	}
	type expected struct {
		chunks []Chunk
	}
	tests := []struct {
		name     string
		input    input
		expected expected
	}{
		{
			name:     "empty text returns nil",
			input:    input{text: "", chunkSize: 256},
			expected: expected{chunks: nil},
		},
		{
			name:     "whitespace only returns nil",
			input:    input{text: "   \n\n  \t  ", chunkSize: 256},
			expected: expected{chunks: nil},
		},
		{
			name:  "short text returns single chunk",
			input: input{text: "Hello world.", chunkSize: 256},
			expected: expected{chunks: []Chunk{
				{Text: "Hello world."},
			}},
		},
		{
			name: "splits at paragraph boundaries",
			input: input{
				text: `First paragraph.

Second paragraph.`,
				chunkSize: 3,
			},
			expected: expected{chunks: []Chunk{
				{Text: "First paragraph."},
				{Text: "Second paragraph."},
			}},
		},
		{
			name: "falls through to sentence splitting",
			input: input{
				text:      "First sentence here. Second sentence here. Third sentence here.",
				chunkSize: 4,
			},
			expected: expected{chunks: []Chunk{
				{Text: "First sentence here."},
				{Text: "Second sentence here."},
				{Text: "Third sentence here."},
			}},
		},
		{
			name:  "falls through to word splitting",
			input: input{text: "alpha bravo charlie delta echo foxtrot", chunkSize: 2},
			expected: expected{chunks: []Chunk{
				{Text: "alpha bravo"},
				{Text: "charlie delta"},
				{Text: "echo foxtrot"},
			}},
		},
		{
			name:  "text exactly at chunk size stays as one chunk",
			input: input{text: "one two three four five", chunkSize: 5},
			expected: expected{chunks: []Chunk{
				{Text: "one two three four five"},
			}},
		},
		{
			name:  "text one token over limit triggers split",
			input: input{text: "one two three four five six", chunkSize: 5},
			expected: expected{chunks: []Chunk{
				{Text: "one two three four five"},
				{Text: "six"},
			}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			chunker := &MarkdownChunker{ChunkSize: tt.input.chunkSize, TokenCount: wordCount}
			assert.Equal(t, tt.expected.chunks, chunker.Chunk(tt.input.text))
		})
	}
}

func TestMarkdownChunker_NilTokenCount(t *testing.T) {
	chunker := &MarkdownChunker{ChunkSize: 3, TokenCount: nil}
	assert.Equal(t, []Chunk{
		{Text: "First paragraph."},
		{Text: "Second paragraph."},
	}, chunker.Chunk(`First paragraph.

Second paragraph.`))
}

// ============================================================================
// Heading ancestor tracking — compact format
// ============================================================================

func TestMarkdownChunker_HeadingAncestors(t *testing.T) {
	doc := `# Title

## Section A

### Sub A1

Content A1 here.

### Sub A2

Content A2 here.

## Section B

Content B here.`

	chunker := &MarkdownChunker{ChunkSize: 20, TokenCount: wordCount}
	assert.Equal(t, []Chunk{
		{
			Text:     "# Title",
			Metadata: nil,
		},
		{
			Text: `h1: Title
## Section A`,
			Metadata: map[string]string{"h1": "Title"},
		},
		{
			Text: `h1: Title | h2: Section A
### Sub A1

Content A1 here.`,
			Metadata: map[string]string{"h1": "Title", "h2": "Section A"},
		},
		{
			Text: `h1: Title | h2: Section A
### Sub A2

Content A2 here.`,
			Metadata: map[string]string{"h1": "Title", "h2": "Section A"},
		},
		{
			Text: `h1: Title
## Section B

Content B here.`,
			Metadata: map[string]string{"h1": "Title"},
		},
	}, chunker.Chunk(doc))
}

func TestMarkdownChunker_HeadingStackResets(t *testing.T) {
	doc := `# Doc

## A

### A1

Content A1.

## B

### B1

Content B1.`

	chunker := &MarkdownChunker{ChunkSize: 20, TokenCount: wordCount}
	chunks := chunker.Chunk(doc)

	var found bool
	for _, ch := range chunks {
		if strings.Contains(ch.Text, "Content B1") {
			found = true
			assert.Equal(t, Chunk{
				Text: `h1: Doc | h2: B
### B1

Content B1.`,
				Metadata: map[string]string{"h1": "Doc", "h2": "B"},
			}, ch)
			break
		}
	}
	assert.True(t, found, "should find chunk containing Content B1")
}

// ============================================================================
// Deep nesting — ancestor overhead forces split
// ============================================================================

// This test proves that ancestor prefix overhead is accounted for in the chunk budget.
// The section content (177 words) fits within 190 tokens alone. But the 3-level ancestor
// prefix adds ~15 words, pushing total to ~192 and forcing a split into 2 chunks.
var deepNestedSOP = `# Airline Standard Operating Procedures

## Customer Service Department

### Complaint Handling Process

#### Written Complaint Response

All written complaints received via email or postal mail must be acknowledged within 24 hours of receipt. The acknowledgment must include the complaint reference number, the name of the assigned case handler, and an estimated response timeline. For complaints involving safety concerns, the acknowledgment must also include contact information for the safety department and a statement that the matter has been flagged for priority review.

The case handler must review all relevant documentation including booking records, flight logs, crew reports, and any previous correspondence with the customer. A detailed response must be drafted within 5 business days addressing each specific concern raised by the customer. The response must reference specific policies and procedures that were followed, explain any deviations from standard practice, and outline corrective actions taken or planned.

If the complaint requires compensation, the case handler must follow the compensation guidelines in section 4.2. Compensation offers must be approved by a supervisor before being communicated to the customer. The complete complaint file must be retained for a minimum of 3 years from the date of resolution.`

func TestMarkdownChunker_DeepNesting_AncestorOverheadForcesSplit(t *testing.T) {
	chunker := &MarkdownChunker{ChunkSize: 190, TokenCount: wordCount}
	chunks := chunker.Chunk(deepNestedSOP)

	assert.Equal(t, []Chunk{
		{
			Text:     "# Airline Standard Operating Procedures",
			Metadata: nil,
		},
		{
			Text: `h1: Airline Standard Operating Procedures
## Customer Service Department`,
			Metadata: map[string]string{"h1": "Airline Standard Operating Procedures"},
		},
		{
			Text: `h1: Airline Standard Operating Procedures | h2: Customer Service Department
### Complaint Handling Process`,
			Metadata: map[string]string{
				"h1": "Airline Standard Operating Procedures",
				"h2": "Customer Service Department",
			},
		},
		{
			Text: `h1: Airline Standard Operating Procedures | h2: Customer Service Department | h3: Complaint Handling Process
#### Written Complaint Response

All written complaints received via email or postal mail must be acknowledged within 24 hours of receipt. The acknowledgment must include the complaint reference number, the name of the assigned case handler, and an estimated response timeline. For complaints involving safety concerns, the acknowledgment must also include contact information for the safety department and a statement that the matter has been flagged for priority review.

The case handler must review all relevant documentation including booking records, flight logs, crew reports, and any previous correspondence with the customer. A detailed response must be drafted within 5 business days addressing each specific concern raised by the customer. The response must reference specific policies and procedures that were followed, explain any deviations from standard practice, and outline corrective actions taken or planned.`,
			Metadata: map[string]string{
				"h1": "Airline Standard Operating Procedures",
				"h2": "Customer Service Department",
				"h3": "Complaint Handling Process",
			},
		},
		{
			Text: `h1: Airline Standard Operating Procedures | h2: Customer Service Department | h3: Complaint Handling Process
If the complaint requires compensation, the case handler must follow the compensation guidelines in section 4.2. Compensation offers must be approved by a supervisor before being communicated to the customer. The complete complaint file must be retained for a minimum of 3 years from the date of resolution.`,
			Metadata: map[string]string{
				"h1": "Airline Standard Operating Procedures",
				"h2": "Customer Service Department",
				"h3": "Complaint Handling Process",
			},
		},
	}, chunks)
}

// ============================================================================
// Realistic airline SOP
// ============================================================================

var airlineSOP = `# Airline Customer Service Standard Operating Procedures

## 1. Flight Rebooking and Schedule Changes

When a customer requests to change their flight, follow these steps in order. First, verify the customer's identity by confirming their booking reference, full name, and email address on file. Never proceed with any changes without positive identity verification.

Check the booking status. Only confirmed bookings are eligible for rebooking. Cancelled bookings must be reinstated first through a separate process. Checked-in bookings can be rebooked but the customer must be informed that their check-in will be voided and they will need to check in again.

For same-day changes, check seat availability on the requested flight. If seats are available in the same cabin class, process the change with no fare difference for Gold and Platinum frequent flyer members. Silver and Bronze members pay the published same-day change fee of $75 per passenger per segment. If the customer needs to change to a different cabin class, quote the fare difference plus any applicable change fees.

## 2. Cancellation and Refund Policy

Cancellation requests must be processed according to the fare rules associated with the ticket. Fully refundable tickets (Flex fare) can be cancelled at any time with a full refund to the original payment method. Processing time is 5-7 business days for credit card refunds and 10-14 business days for bank transfers.

## 3. Baggage Handling

When a customer reports missing baggage, create a Property Irregularity Report immediately. Record the flight details, baggage tag numbers, and a description of each missing bag.`

func TestMarkdownChunker_AirlineSOP_256Tokens(t *testing.T) {
	chunker := &MarkdownChunker{ChunkSize: 256, TokenCount: wordCount}
	assert.Equal(t, []Chunk{
		{
			Text:     "# Airline Customer Service Standard Operating Procedures",
			Metadata: nil,
		},
		{
			Text: `h1: Airline Customer Service Standard Operating Procedures
## 1. Flight Rebooking and Schedule Changes

When a customer requests to change their flight, follow these steps in order. First, verify the customer's identity by confirming their booking reference, full name, and email address on file. Never proceed with any changes without positive identity verification.

Check the booking status. Only confirmed bookings are eligible for rebooking. Cancelled bookings must be reinstated first through a separate process. Checked-in bookings can be rebooked but the customer must be informed that their check-in will be voided and they will need to check in again.

For same-day changes, check seat availability on the requested flight. If seats are available in the same cabin class, process the change with no fare difference for Gold and Platinum frequent flyer members. Silver and Bronze members pay the published same-day change fee of $75 per passenger per segment. If the customer needs to change to a different cabin class, quote the fare difference plus any applicable change fees.`,
			Metadata: map[string]string{
				"h1": "Airline Customer Service Standard Operating Procedures",
			},
		},
		{
			Text: `h1: Airline Customer Service Standard Operating Procedures
## 2. Cancellation and Refund Policy

Cancellation requests must be processed according to the fare rules associated with the ticket. Fully refundable tickets (Flex fare) can be cancelled at any time with a full refund to the original payment method. Processing time is 5-7 business days for credit card refunds and 10-14 business days for bank transfers.`,
			Metadata: map[string]string{
				"h1": "Airline Customer Service Standard Operating Procedures",
			},
		},
		{
			Text: `h1: Airline Customer Service Standard Operating Procedures
## 3. Baggage Handling

When a customer reports missing baggage, create a Property Irregularity Report immediately. Record the flight details, baggage tag numbers, and a description of each missing bag.`,
			Metadata: map[string]string{
				"h1": "Airline Customer Service Standard Operating Procedures",
			},
		},
	}, chunker.Chunk(airlineSOP))
}

// ============================================================================
// Realistic ecommerce policy
// ============================================================================

var ecommercePolicy = `# E-Commerce Returns and Refund Policy

## Eligibility for Returns

Products may be returned within 30 days of delivery for a full refund, provided they are in their original condition with all tags attached and original packaging intact. Electronics and appliances must include all accessories, manuals, and cables that were included in the original shipment.

The following items are not eligible for return: personalized or custom-made products, intimate apparel and swimwear for hygiene reasons, perishable goods including food and flowers, downloadable software and digital content once accessed, and gift cards or store credit vouchers.

## Refund Processing

Refunds are processed to the original payment method within 5-7 business days after we receive and inspect the returned item. Credit card refunds may take an additional 3-5 business days to appear on the customer's statement depending on their bank's processing time.

## Dispute Resolution

If a customer disputes a charge related to an order, the billing team investigates within 48 hours. For double charges, the duplicate payment is refunded immediately without requiring a return.`

func TestMarkdownChunker_EcommercePolicy_384Tokens(t *testing.T) {
	chunker := &MarkdownChunker{ChunkSize: 384, TokenCount: wordCount}
	assert.Equal(t, []Chunk{
		{
			Text:     "# E-Commerce Returns and Refund Policy",
			Metadata: nil,
		},
		{
			Text: `h1: E-Commerce Returns and Refund Policy
## Eligibility for Returns

Products may be returned within 30 days of delivery for a full refund, provided they are in their original condition with all tags attached and original packaging intact. Electronics and appliances must include all accessories, manuals, and cables that were included in the original shipment.

The following items are not eligible for return: personalized or custom-made products, intimate apparel and swimwear for hygiene reasons, perishable goods including food and flowers, downloadable software and digital content once accessed, and gift cards or store credit vouchers.`,
			Metadata: map[string]string{"h1": "E-Commerce Returns and Refund Policy"},
		},
		{
			Text: `h1: E-Commerce Returns and Refund Policy
## Refund Processing

Refunds are processed to the original payment method within 5-7 business days after we receive and inspect the returned item. Credit card refunds may take an additional 3-5 business days to appear on the customer's statement depending on their bank's processing time.`,
			Metadata: map[string]string{"h1": "E-Commerce Returns and Refund Policy"},
		},
		{
			Text: `h1: E-Commerce Returns and Refund Policy
## Dispute Resolution

If a customer disputes a charge related to an order, the billing team investigates within 48 hours. For double charges, the duplicate payment is refunded immediately without requiring a return.`,
			Metadata: map[string]string{"h1": "E-Commerce Returns and Refund Policy"},
		},
	}, chunker.Chunk(ecommercePolicy))
}

// ============================================================================
// Tool description — always single chunk
// ============================================================================

func TestMarkdownChunker_ToolDescription(t *testing.T) {
	toolText := `get_billing_ledger: Retrieve billing ledger entries
Domain: Billing
Categories: lookup, billing
Keywords: billing, payment, invoice
Example queries: check payment status; look up invoices`

	for _, size := range []int{200, 256, 384, 512} {
		chunker := &MarkdownChunker{ChunkSize: size, TokenCount: wordCount}
		assert.Equal(t, []Chunk{{Text: toolText}}, chunker.Chunk(toolText),
			"tool description should be single chunk at size %d", size)
	}
}

// ============================================================================
// Indonesian content
// ============================================================================

func TestMarkdownChunker_Indonesian_256Tokens(t *testing.T) {
	indonesian := `# Kebijakan Pembatalan Reservasi Hotel

## Pembatalan Standar

Tamu dapat membatalkan reservasi hingga 24 jam sebelum tanggal check-in tanpa dikenakan biaya pembatalan.

## Pengembalian Dana

Pengembalian dana untuk pembatalan yang memenuhi syarat akan diproses dalam waktu 5-7 hari kerja.`

	chunker := &MarkdownChunker{ChunkSize: 256, TokenCount: wordCount}
	assert.Equal(t, []Chunk{
		{
			Text:     "# Kebijakan Pembatalan Reservasi Hotel",
			Metadata: nil,
		},
		{
			Text: `h1: Kebijakan Pembatalan Reservasi Hotel
## Pembatalan Standar

Tamu dapat membatalkan reservasi hingga 24 jam sebelum tanggal check-in tanpa dikenakan biaya pembatalan.`,
			Metadata: map[string]string{"h1": "Kebijakan Pembatalan Reservasi Hotel"},
		},
		{
			Text: `h1: Kebijakan Pembatalan Reservasi Hotel
## Pengembalian Dana

Pengembalian dana untuk pembatalan yang memenuhi syarat akan diproses dalam waktu 5-7 hari kerja.`,
			Metadata: map[string]string{"h1": "Kebijakan Pembatalan Reservasi Hotel"},
		},
	}, chunker.Chunk(indonesian))
}

// ============================================================================
// Markdown with code blocks
// ============================================================================

func TestMarkdownChunker_CodeBlocks(t *testing.T) {
	markdown := "# Setup\n\n## Install\n\nRun this command:\n\n" +
		"```bash\ngent setup onnx\n```\n\n" +
		"## Configure\n\nEdit your config file."

	chunker := &MarkdownChunker{ChunkSize: 256, TokenCount: wordCount}
	assert.Equal(t, []Chunk{
		{
			Text:     "# Setup",
			Metadata: nil,
		},
		{
			Text: "h1: Setup\n## Install\n\nRun this command:\n\n" +
				"```bash\ngent setup onnx\n```",
			Metadata: map[string]string{"h1": "Setup"},
		},
		{
			Text:     "h1: Setup\n## Configure\n\nEdit your config file.",
			Metadata: map[string]string{"h1": "Setup"},
		},
	}, chunker.Chunk(markdown))
}
