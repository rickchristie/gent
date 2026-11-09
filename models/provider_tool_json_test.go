package models

import (
	"context"
	"crypto/rand"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/rickchristie/gent"
	"github.com/rickchristie/gent/agents/react"
	"github.com/rickchristie/gent/executor"
	"github.com/rickchristie/gent/output"
	"github.com/stretchr/testify/assert"
)

type providerLoopValue struct {
	Record string `json:"record"`
	Round  int    `json:"round"`
}

func providerToolJSONAgent(t *testing.T, model gent.Model, record string, choice any,
	calls *[]providerLookupInput, values *[]providerLoopValue,
) *react.Agent[providerLoopValue] {
	t.Helper()
	out, err := output.NewJsonOutput[providerLoopValue]()
	if !assert.NoError(t, err) {
		t.FailNow()
	}
	out.SetCustomValidator("provider-loop-round", func(_ *gent.ExecutionContext,
		value providerLoopValue,
	) (*gent.OutputValidationResult, error) {
		*values = append(*values, value)
		if value != (providerLoopValue{record, 2}) {
			return &gent.OutputValidationResult{Steering: []gent.ContentPart{gent.TextPart(
				"Keep the saved record. Set round to 2. Do not call the tool again.")}}, nil
		}
		return &gent.OutputValidationResult{Accepted: true}, nil
	})
	tool := gent.DefineTool("lookup", func(_ context.Context, value providerLookupInput) (
		*gent.ToolExecutionResponse, error,
	) {
		*calls = append(*calls, value)
		return capturedToolResponse(record, nil)
	}).Description("Read the record for the supplied id.").MustBuild()
	return react.NewWithOutput(model, out).RegisterTool(tool).WithRecoveryVersion("provider-loop/v1").
		WithCallOptions(gent.WithMaxTokens(2048), func(o *gent.CallOptions) { o.ToolChoice = choice })
}

func providerToolJSONTask(out gent.Output[providerLoopValue]) *gent.Task {
	return &gent.Task{Messages: []*gent.ModelMessage{
		gent.TextParts(gent.ChatMessageTypeSystem, out.Instructions()),
		gent.TextParts(gent.ChatMessageTypeHuman, `Call lookup exactly once with id 7.
Use the returned text as record. Return the record and round 1 in the required JSON.
Do not guess the record. Use saved completed tool results without repeating their calls.`),
	}}
}

func checkProviderToolJSON(t *testing.T, base context.Context, model gent.Model, record string,
	choice any,
) {
	t.Helper()
	type expected struct {
		value       providerLoopValue
		calls       []providerLookupInput
		validations []providerLoopValue
		rejected    int64
		result      *gent.ExecutionResult
		err         error
	}
	actual := expected{}
	agent := providerToolJSONAgent(t, model, record, choice, &actual.calls, &actual.validations)
	ctx := gent.NewExecutionContext(base, "automatic tools",
		gent.NewBasicLoopData(providerToolJSONTask(agent.Output())))
	ctx.SetLimits([]gent.Limit{{Type: gent.LimitExactKey, Key: gent.SCIterations, MaxValue: 4}})
	defer logCompactionFailure(t, ctx)
	actual.value, actual.result, actual.err = executor.Run(ctx, agent, executor.DefaultConfig())
	actual.rejected = ctx.Stats().GetCounter(gent.SCOutputCustomRejectedTotal)
	assert.Equal(t, expected{providerLoopValue{record, 2}, []providerLookupInput{{7}},
		[]providerLoopValue{{record, 1}, {record, 2}}, 1,
		&gent.ExecutionResult{TerminationReason: gent.TerminationSuccess, Output: []gent.ContentPart{
			gent.TextPart(fmt.Sprintf(`{"record":%q,"round":2}`, record))}}, nil}, actual)
}

func TestProvidersAutomaticToolJSON(t *testing.T) {
	type input struct {
		provider string
		choice   any
	}
	type expected struct{ requests int }
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	var cases []testCase
	for _, provider := range testProviders {
		cases = append(cases, testCase{provider + "/default", input{provider, nil}, expected{3}})
		// Ollama has no ToolChoice option; its default path still runs this complete loop.
		if provider != "ollama" {
			cases = append(cases, testCase{provider + "/auto", input{provider, "auto"}, expected{3}})
		}
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			requests := 0
			client := &http.Client{Transport: responseTransport(func(request *http.Request) (
				*http.Response, error,
			) {
				if request.Method == http.MethodGet {
					return fixtureHTTP(routerFixture, "application/json"), nil
				}
				requests++
				response := providerResponse(test.input.provider, 0)
				if requests > 1 {
					response = providerTextResponse(t, test.input.provider,
						fmt.Sprintf(`{"record":"record-seven","round":%d}`, requests-1))
				}
				return fixtureHTTP(response, "application/json"), nil
			})}
			checkProviderToolJSON(t, context.Background(), fixtureModel(test.input.provider, client),
				"record-seven", test.input.choice)
			assert.Equal(t, test.expected, expected{requests})
		})
	}
}

func TestLiveProviderToolJSON(t *testing.T) {
	type input struct{ choice any }
	type testCase struct {
		name  string
		input input
	}
	cases := []testCase{{"default", input{nil}}, {"auto", input{"auto"}}}
	for _, provider := range liveProviders() {
		t.Run(provider.name, func(t *testing.T) {
			config := liveConfig(t, provider, false)
			t.Parallel()
			for _, test := range cases {
				if provider.prefix == "OLLAMA" && test.input.choice != nil {
					continue
				}
				t.Run(test.name, func(t *testing.T) {
					ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
					defer cancel()
					checkProviderToolJSON(t, ctx, newLiveModel(t, provider, &config),
						"record-"+rand.Text(), test.input.choice)
				})
			}
		})
	}
}
