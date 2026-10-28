package toolchain

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rickchristie/gent"
	"github.com/rickchristie/gent/events"
	"github.com/rickchristie/gent/format"
	"github.com/stretchr/testify/assert"
)

func TestNativeJavaScript_ParallelOverlap(t *testing.T) {
	worker, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ctx := gent.NewExecutionContext(worker, "parallel", gent.NewBasicLoopData(nil))
	defer ctx.Release()
	var active, peak, started atomic.Int32
	ready := []chan struct{}{make(chan struct{}), make(chan struct{})}
	chain := New().RegisterTool(gent.DefineTool("lookup",
		func(ctx context.Context, args map[string]any) (string, error) {
			count := active.Add(1)
			defer active.Add(-1)
			for previous := peak.Load(); count > previous; previous = peak.Load() {
				if peak.CompareAndSwap(previous, count) {
					break
				}
			}
			position := started.Add(1)
			round := ready[(position-1)/2]
			if position%2 == 0 {
				close(round)
			}
			select {
			case <-round:
				return args["id"].(string), nil
			case <-ctx.Done():
				return "", ctx.Err()
			}
		}).Description("Read data").ParameterSchema(nil).MustBuild())
	wrapper := NewJsToolChainWrapper(chain)
	result, err := executeTestCode(wrapper, ctx, `for (let i = 0; i < 2; i++) {
const results = tool.parallelCall([
  {tool: "lookup", args: {id: "first"}},
  {tool: "lookup", args: {id: "second"}}
]);
console.log(results.map(result => result.output).join(","));
}`, format.NewXML())
	assert.Equal(t, int32(2), peak.Load())
	assert.Equal(t, int32(4), started.Load())
	assert.Equal(t, int32(0), active.Load())
	if !assert.NoError(t, err) {
		return
	}
	assert.Equal(t, `<tool_call_log>
[1a] lookup({"id":"first"}) -> "first"
[1b] lookup({"id":"second"}) -> "second"
[2a] lookup({"id":"first"}) -> "first"
[2b] lookup({"id":"second"}) -> "second"
</tool_call_log>
<output>
first,second
first,second
</output>`, collectText(result))
}

type parallelInputHook struct {
	secondDone chan struct{}
}

func (hook *parallelInputHook) OnBeforeToolCall(_ *gent.ExecutionContext,
	event *gent.BeforeToolCallEvent,
) {
	args := event.Args.(map[string]any)
	args["id"] = "checked-" + args["id"].(string)
}

func (hook *parallelInputHook) OnAfterToolCall(_ *gent.ExecutionContext,
	event *gent.AfterToolCallEvent,
) {
	if event.Args.(map[string]any)["id"] == "checked-second" {
		close(hook.secondDone)
	}
}

func TestNativeJavaScript_ParallelResults(t *testing.T) {
	type input struct {
		search bool
		fail   bool
	}
	type expected struct {
		errors int64
		text   string
		err    error
	}
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	success := `<tool_call_log>
[1a] lookup({"id":"first"}) -> "checked-first"
[1b] lookup({"id":"second"}) -> "checked-second"
</tool_call_log>
<output>
checked-first,checked-second
</output>`
	failure := `<tool_call_log>
[1a] lookup({"id":"first"}) -> error: lookup failed
[1b] lookup({"id":"second"}) -> "checked-second"
</tool_call_log>
<output>
lookup failed,checked-second
</output>`
	cases := []testCase{
		{"registry", input{}, expected{0, success, nil}},
		{"registry with tool error", input{fail: true}, expected{1, failure, nil}},
		{"search chain", input{search: true}, expected{0, success, nil}},
		{"search chain with tool error", input{search: true, fail: true},
			expected{1, failure, nil}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			worker, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			ctx := gent.NewExecutionContext(worker, "parallel", gent.NewBasicLoopData(nil))
			defer ctx.Release()
			hook := &parallelInputHook{secondDone: make(chan struct{})}
			ctx.SetEventPublisher(events.NewRegistry().Subscribe(hook))
			var lock sync.Mutex
			identities := make(map[string]gent.ToolIdentity)
			toolError := errors.New("lookup failed")
			tool := newIndexableTool("lookup", "Read data", "Data", nil, nil,
				func(callCtx context.Context, args map[string]any) (string, error) {
					id := args["id"].(string)
					identity, ok := gent.ToolIdentityFromContext(callCtx)
					assert.True(t, ok)
					lock.Lock()
					identities[id] = identity
					lock.Unlock()
					if id == "checked-second" {
						return id, nil
					}
					select {
					case <-hook.secondDone:
					case <-callCtx.Done():
						return "", callCtx.Err()
					}
					if test.input.fail {
						return "", toolError
					}
					return id, nil
				})
			var chain gent.ToolChain = New().RegisterTool(tool)
			if test.input.search {
				chain = setupSearchChain([]indexableToolFunc{tool},
					[]gent.ToolSearcher{NewRegexToolSearcher()})
			}
			wrapper := NewJsToolChainWrapper(chain)
			result, err := executeTestCode(wrapper, ctx, `const results = tool.parallelCall([
  {tool: "lookup", args: {id: "first"}},
  {tool: "lookup", args: {id: "second"}}
]);
console.log(results.map(result => result.error || result.output).join(","));`, format.NewXML())
			if !assert.Equal(t, test.expected.err, err) {
				return
			}
			assert.Equal(t, test.expected.text, collectText(result))
			assert.Equal(t, int64(2), ctx.Stats().GetCounter(gent.SCToolCalls))
			assert.Equal(t, int64(2), ctx.Stats().GetCounter(gent.SCToolCallsFor+"lookup"))
			assert.Equal(t, test.expected.errors, ctx.Stats().GetCounter(gent.SCToolCallsErrorTotal))
			assert.Equal(t, float64(test.expected.errors),
				ctx.Stats().GetGauge(gent.SGToolCallsErrorConsecutive))
			wantIdentities := make(map[string]gent.ToolIdentity)
			var wantResults []*gent.ToolCallResult
			for i, id := range []string{"first", "second"} {
				callId := fmt.Sprintf("%s:tool:%d", ctx.ContextId(), i+2)
				args := map[string]any{"id": id}
				want := &gent.ToolCallResult{Name: "lookup", Args: args,
					Input: map[string]any{"id": "checked-" + id}, Output: "checked-" + id,
					Hash: ToolCallResultHash("lookup", args), SummaryCaptured: true,
					ToolCallId: callId, AttemptId: ctx.AttemptId()}
				if i == 0 && test.input.fail {
					want.Output, want.Error = nil, toolError
				}
				wantResults = append(wantResults, want)
				wantIdentities["checked-"+id] = gent.ToolIdentity{RunId: ctx.RunId(),
					ContextId: ctx.ContextId(), ToolCallId: callId, AttemptId: ctx.AttemptId()}
			}
			wantResults = append(wantResults, &gent.ToolCallResult{Text: test.expected.text})
			assert.Equal(t, wantResults, result.Results)
			assert.Equal(t, wantIdentities, identities)
			assertProgramStateCleared(t, ctx)
		})
	}
}

func assertProgramStateCleared(t *testing.T, ctx *gent.ExecutionContext) {
	t.Helper()
	view, err := ctx.Export()
	if !assert.NoError(t, err) {
		return
	}
	assert.NoError(t, gent.ValidateBundle(view, ctx.Codecs()))
	var keys []string
	for key := range view.Checkpoint.Contexts[0].State {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	assert.Equal(t, []string{"gent:search_seen", "gent:tool_plan"}, keys)
}

func TestNativeJavaScript_ParallelCancellation(t *testing.T) {
	worker, cancel := context.WithCancel(context.Background())
	defer cancel()
	ctx := gent.NewExecutionContext(worker, "parallel", gent.NewBasicLoopData(nil))
	defer ctx.Release()
	started := make(chan struct{}, 2)
	var active atomic.Int32
	chain := New().RegisterTool(gent.DefineTool("lookup",
		func(ctx context.Context, _ map[string]any) (string, error) {
			active.Add(1)
			defer active.Add(-1)
			started <- struct{}{}
			<-ctx.Done()
			return "", ctx.Err()
		}).Description("Read data").ParameterSchema(nil).MustBuild())
	done := make(chan error, 1)
	go func() {
		_, err := executeTestCode(NewJsToolChainWrapper(chain), ctx, `tool.parallelCall([
  {tool: "lookup", args: {}}, {tool: "lookup", args: {}}
]);`, format.NewXML())
		done <- err
	}()
	for range 2 {
		select {
		case <-started:
		case <-time.After(5 * time.Second):
			t.Error("both tool calls must start before cancellation")
			cancel()
		}
	}
	cancel()
	select {
	case err := <-done:
		assert.ErrorIs(t, err, context.Canceled)
		assert.Equal(t, int32(0), active.Load())
	case <-time.After(5 * time.Second):
		t.Error("parallel calls did not stop after cancellation")
	}
}

type parallelSaveFailure struct {
	view        gent.RunView
	codecs      *gent.CodecRegistry
	afterSave   bool
	beforeCalls bool
	err         error
}

func (store *parallelSaveFailure) Save(_ context.Context, batch *gent.SaveBatch) error {
	fail := false
	if store.beforeCalls && batch.Checkpoint != nil {
		_, fail = batch.Checkpoint.Contexts[0].State["gent:program_calls"]
	}
	for _, event := range batch.Events {
		if event.Type == "tool_call_finished" {
			fail = true
		}
	}
	if fail && !store.afterSave {
		return store.err
	}
	next, err := gent.ApplyBatch(store.view, batch, store.codecs)
	if err != nil {
		return err
	}
	store.view = next
	if fail {
		return store.err
	}
	return nil
}

func TestNativeJavaScript_ParallelRecovery(t *testing.T) {
	type input struct {
		afterSave   bool
		beforeCalls bool
	}
	type expected struct {
		started  int
		outcomes []string
		err      error
	}
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	saveError := errors.New("parallel save failed")
	cases := []testCase{
		{"before tool calls", input{true, true},
			expected{0, []string{"not_started", "not_started"}, saveError}},
		{"lost result", input{}, expected{2, []string{"unknown", "unknown"}, saveError}},
		{"lost acknowledgement", input{afterSave: true},
			expected{2, []string{"", "unknown"}, saveError}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			worker, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			var started, active atomic.Int32
			ready := make(chan struct{})
			build := func() *JsToolChainWrapper {
				chain := New().RegisterTool(gent.DefineTool("lookup",
					func(ctx context.Context, args map[string]any) (string, error) {
						active.Add(1)
						defer active.Add(-1)
						if started.Add(1) == 2 {
							close(ready)
						}
						if args["id"] == "second" {
							<-ctx.Done()
							return "", ctx.Err()
						}
						select {
						case <-ready:
							return "first", nil
						case <-ctx.Done():
							return "", ctx.Err()
						}
					}).Description("Read data").ParameterSchema(nil).MustBuild())
				return NewJsToolChainWrapper(chain)
			}
			store := &parallelSaveFailure{codecs: gent.NewCodecRegistry(),
				afterSave: test.input.afterSave, beforeCalls: test.input.beforeCalls, err: saveError}
			ctx := gent.NewExecutionContext(worker, "parallel", gent.NewBasicLoopData(nil),
				gent.WithRecorder(store), gent.WithCodecs(store.codecs))
			defer ctx.Release()
			store.view = gent.RunView{SchemaVersion: gent.RunSchemaVersion, RunId: ctx.RunId()}
			const script = `tool.parallelCall([
  {tool: "lookup", args: {id: "first"}},
  {tool: "lookup", args: {id: "second"}}
]);
tool.call({tool: "lookup", args: {id: "later"}});`
			_, err := executeTestCode(build(), ctx, script, format.NewXML())
			assert.ErrorIs(t, err, test.expected.err)
			assert.Equal(t, int32(test.expected.started), started.Load())
			assert.Equal(t, int32(0), active.Load())
			freshChain := build()
			codecs := gent.NewCodecRegistry()
			assert.NoError(t, freshChain.RegisterCodecs(codecs))
			data, err := store.codecs.Marshal(store.view)
			if !assert.NoError(t, err) {
				return
			}
			var saved gent.RunView
			if !assert.NoError(t, codecs.Unmarshal(data, &saved)) {
				return
			}
			fresh, err := gent.RestoreExecutionContext(context.Background(), saved,
				gent.RestoreBindings{Codecs: codecs})
			if !assert.NoError(t, err) {
				return
			}
			defer fresh.Release()
			plan, found, err := fresh.SavedToolPlan()
			if !assert.NoError(t, err) || !assert.True(t, found) {
				return
			}
			result, err := freshChain.ExecuteNext(fresh, plan, format.NewXML())
			if !assert.NoError(t, err) {
				return
			}
			assert.ErrorIs(t, result.Error, gent.ErrUncertainTool)
			assert.Equal(t, "unknown", result.Outcome)
			assert.Equal(t, "", result.Instructions)
			partial := result.Output.(*gent.ToolChainResult)
			var outcomes, ids []string
			var wantCalls []*gent.ToolCallResult
			for i, call := range partial.Results {
				outcomes = append(outcomes, call.Outcome)
				ids = append(ids, call.ToolCallId)
				id := []string{"first", "second"}[i]
				args := map[string]any{"id": id}
				want := &gent.ToolCallResult{Name: "lookup", Args: args, SummaryCaptured: true,
					ToolCallId: fmt.Sprintf("%s:tool:%d", ctx.ContextId(), i+2),
					Outcome:    test.expected.outcomes[i]}
				switch want.Outcome {
				case "not_started":
					want.Text = "This call did not start. " +
						"Review the interrupted operation before making a new plan."
				case "unknown":
					want.Input, want.AttemptId = args, ctx.AttemptId()
					want.Error = gent.ErrUncertainTool
					want.Text = fmt.Sprintf(`Tool lookup (call %s):
%s`, want.ToolCallId, `The operation was interrupted. Its outcome is unknown.
It may have completed. Check the current state before you decide whether to repeat it.`)
				default:
					want.Input, want.AttemptId, want.Output = args, ctx.AttemptId(), "first"
					want.Hash = ToolCallResultHash("lookup", args)
					want.Text = `<lookup>
first
</lookup>`
				}
				wantCalls = append(wantCalls, want)
			}
			assert.Equal(t, test.expected.outcomes, outcomes)
			assert.Equal(t, []string{ctx.ContextId() + ":tool:2", ctx.ContextId() + ":tool:3"}, ids)
			assert.Equal(t, wantCalls, partial.Results)
			texts := []string{fmt.Sprintf(
				`JavaScript execution was interrupted. The script will not be restarted.
Script:
%s`, script)}
			for _, call := range wantCalls {
				texts = append(texts, call.Text)
			}
			texts = append(texts, "Review these results and make a new plan.")
			assert.Equal(t, &gent.ToolCallResult{Name: "run_code", Args: plan.Calls[0].Args,
				ToolCallId: plan.Calls[0].Id, AttemptId: ctx.AttemptId(), Outcome: "unknown",
				Output: &gent.ToolChainResult{Results: wantCalls}, Error: gent.ErrUncertainTool,
				Text: strings.Join(texts, "\n\n"),

				SummaryCaptured: true}, result)
			assert.Equal(t, int64(test.expected.started), fresh.Stats().GetCounter(gent.SCToolCalls))
			assert.Equal(t, int32(test.expected.started), started.Load())
			assertProgramStateCleared(t, fresh)
		})
	}
}

func TestNativeJavaScript_ParallelSearch(t *testing.T) {
	worker, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ctx := gent.NewExecutionContext(worker, "parallel search", gent.NewBasicLoopData(nil))
	defer ctx.Release()
	var started atomic.Int32
	ready := make(chan struct{})
	tool := newIndexableTool("lookup", "Read data", "Data", nil, nil,
		func(context.Context, map[string]any) (string, error) { return "record", nil })
	chain := setupSearchChain([]indexableToolFunc{tool}, []gent.ToolSearcher{
		&mockToolSearcher{id: "test",
			searchFn: func(ctx context.Context, _ string) ([]string, error) {
				if started.Add(1) == 2 {
					close(ready)
				}
				select {
				case <-ready:
					return []string{"lookup"}, nil
				case <-ctx.Done():
					return nil, ctx.Err()
				}
			}},
	})
	result, err := executeTestCode(NewJsToolChainWrapper(chain), ctx, `tool.parallelCall([
  {tool: "tool_registry_search", args: {query: "first", query_type: "test"}},
  {tool: "tool_registry_search", args: {query: "second", query_type: "test"}}
]);`, format.NewXML())
	if !assert.NoError(t, err) {
		return
	}
	assert.Equal(t, int32(2), started.Load())
	output := `- lookup: Read data
  Returns: {
    "type": "string"
  }

Showing page 1 of 1 (1 total results)`
	var wantCalls []*gent.ToolCallResult
	for i, query := range []string{"first", "second"} {
		args := map[string]any{"query": query, "query_type": "test"}
		wantCalls = append(wantCalls, &gent.ToolCallResult{Name: searchToolName,
			Args: args, Input: args, Output: output, Hash: ToolCallResultHash(searchToolName, args),
			ToolCallId: fmt.Sprintf("%s:tool:%d", ctx.ContextId(), i+2),
			AttemptId:  ctx.AttemptId(), DiscoveredTools: []string{"lookup"}, SummaryCaptured: true})
	}
	assert.Equal(t, wantCalls, result.Results[:2])
	assertProgramStateCleared(t, ctx)
}

// Embedding the base interface keeps this chain's execution contract sequential.
type sequentialProgramChain struct{ gent.ToolChain }

type disabledParallelProgramChain struct{ gent.ToolChain }

func (disabledParallelProgramChain) SupportsParallelCalls() bool { return false }

func TestNativeJavaScript_ParallelRequiresSupport(t *testing.T) {
	type input struct {
		wrap func(gent.ToolChain) gent.ToolChain
	}
	type expected struct {
		calls     int32
		errorText string
	}
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	want := expected{0, "execute tool 0: tool.parallelCall requires a ParallelToolChain"}
	cases := []testCase{
		{"support absent", input{func(chain gent.ToolChain) gent.ToolChain {
			return sequentialProgramChain{chain}
		}}, want},
		{"support disabled", input{func(chain gent.ToolChain) gent.ToolChain {
			return disabledParallelProgramChain{chain}
		}}, want},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			var calls atomic.Int32
			chain := New().RegisterTool(gent.DefineTool("lookup",
				func(context.Context, map[string]any) (string, error) {
					calls.Add(1)
					return "record", nil
				}).Description("Read data").ParameterSchema(nil).MustBuild())
			ctx := newExecCtx()
			defer ctx.Release()
			result, err := executeTestCode(NewJsToolChainWrapper(test.input.wrap(chain)), ctx,
				`tool.parallelCall([{tool: "lookup", args: {}}, {tool: "lookup", args: {}}]);`,
				format.NewXML())
			assert.Nil(t, result)
			assert.EqualError(t, err, test.expected.errorText)
			assert.Equal(t, test.expected.calls, calls.Load())
		})
	}
}

type checkedProgramRegistry struct {
	*Registry
	checks atomic.Int32
	err    error
}

func (chain *checkedProgramRegistry) ExecuteNext(_ *gent.ExecutionContext, _ *gent.ToolPlan,
	_ gent.TextFormat,
) (*gent.ToolCallResult, error) {
	chain.checks.Add(1)
	return nil, chain.err
}

type checkedProgramSearch struct {
	*SearchChain
	checks atomic.Int32
	err    error
}

func (chain *checkedProgramSearch) ExecuteNext(_ *gent.ExecutionContext, _ *gent.ToolPlan,
	_ gent.TextFormat,
) (*gent.ToolCallResult, error) {
	chain.checks.Add(1)
	return nil, chain.err
}

func TestNativeJavaScript_ParallelKeepsExecutionChecks(t *testing.T) {
	type input struct {
		search bool
		code   string
	}
	type expected struct {
		calls, checks int32
		err           error
		errorText     string
	}
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	blocked := errors.New("tool call requires approval")
	single := `tool.call({tool: "write", args: {}});`
	parallel := `tool.parallelCall([
  {tool: "write", args: {}}, {tool: "write", args: {}}
]);`
	wantSingle := expected{0, 1, blocked, "execute tool 0: tool call requires approval"}
	wantParallel := expected{0, 2, blocked, `execute tool 0: tool call requires approval
tool call requires approval`}
	cases := []testCase{
		{"registry single", input{false, single}, wantSingle},
		{"registry parallel", input{false, parallel}, wantParallel},
		{"search single", input{true, single}, wantSingle},
		{"search parallel", input{true, parallel}, wantParallel},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			var calls atomic.Int32
			tool := newIndexableTool("write", "Write data", "Data", nil, nil,
				func(context.Context, map[string]any) (string, error) {
					calls.Add(1)
					return "written", nil
				})
			registry := &checkedProgramRegistry{Registry: New(), err: blocked}
			var chain gent.ToolChain = registry
			checks := &registry.checks
			if test.input.search {
				search := &checkedProgramSearch{
					SearchChain: setupSearchChain(nil, []gent.ToolSearcher{NewRegexToolSearcher()}),
					err:         blocked,
				}
				chain, checks = search, &search.checks
			}
			chain.RegisterTool(tool)
			ctx := newExecCtx()
			defer ctx.Release()
			result, err := executeTestCode(NewJsToolChainWrapper(chain), ctx, test.input.code,
				format.NewXML())
			assert.Nil(t, result)
			assert.ErrorIs(t, err, test.expected.err)
			assert.EqualError(t, err, test.expected.errorText)
			assert.Equal(t, test.expected.calls, calls.Load())
			assert.Equal(t, test.expected.checks, checks.Load())
		})
	}
}

func TestNativeJavaScript_ParallelPanicStopsCalls(t *testing.T) {
	type input struct {
		search, parallel, cleanupPanic bool
	}
	type expected struct {
		panicValue  any
		err         error
		interrupted bool
		calls       int32
		active      int32
	}
	type completion struct {
		panicValue any
		err        error
	}
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	wantSingle := expected{panicValue: "tool failed", calls: 1}
	wantParallel := expected{panicValue: "tool failed", interrupted: true, calls: 2}
	cases := []testCase{
		{"registry single", input{}, wantSingle},
		{"registry parallel", input{parallel: true}, wantParallel},
		{"search parallel", input{search: true, parallel: true}, wantParallel},
		{"first panic survives cleanup panic", input{parallel: true, cleanupPanic: true}, wantParallel},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			worker, cancel := context.WithCancel(context.Background())
			defer cancel()
			ctx := gent.NewExecutionContext(worker, "parallel panic", gent.NewBasicLoopData(nil))
			defer ctx.Release()
			var calls, active atomic.Int32
			started := make(chan struct{})
			tools := []indexableToolFunc{
				newIndexableTool("fail", "Fail", "Data", nil, nil,
					func(callCtx context.Context, _ map[string]any) (string, error) {
						calls.Add(1)
						active.Add(1)
						defer active.Add(-1)
						if test.input.parallel {
							select {
							case <-started:
							case <-callCtx.Done():
								return "", callCtx.Err()
							}
						}
						panic("tool failed")
					}),
				newIndexableTool("wait", "Wait", "Data", nil, nil,
					func(callCtx context.Context, _ map[string]any) (string, error) {
						calls.Add(1)
						active.Add(1)
						defer active.Add(-1)
						close(started)
						<-callCtx.Done()
						if test.input.cleanupPanic {
							panic("cleanup failed")
						}
						return "", callCtx.Err()
					}),
			}
			build := func() *JsToolChainWrapper {
				var chain gent.ToolChain = New()
				if test.input.search {
					chain = setupSearchChain(tools, []gent.ToolSearcher{NewRegexToolSearcher()})
				} else {
					for _, tool := range tools {
						chain.RegisterTool(tool)
					}
				}
				return NewJsToolChainWrapper(chain)
			}
			code := `tool.call({tool: "fail", args: {}});`
			if test.input.parallel {
				// The cleanup panic is in the first slot. The first panic must still win.
				code = `tool.parallelCall([{tool: "wait", args: {}}, {tool: "fail", args: {}}]);`
			}
			done := make(chan completion, 1)
			go func() {
				var err error
				defer func() { done <- completion{recover(), err} }()
				_, err = executeTestCode(build(), ctx, code, format.NewXML())
			}()
			var result completion
			select {
			case result = <-done:
			case <-time.After(5 * time.Second):
				t.Error("panic did not stop the other calls")
				cancel()
				select {
				case result = <-done:
				case <-time.After(5 * time.Second):
					t.Fatal("calls did not stop after test cleanup")
				}
			}
			assert.Equal(t, test.expected, expected{result.panicValue, result.err,
				ctx.IsWorkerInterrupted(), calls.Load(), active.Load()})
			assert.NoError(t, ctx.RecordingError())
			if !test.input.parallel {
				return
			}
			assert.EqualError(t, context.Cause(ctx.Context()), "gent: parallel tool call panicked")
			view, err := ctx.Export()
			if !assert.NoError(t, err) {
				return
			}
			assert.NoError(t, gent.ValidateBundle(view, ctx.Codecs()))
			data, err := ctx.Codecs().Marshal(view)
			if !assert.NoError(t, err) {
				return
			}
			freshChain := build()
			codecs := gent.NewCodecRegistry()
			assert.NoError(t, freshChain.RegisterCodecs(codecs))
			var saved gent.RunView
			if !assert.NoError(t, codecs.Unmarshal(data, &saved)) {
				return
			}
			fresh, err := gent.RestoreExecutionContext(context.Background(), saved,
				gent.RestoreBindings{Codecs: codecs})
			if !assert.NoError(t, err) {
				return
			}
			defer fresh.Release()
			plan, found, err := fresh.SavedToolPlan()
			if !assert.NoError(t, err) || !assert.True(t, found) {
				return
			}
			recovered, err := freshChain.ExecuteNext(fresh, plan, format.NewXML())
			if !assert.NoError(t, err) {
				return
			}
			assert.ErrorIs(t, recovered.Error, gent.ErrUncertainTool)
			partial := recovered.Output.(*gent.ToolChainResult)
			var outcomes []string
			for _, call := range partial.Results {
				outcomes = append(outcomes, call.Outcome)
			}
			assert.Equal(t, []string{"unknown", "unknown"}, outcomes)
			assert.Equal(t, test.expected.calls, calls.Load())
			assertProgramStateCleared(t, fresh)
		})
	}
}
