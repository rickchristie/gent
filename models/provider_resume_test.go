package models

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/rickchristie/gent"
	"github.com/rickchristie/gent/compaction"
	"github.com/rickchristie/gent/executor"
	"github.com/rickchristie/gent/format"
	"github.com/stretchr/testify/assert"
)

// Capture an accepted storage boundary, then interrupt the worker. The saved bytes are fixed
// before interruption handling runs, as they would be if that process stopped at this boundary.
type providerResumeStore struct {
	view   gent.RunView
	codecs *gent.CodecRegistry
	stop   func(*gent.SaveBatch) bool
	cancel context.CancelFunc
	saved  []byte
}

func (s *providerResumeStore) Save(_ context.Context, batch *gent.SaveBatch) error {
	next, err := gent.ApplyBatch(s.view, batch, s.codecs)
	if err != nil {
		return err
	}
	s.view = next
	if s.saved != nil || s.stop == nil || !s.stop(batch) {
		return nil
	}
	s.saved, err = s.codecs.Marshal(next)
	if err != nil {
		return err
	}
	s.cancel()
	return nil
}

func providerResumeBoundary(name string) func(*gent.SaveBatch) bool {
	responses := 0
	return func(batch *gent.SaveBatch) bool {
		if name == "model response" {
			for _, event := range batch.Events {
				if event.Type == "model_call_finished" {
					responses++
				}
			}
			// Stop after the tool result was used, before processing its first JSON answer.
			return responses == 2
		}
		checkpoint := batch.Checkpoint
		if checkpoint == nil {
			return false
		}
		switch name {
		case "tool result":
			return checkpoint.Phase == gent.PhaseToolCompleted
		case "rejected output":
			return checkpoint.Phase == gent.PhaseIterationCompleted &&
				checkpoint.Contexts[0].Iteration == 2
		case "compacted history":
			return checkpoint.Phase == gent.PhaseCompactionCompleted
		default:
			panic("unknown resume boundary")
		}
	}
}

func configureProviderResume(ctx *gent.ExecutionContext, model gent.Model, compact bool) {
	if compact {
		ctx.SetCompaction(compaction.NewStatThresholdTrigger().OnGauge(gent.SGScratchpadLength, 4),
			compaction.NewSummarization(model, format.NewMarkdown()).WithKeepRecent(1))
	}
}

func checkProviderResume(t *testing.T, base context.Context, build func() gent.Model, record string,
	compact bool, stop func(*gent.SaveBatch) bool,
) *gent.ExecutionContext {
	t.Helper()
	worker, cancel := context.WithCancel(base)
	defer cancel()
	codecs := gent.NewCodecRegistry()
	store := &providerResumeStore{codecs: codecs, stop: stop, cancel: cancel,
		view: gent.RunView{SchemaVersion: gent.RunSchemaVersion, RunId: "provider-resume"}}
	var calls []providerLookupInput
	var values []providerLoopValue
	model := build()
	agent := providerToolJSONAgent(t, model, record, nil, &calls, &values)
	options := []gent.ContextOption{gent.WithRunId(store.view.RunId), gent.WithCodecs(codecs)}
	if stop != nil {
		options = append(options, gent.WithRecorder(store))
	}
	ctx := gent.NewExecutionContext(worker, "native resume",
		gent.NewBasicLoopData(providerToolJSONTask(agent.Output())), options...)
	ctx.SetLimits([]gent.Limit{{Type: gent.LimitExactKey, Key: gent.SCIterations, MaxValue: 4}})
	configureProviderResume(ctx, model, compact)
	if compact {
		texts := []string{"Read the record.", "Keep its exact text.", "Use the lookup tool."}
		for _, text := range texts {
			if !assert.NoError(t, ctx.AppendRecord(&gent.HistoryEntry{
				Messages: []*gent.MessageContent{{Role: gent.ChatMessageTypeHuman,
					Parts: []gent.ContentPart{gent.TextPart(text)}}}}, true)) {
				return ctx
			}
		}
	}
	runner := executor.NewRunner(agent, executor.DefaultConfig())
	value, result, err := runner.Execute(ctx)
	if stop != nil {
		if !assert.ErrorIs(t, err, context.Canceled) || !assert.NotEmpty(t, store.saved) {
			return ctx
		}
		assert.Equal(t, []providerLookupInput{{7}}, calls)
		// New adapters, agent, runner, registry, and decoded state must replace the old object graph.
		model = build()
		agent = providerToolJSONAgent(t, model, record, nil, &calls, &values)
		codecs = gent.NewCodecRegistry()
		if !assert.NoError(t, agent.RegisterCodecs(codecs)) {
			return ctx
		}
		var saved gent.RunView
		if !assert.NoError(t, codecs.Unmarshal(store.saved, &saved)) ||
			!assert.NoError(t, gent.ValidateBundle(saved, codecs)) {
			return ctx
		}
		assert.NotEqual(t, gent.PhaseTerminal, saved.Checkpoint.Phase)
		value, result, err = executor.NewRunner(agent, executor.DefaultConfig()).Resume(base, saved,
			gent.RestoreBindings{Codecs: codecs, Configure: func(restored *gent.ExecutionContext) error {
				ctx = restored
				configureProviderResume(ctx, model, compact)
				return nil
			}})
	}
	if !assert.NoError(t, err) {
		logCompactionFailure(t, ctx)
		return ctx
	}
	assert.Equal(t, providerLoopValue{record, 2}, value)
	assert.Equal(t, &gent.ExecutionResult{TerminationReason: gent.TerminationSuccess,
		Output: []gent.ContentPart{gent.TextPart(
			fmt.Sprintf(`{"record":%q,"round":2}`, record))}}, result)
	assert.Equal(t, []providerLookupInput{{7}}, calls, "completed tool work must not run again")
	assert.Equal(t, []providerLoopValue{{record, 1}, {record, 2}}, values)
	assert.Equal(t, int64(1), ctx.Stats().GetCounter(gent.SCToolCalls))
	assert.Equal(t, int64(1), ctx.Stats().GetCounter(gent.SCOutputCustomRejectedTotal))
	// Rebuild usage from accepted model evidence. Resume must not count a saved response twice.
	saved, err := ctx.Export()
	if !assert.NoError(t, err) {
		return ctx
	}
	var input, output int64
	for _, event := range saved.Activity {
		if event.Type != "model_call_finished" {
			continue
		}
		decoded, err := ctx.DecodeEvent(event)
		if !assert.NoError(t, err) {
			return ctx
		}
		after := decoded.(*gent.AfterModelCallEvent)
		if !assert.NoError(t, after.Error) || !assert.NotNil(t, after.Response) {
			return ctx
		}
		input += int64(after.Response.Info.InputTokens)
		output += int64(after.Response.Info.OutputTokens)
	}
	assert.Positive(t, input)
	assert.Positive(t, output)
	assert.Equal(t, input, ctx.Stats().GetCounter(gent.SCInputTokens))
	assert.Equal(t, output, ctx.Stats().GetCounter(gent.SCOutputTokens))
	assert.Equal(t, input+output, ctx.Stats().GetCounter(gent.SCTotalTokens))
	return ctx
}

func providerResumeResponse(t *testing.T, provider, text string, tool bool) string {
	t.Helper()
	response := providerResponse(provider, 0)
	if !tool {
		response = providerTextResponse(t, provider, text)
	}
	var body map[string]any
	if !assert.NoError(t, json.Unmarshal([]byte(response), &body)) {
		t.FailNow()
	}
	switch provider {
	case "openai", "xai":
		body["usage"] = map[string]any{"input_tokens": 12, "output_tokens": 8, "total_tokens": 20}
	case "anthropic":
		body["usage"] = map[string]any{"input_tokens": 12, "output_tokens": 8}
	case "gemini":
		body["usageMetadata"] = map[string]any{"promptTokenCount": 12, "candidatesTokenCount": 8,
			"totalTokenCount": 20}
	case "ollama":
		body["prompt_eval_count"], body["eval_count"] = 12, 8
	default:
		body["usage"] = map[string]any{"prompt_tokens": 12, "completion_tokens": 8, "total_tokens": 20}
	}
	data, err := json.Marshal(body)
	if !assert.NoError(t, err) {
		t.FailNow()
	}
	return string(data)
}

func TestProvidersUnfinishedResume(t *testing.T) {
	type input struct {
		provider, boundary string
		compact            bool
	}
	type expected struct {
		requests int
		tokens   int64
	}
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	var cases []testCase
	for _, provider := range testProviders {
		for _, boundary := range []string{
			"model response", "tool result", "rejected output", "compacted history",
		} {
			compact := boundary == "compacted history"
			want := expected{3, 60}
			if compact {
				want = expected{4, 80}
			}
			cases = append(cases, testCase{provider + "/" + boundary,
				input{provider, boundary, compact}, want})
		}
	}
	// The uninterrupted request sequence is the same for the three non-compaction boundaries.
	// Save it once per provider and mode; each interruption still uses a separate run and restore.
	baselines := make(map[string][]map[string]any)
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			key := fmt.Sprintf("%s/%t", test.input.provider, test.input.compact)
			for _, resumed := range []bool{false, true} {
				if !resumed && baselines[key] != nil {
					continue
				}
				var requests []map[string]any
				turn := 0
				build := func() gent.Model {
					client := &http.Client{Transport: responseTransport(func(request *http.Request) (
						*http.Response, error,
					) {
						if request.Method == http.MethodGet {
							return fixtureHTTP(routerFixture, "application/json"), nil
						}
						var body map[string]any
						if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
							return nil, err
						}
						requests = append(requests, body)
						if test.input.compact && len(requests) == 2 {
							return fixtureHTTP(providerResumeResponse(t, test.input.provider,
								"Earlier notes say to use the exact lookup result.", false),
								"application/json"), nil
						}
						turn++
						return fixtureHTTP(providerResumeResponse(t, test.input.provider,
							fmt.Sprintf(`{"record":"record-seven","round":%d}`, turn-1), turn == 1),
							"application/json"), nil
					})}
					return fixtureModel(test.input.provider, client)
				}
				var stop func(*gent.SaveBatch) bool
				if resumed {
					stop = providerResumeBoundary(test.input.boundary)
				}
				ctx := checkProviderResume(t, context.Background(), build, "record-seven",
					test.input.compact, stop)
				assert.Equal(t, test.expected, expected{len(requests),
					ctx.Stats().GetCounter(gent.SCTotalTokens)})
				if !resumed {
					baselines[key] = requests
					continue
				}
				// Compare every complete wire request, including saved signatures, tool IDs,
				// correction steering, synthetic summaries, output controls, and tool results.
				assert.Equal(t, baselines[key], requests)
				assertProviderResumeNativeHistory(t, test.input.provider, requests[len(requests)-1],
					test.input.compact)
			}
		})
	}
}

func TestLiveProviderUnfinishedResume(t *testing.T) {
	for _, provider := range liveProviders() {
		t.Run(provider.name, func(t *testing.T) {
			config := liveConfig(t, provider, false)
			t.Parallel()
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			build := func() gent.Model { return newLiveModel(provider, &config) }
			checkProviderResume(t, ctx, build, "record-"+rand.Text(), false,
				providerResumeBoundary("tool result"))
		})
	}
}
