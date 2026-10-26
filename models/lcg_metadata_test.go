package models_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/rickchristie/gent"
	"github.com/rickchristie/gent/models"
	"github.com/stretchr/testify/assert"
	"github.com/tmc/langchaingo/llms"
)

type metadataCallbackModel struct {
	chunks []*gent.MetaContent
	choice *llms.ContentChoice
}

func (m *metadataCallbackModel) Call(context.Context, string, ...llms.CallOption) (string, error) {
	return "", fmt.Errorf("use GenerateContent")
}

func (m *metadataCallbackModel) GenerateContent(ctx context.Context, _ []llms.MessageContent,
	options ...llms.CallOption,
) (*llms.ContentResponse, error) {
	var opts llms.CallOptions
	for _, option := range options {
		option(&opts)
	}
	for _, chunk := range m.chunks {
		var err error
		if chunk.Type == gent.MetaContentReasoning {
			err = opts.StreamingReasoningFunc(ctx, []byte(chunk.Text), nil)
		} else {
			err = opts.StreamingFunc(ctx, []byte(chunk.Text))
		}
		if err != nil {
			return nil, err
		}
	}
	return &llms.ContentResponse{Choices: []*llms.ContentChoice{m.choice}}, nil
}

func TestLCGWrapper_FinalMetadata(t *testing.T) {
	type input struct {
		chunks []*gent.MetaContent
		choice *llms.ContentChoice
	}
	type expected struct {
		choices []*gent.ContentChoice
		chunks  []*gent.MetaContent
		err     error
	}
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	cases := []testCase{
		{"final reasoning with streamed text", input{
			chunks: []*gent.MetaContent{{Type: gent.MetaContentIntermediary, Text: "Done."}},
			choice: &llms.ContentChoice{Content: "Done.", ReasoningContent: "Checked the record.",
				StopReason: "stop"},
		}, expected{choices: []*gent.ContentChoice{{CandidateContent: "Done.", StopReason: "stop",
			MetaContent: []*gent.MetaContent{
				{Type: gent.MetaContentIntermediary, Text: "Done."},
				{Type: gent.MetaContentReasoning, Text: "Checked the record.",
					MessageId: "reasoning"},
			},
		}}, chunks: []*gent.MetaContent{{Type: gent.MetaContentIntermediary, Text: "Done."}}}},
		{"interleaved callbacks without duplicate final fields", input{
			chunks: []*gent.MetaContent{
				{Type: gent.MetaContentReasoning, Text: "Check ", MessageId: "reasoning"},
				{Type: gent.MetaContentIntermediary, Text: "The order "},
				{Type: gent.MetaContentReasoning, Text: "payment.", MessageId: "reasoning"},
				{Type: gent.MetaContentIntermediary, Text: "is paid."},
			},
			choice: &llms.ContentChoice{Content: "The order is paid.",
				ReasoningContent: "Check payment.", StopReason: "stop"},
		}, expected{choices: []*gent.ContentChoice{{
			CandidateContent: "The order is paid.", StopReason: "stop",
			MetaContent: []*gent.MetaContent{
				{Type: gent.MetaContentReasoning, Text: "Check ", MessageId: "reasoning"},
				{Type: gent.MetaContentIntermediary, Text: "The order "},
				{Type: gent.MetaContentReasoning, Text: "payment.", MessageId: "reasoning"},
				{Type: gent.MetaContentIntermediary, Text: "is paid."},
			},
		}}, chunks: []*gent.MetaContent{
			{Type: gent.MetaContentReasoning, Text: "Check ", MessageId: "reasoning"},
			{Type: gent.MetaContentIntermediary, Text: "The order "},
			{Type: gent.MetaContentReasoning, Text: "payment.", MessageId: "reasoning"},
			{Type: gent.MetaContentIntermediary, Text: "is paid."},
		}}},
		{"partial callback fields", input{
			chunks: []*gent.MetaContent{
				{Type: gent.MetaContentReasoning, Text: "Check ", MessageId: "reasoning"},
				{Type: gent.MetaContentIntermediary, Text: "The order "},
			},
			choice: &llms.ContentChoice{Content: "The order is paid.",
				ReasoningContent: "Check payment.", StopReason: "stop"},
		}, expected{choices: []*gent.ContentChoice{{
			CandidateContent: "The order is paid.", StopReason: "stop",
			MetaContent: []*gent.MetaContent{
				{Type: gent.MetaContentReasoning, Text: "Check ", MessageId: "reasoning"},
				{Type: gent.MetaContentIntermediary, Text: "The order "},
				{Type: gent.MetaContentReasoning, Text: "payment.", MessageId: "reasoning"},
				{Type: gent.MetaContentIntermediary, Text: "is paid."},
			},
		}}, chunks: []*gent.MetaContent{
			{Type: gent.MetaContentReasoning, Text: "Check ", MessageId: "reasoning"},
			{Type: gent.MetaContentIntermediary, Text: "The order "},
		}}},
		{"final text with streamed reasoning", input{
			chunks: []*gent.MetaContent{
				{Type: gent.MetaContentReasoning, Text: "Checked the record.",
					MessageId: "reasoning"},
			},
			choice: &llms.ContentChoice{Content: "Done.", ReasoningContent: "Checked the record.",
				StopReason: "stop"},
		}, expected{choices: []*gent.ContentChoice{{CandidateContent: "Done.", StopReason: "stop",
			MetaContent: []*gent.MetaContent{
				{Type: gent.MetaContentReasoning, Text: "Checked the record.",
					MessageId: "reasoning"},
				{Type: gent.MetaContentIntermediary, Text: "Done."},
			},
		}}, chunks: []*gent.MetaContent{
			{Type: gent.MetaContentReasoning, Text: "Checked the record.", MessageId: "reasoning"},
		}}},
		{"no callbacks", input{choice: &llms.ContentChoice{Content: "Done.",
			ReasoningContent: "Checked the record.", StopReason: "stop"}},
			expected{choices: []*gent.ContentChoice{{CandidateContent: "Done.", StopReason: "stop",
				MetaContent: []*gent.MetaContent{
					{Type: gent.MetaContentReasoning, Text: "Checked the record.",
						MessageId: "reasoning"},
					{Type: gent.MetaContentIntermediary, Text: "Done."},
				},
			}}}},
		{"shorter final field already present in callbacks", input{
			chunks: []*gent.MetaContent{
				{Type: gent.MetaContentReasoning, Text: "Check payment.", MessageId: "reasoning"},
				{Type: gent.MetaContentIntermediary, Text: "Done."},
			},
			choice: &llms.ContentChoice{Content: "Done.", ReasoningContent: "Check ",
				StopReason: "stop"},
		}, expected{choices: []*gent.ContentChoice{{CandidateContent: "Done.", StopReason: "stop",
			MetaContent: []*gent.MetaContent{
				{Type: gent.MetaContentReasoning, Text: "Check payment.", MessageId: "reasoning"},
				{Type: gent.MetaContentIntermediary, Text: "Done."},
			},
		}}, chunks: []*gent.MetaContent{
			{Type: gent.MetaContentReasoning, Text: "Check payment.", MessageId: "reasoning"},
			{Type: gent.MetaContentIntermediary, Text: "Done."},
		}}},
		{"different final field retains both versions", input{
			chunks: []*gent.MetaContent{
				{Type: gent.MetaContentReasoning, Text: "Check payment.", MessageId: "reasoning"},
				{Type: gent.MetaContentIntermediary, Text: "Done."},
			},
			choice: &llms.ContentChoice{Content: "Done.", ReasoningContent: "Checked the record.",
				StopReason: "stop"},
		}, expected{choices: []*gent.ContentChoice{{CandidateContent: "Done.", StopReason: "stop",
			MetaContent: []*gent.MetaContent{
				{Type: gent.MetaContentReasoning, Text: "Check payment.", MessageId: "reasoning"},
				{Type: gent.MetaContentIntermediary, Text: "Done."},
				{Type: gent.MetaContentReasoning, Text: "Checked the record.",
					MessageId: "reasoning"},
			},
		}}, chunks: []*gent.MetaContent{
			{Type: gent.MetaContentReasoning, Text: "Check payment.", MessageId: "reasoning"},
			{Type: gent.MetaContentIntermediary, Text: "Done."},
		}}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			model := &metadataCallbackModel{chunks: test.input.chunks, choice: test.input.choice}
			request := gent.NewModelCallRequest([]gent.ModelMessage{
				gent.TextParts(gent.ChatMessageTypeHuman, "Read the record."),
			})
			wrapper := models.NewLCGWrapper(model)
			stream, err := wrapper.GenerateContentStream(context.Background(), request)
			if !assert.NoError(t, err) {
				return
			}
			defer stream.Close()
			actual := expected{}
			for chunk := range stream.Chunks() {
				assert.NoError(t, chunk.Err)
				assert.Empty(t, chunk.Content)
				actual.chunks = append(actual.chunks, chunk.MetaContent...)
			}
			response, err := stream.Response()
			if !assert.Equal(t, test.expected.err, err) {
				return
			}
			actual.choices, actual.err = response.Choices, err
			assert.Equal(t, test.expected, actual)
		})
	}
}
