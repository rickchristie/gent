package testutil

import (
	"context"
	"io"
	"testing"

	"github.com/rickchristie/gent"
	"github.com/stretchr/testify/assert"
)

type chatTaskModel struct{ messages [][]*gent.ModelMessage }

func (model *chatTaskModel) GenerateContentStream(
	_ context.Context, request gent.ModelCallRequest,
) (
	gent.Stream, error,
) {
	model.messages = append(model.messages, request.Messages)
	return gent.NewCompletedStream(&gent.ContentResponse{Choices: []*gent.ContentChoice{{
		Completion:       gent.ModelCompletion{Status: gent.ModelStatusAnswer},
		CandidateContent: []gent.ContentPart{gent.TextContent{Text: "Done."}},
	}}, Info: &gent.GenerationInfo{}}, nil), nil
}

func TestInteractiveChatKeepsMessageRoles(t *testing.T) {
	type input struct{ history []ConversationMessage }
	type expected struct {
		messages [][]*gent.ModelMessage
		history  []ConversationMessage
		err      string
	}
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	const system = `Help the customer.

Use supplied facts.`
	cases := []testCase{
		{"first turn", input{}, expected{messages: [][]*gent.ModelMessage{{
			gent.TextParts(gent.ChatMessageTypeSystem, system),
			gent.TextParts(gent.ChatMessageTypeHuman, "Read."),
		}}, history: []ConversationMessage{{"user", "Read."}, {"agent", "Done."}}}},
		{"prior conversation", input{[]ConversationMessage{{"user", "Earlier."}, {"agent", "Ready."}}},
			expected{messages: [][]*gent.ModelMessage{{
				gent.TextParts(gent.ChatMessageTypeSystem, system),
				gent.TextParts(gent.ChatMessageTypeHuman, "Earlier."),
				gent.TextParts(gent.ChatMessageTypeAI, "Ready."),
				gent.TextParts(gent.ChatMessageTypeHuman, "Read."),
			}}, history: []ConversationMessage{{"user", "Earlier."}, {"agent", "Ready."},
				{"user", "Read."}, {"agent", "Done."}}}},
		{"invalid role", input{[]ConversationMessage{{"unknown", "Earlier."}}},
			expected{err: `unsupported chat role "unknown"`,
				history: []ConversationMessage{{"unknown", "Earlier."}, {"user", "Read."}}}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			model := &chatTaskModel{}
			chat := &InteractiveChat{History: test.input.history, Model: model, Writer: io.Discard,
				Config: TestConfig{ToolChain: ToolChainRegistry}, ChatCfg: ChatConfig{
					Name: "roles", SystemPrompt: "Help the customer.", CriticalRules: "Use supplied facts.",
					MaxIterations: 3, RegisterTools: func(gent.ToolChain) {},
				}}
			err := chat.SendMessage(context.Background(), "Read.")
			if test.expected.err == "" {
				assert.NoError(t, err)
			} else {
				assert.EqualError(t, err, test.expected.err)
			}
			assert.Equal(t, test.expected.messages, model.messages)
			assert.Equal(t, test.expected.history, chat.History)
		})
	}
}
