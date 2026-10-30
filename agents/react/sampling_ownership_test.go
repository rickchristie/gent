package react

import genttest "github.com/rickchristie/gent/internal/tt"

import (
	"context"
	"fmt"
	"testing"

	"github.com/rickchristie/gent"
	"github.com/stretchr/testify/assert"
)

type samplingHook struct{ change func(*gent.CallOptions) }

func (h *samplingHook) MaxRecursion() int { return 10 }
func (h *samplingHook) Dispatch(_ *gent.ExecutionContext, event gent.Event) {
	if before, ok := event.(*gent.BeforeModelCallEvent); ok {
		h.change(&before.Request.Options)
	}
}

// The reply exposes the values received by the adapter without shared test state.
type samplingEchoModel struct{}

func (*samplingEchoModel) GenerateContentStream(_ context.Context, request gent.ModelCallRequest) (
	gent.Stream, error,
) {
	content := fmt.Sprintf("%.1f %.1f", *request.Options.Temperature, *request.Options.TopP)
	response := &gent.ContentResponse{
		Choices: []*gent.ContentChoice{
			{
				Completion:       gent.ModelCompletion{Status: gent.ModelStatusAnswer},
				CandidateContent: []gent.ContentPart{gent.TextContent{Text: content}},
			},
		},
	}
	return gent.NewCompletedStream(response, nil), nil
}

func TestAgentSamplingHooksOwnOptions(t *testing.T) {
	type input struct{ change func(*gent.CallOptions) }
	type callResult struct {
		result *gent.AgentLoopResult
		err    error
	}
	type expected struct{ calls []callResult }
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	answer := func(content string) callResult {
		return callResult{result: &gent.AgentLoopResult{TerminationReason: gent.TerminationSuccess,
			Action: gent.LATerminate,
			Result: []gent.ContentPart{
				gent.TextContent{Text: content},
			},
			Value: genttest.Value(gent.ContentText([]gent.ContentPart{
				gent.TextContent{Text: content},
			}))}}
	}
	cases := []testCase{
		{"temperature", input{func(o *gent.CallOptions) { *o.Temperature = 0.1 }},
			expected{[]callResult{answer("0.1 0.9"), answer("0.8 0.9")}}},
		{"top p", input{func(o *gent.CallOptions) { *o.TopP = 0.1 }},
			expected{[]callResult{answer("0.8 0.1"), answer("0.8 0.9")}}},
		{"explicit zero", input{func(o *gent.CallOptions) { *o.Temperature, *o.TopP = 0, 0 }},
			expected{[]callResult{answer("0.0 0.0"), answer("0.8 0.9")}}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			loop := NewAgent(&samplingEchoModel{}).
				WithCallOptions(gent.WithTemperature(0.8), gent.WithTopP(0.9))
			var actual expected
			for call := range 2 {
				ctx := newTestExecCtx(gent.NewBasicLoopData(gent.NewTask(gent.TextContent{
					Text: "Give the answer.",
				})))
				if call == 0 {
					ctx.SetEventPublisher(&samplingHook{change: test.input.change})
				}
				result, err := loop.Next(ctx)
				actual.calls = append(actual.calls, callResult{result, err})
				test.expected.calls[call].result.ContentSource = nativeTestSource(ctx, 1)
			}
			assert.Equal(t, test.expected, actual)
		})
	}
}

func TestAgentSamplingHooksConcurrent(t *testing.T) {
	type expected struct {
		result *gent.AgentLoopResult
		err    error
	}
	loop := NewAgent(&samplingEchoModel{}).
		WithCallOptions(gent.WithTemperature(0.8), gent.WithTopP(0.9))
	ready := make(chan struct{}, 2)
	changed := make(chan struct{}, 2)
	start := make(chan struct{})
	finish := make(chan struct{})
	results := make([]chan expected, 2)
	contexts := make([]*gent.ExecutionContext, 2)
	for worker := range results {
		contexts[worker] = newTestExecCtx(gent.NewBasicLoopData(gent.NewTask(gent.TextContent{
			Text: "Give the answer.",
		})))
		results[worker] = make(chan expected, 1)
		go func() {
			ctx := contexts[worker]
			ctx.SetEventPublisher(&samplingHook{change: func(o *gent.CallOptions) {
				ready <- struct{}{}
				<-start
				*o.Temperature = float64(worker) / 10
				*o.TopP = float64(worker+1) / 10
				changed <- struct{}{}
				<-finish
			}})
			result, err := loop.Next(ctx)
			results[worker] <- expected{result, err}
		}()
	}
	for range results {
		<-ready
	}
	close(start)
	for range results {
		<-changed
	}
	close(finish)
	// Both hooks have changed their own values before either adapter reads them.
	for worker, content := range []string{"0.0 0.1", "0.1 0.2"} {
		actual := <-results[worker]
		want := expected{result: &gent.AgentLoopResult{TerminationReason: gent.TerminationSuccess,
			Action:        gent.LATerminate,
			Result:        []gent.ContentPart{gent.TextContent{Text: content}},
			ContentSource: nativeTestSource(contexts[worker], 1),
			Value: genttest.Value(gent.ContentText([]gent.ContentPart{
				gent.TextContent{Text: content},
			})),
		}}
		assert.Equal(t, want, actual)
	}
}
