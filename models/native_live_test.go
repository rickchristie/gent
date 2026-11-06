package models

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/rickchristie/gent"
	"github.com/rickchristie/gent/agents/react"
	"github.com/rickchristie/gent/executor"
	"github.com/rickchristie/gent/output"
	"github.com/rickchristie/gent/toolchain"
	"github.com/stretchr/testify/assert"
)

type liveIndexable[I, O any] struct{ gent.Tool[I] }

func (*liveIndexable[I, O]) Domain() string             { return "records" }
func (*liveIndexable[I, O]) Categories() []string       { return []string{"records"} }
func (*liveIndexable[I, O]) Keywords() []string         { return []string{"lookup", "read"} }
func (*liveIndexable[I, O]) SyntheticQueries() []string { return []string{"read a record"} }

type liveProgramCall struct {
	name   string
	input  any
	output any
	err    error
}

func liveProgramCalls(ctx *gent.ExecutionContext) ([][]liveProgramCall, error) {
	var programs [][]liveProgramCall
	var calls []liveProgramCall
	collecting := false
	for _, event := range ctx.Events() {
		if before, ok := event.(*gent.BeforeToolCallEvent); ok && before.ToolName == "run_code" {
			collecting = true
			calls = nil
		}
		after, ok := event.(*gent.AfterToolCallEvent)
		if !ok || !collecting {
			continue
		}
		record, ok := after.Output.(*gent.ToolCallRecord)
		if !ok {
			continue
		}
		if record.Name() == "run_code" {
			collecting = false
			if len(calls) > 0 {
				programs = append(programs, calls)
				calls = nil
			}
			continue
		}
		var input any
		if record.Input() != nil {
			var err error
			input, err = ctx.Codecs().Decode(*record.Input())
			if err != nil {
				return nil, err
			}
		}
		calls = append(calls, liveProgramCall{record.Name(), input,
			gent.ContentTextData(record.Response().Parts()), after.Error})
	}
	return programs, ctx.RecordingError()
}

func liveToolChain(calls *[]string, withSearch bool) (*toolchain.JsToolChainWrapper, error) {
	lookup := &liveIndexable[*responseInput, *responseOutput]{gent.DefineTool("lookup",
		func(_ context.Context, input *responseInput) (*gent.ToolExecutionResponse, error) {
			*calls = append(*calls, "lookup:"+input.ID)
			return capturedToolResponse(&responseOutput{NextID: "record-9"}, nil)
		}).Description("Find the next record ID").ParameterSchema(responseInputSchema()).
		OutputSchemaFrom(responseOutput{}).MustBuild()}
	read := &liveIndexable[*responseInput, string]{gent.DefineTool("read",
		func(_ context.Context, input *responseInput) (*gent.ToolExecutionResponse, error) {
			*calls = append(*calls, "read:"+input.ID)
			if input.ID != "record-9" {
				return capturedToolResponse("", fmt.Errorf("use the ID returned by lookup"))
			}
			return capturedToolResponse("record-finished", nil)
		}).Description("Read the record identified by lookup").ParameterSchema(responseInputSchema()).
		MustBuild()}
	if !withSearch {
		chain := toolchain.New().RegisterTool(lookup).RegisterTool(read)
		return toolchain.NewJsToolChainWrapper(chain).WithDirectCallDisabled(), nil
	}
	search := toolchain.NewSearch(toolchain.SearchHintSimpleList).
		RegisterEngine(toolchain.NewRegexToolSearcher())
	search.RegisterTool(lookup).RegisterTool(read)
	search.RegisterTool(&liveIndexable[*responseInput, string]{gent.DefineTool("ping",
		func(context.Context, *responseInput) (*gent.ToolExecutionResponse, error) {
			return capturedToolResponse("ready", nil)
		}).
		Description("Check service status").ParameterSchema(responseInputSchema()).MustBuild()})
	search.Pin("ping")
	if err := search.Initialize(); err != nil {
		return nil, err
	}
	return toolchain.NewJsToolChainWrapper(search), nil
}

// TestNativeLive requires real native calls. A text answer cannot satisfy the assertions.
// Use the same provider matrix as the other live tests, including endpoint and model overrides.
func TestNativeLive(t *testing.T) {
	type input struct {
		provider liveProvider
		search   bool
	}
	type expected struct {
		calls    []string
		programs [][]liveProgramCall
	}
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	want := expected{
		calls: []string{"lookup:start", "read:record-9"},
		programs: [][]liveProgramCall{{
			{"lookup", &responseInput{ID: "start"}, `{"next_id":"record-9"}`, nil},
			{"read", &responseInput{ID: "record-9"}, "record-finished", nil},
		}},
	}
	var cases []testCase
	for _, provider := range liveProviders() {
		cases = append(cases,
			testCase{provider.name + "/program", input{provider, false}, want},
			testCase{provider.name + "/search and program", input{provider, true}, want})
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			config := liveConfig(t, test.input.provider, false)
			t.Parallel()
			model := newLiveModel(test.input.provider, &config)
			var calls []string
			program, err := liveToolChain(&calls, test.input.search)
			if !assert.NoError(t, err) {
				return
			}
			task := `Use one run_code call to call lookup with id "start", then call read with
id from lookupReply.parts.find(part => part.kind === "data").data.next_id.
Check each reply's kind and executionStatus. Use tool.call for both calls.
Do not call lookup or read directly. The code is a script; do not use a top-level return.
Return only the text from read as the final answer, without quotes or formatting.`
			if test.input.search {
				task = `First use tool_registry_search with regex query "lookup|read".
Wait for that result before the next request. Then follow these instructions:
` + task
			}
			out := output.NewTextOutput().SetCustomValidator("live-program-result",
				func(_ *gent.ExecutionContext, value string) (*gent.OutputValidationResult, error) {
					if value != "record-finished" {
						return &gent.OutputValidationResult{Steering: []gent.ContentPart{gent.TextPart(
							`Return only the exact text from the saved read result, with no extra whitespace.
The program and its tool calls are complete. Do not run them again.`)}}, nil
					}
					return &gent.OutputValidationResult{Accepted: true}, nil
				})
			agent := react.NewWithOutput(model, out).WithToolChain(program).
				WithRecoveryVersion("live-native/v1").WithCallOptions(gent.WithMaxTokens(3000))
			worker, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()
			ctx := gent.NewExecutionContext(worker, "live native",
				gent.NewBasicLoopData(gent.NewTask(gent.TextContent{Text: task})))
			t.Cleanup(func() {
				if !t.Failed() {
					return
				}
				// Show only fixture calls. Provider continuation items and credentials stay private.
				for _, event := range ctx.Events() {
					after, ok := event.(*gent.AfterModelCallEvent)
					if !ok || after.Response == nil {
						continue
					}
					for _, choice := range after.Response.Choices {
						for _, call := range choice.ToolCalls {
							t.Logf("native call: %s %s", call.FunctionCall.Name, call.FunctionCall.Arguments)
						}
					}
				}
			})
			ctx.SetLimits([]gent.Limit{{Type: gent.LimitExactKey, Key: gent.SCIterations, MaxValue: 6}})
			value, result, err := executor.NewRunner(agent, executor.DefaultConfig()).Execute(ctx)
			if !assert.NoError(t, err) {
				return
			}
			assert.Equal(t, gent.TerminationSuccess, result.TerminationReason)
			assert.Equal(t, "record-finished", value)
			assert.Equal(t, "record-finished", gent.ContentText(result.Output))
			programs, err := liveProgramCalls(ctx)
			if !assert.NoError(t, err) {
				return
			}
			assert.Equal(t, test.expected, expected{calls, programs})
			if test.input.search {
				var catalogs [][]string
				for _, event := range ctx.Events() {
					if before, ok := event.(*gent.BeforeModelCallEvent); ok {
						var names []string
						for _, tool := range before.Request.Options.Tools {
							names = append(names, tool.Function.Name)
						}
						catalogs = append(catalogs, names)
					}
				}
				if assert.GreaterOrEqual(t, len(catalogs), 3) {
					// Shared search returns definitions in its result. Native exposure stays fixed.
					for _, catalog := range catalogs {
						assert.Equal(t, []string{"tool_registry_search", "execute_tool", "ping", "run_code"},
							catalog)
					}
				}
			}
		})
	}
}
