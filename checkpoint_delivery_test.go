package gent_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rickchristie/gent"
	"github.com/rickchristie/gent/executor"
	genttest "github.com/rickchristie/gent/internal/tt"
	"github.com/stretchr/testify/assert"
)

type checkpointStore struct {
	mu      sync.Mutex
	view    gent.RunView
	batches []*gent.SaveBatch
	before  func(*gent.SaveBatch) error
	after   func(*gent.SaveBatch) error
	codecs  *gent.CodecRegistry
}

func newCheckpointStore(id string) *checkpointStore {
	return &checkpointStore{view: gent.RunView{SchemaVersion: gent.RunSchemaVersion, RunId: id},
		codecs: gent.NewCodecRegistry()}
}

func (s *checkpointStore) Save(_ context.Context, batch *gent.SaveBatch) error {
	if s.before != nil {
		if err := s.before(batch); err != nil {
			return err
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	next, err := gent.ApplyBatch(s.view, batch, s.codecs)
	if err != nil {
		return err
	}
	s.view = next
	s.batches = append(s.batches, batch)
	if s.after != nil {
		return s.after(batch)
	}
	return nil
}

func (s *checkpointStore) kinds() [][]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var result [][]string
	for _, batch := range s.batches {
		var kinds []string
		for _, event := range batch.Events {
			kinds = append(kinds, event.Type)
		}
		result = append(result, kinds)
	}
	return result
}

type checkpointModel func(context.Context, gent.ModelCallRequest) (gent.Stream, error)

func (m checkpointModel) GenerateContentStream(
	ctx context.Context, request gent.ModelCallRequest,
) (gent.Stream, error) {
	return m(ctx, request)
}

func checkpointReceive[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case value, ok := <-ch:
		if !ok {
			t.Fatal("channel closed before expected value")
		}
		return value
	case <-time.After(10 * time.Second):
		t.Fatal("operation did not complete within ten seconds")
		var zero T
		return zero
	}
}

func checkpointChunk(text string) gent.StreamChunk {
	return gent.StreamChunk{MetaContent: []*gent.MetaContent{{
		Type: gent.MetaContentIntermediary, Content: []gent.ContentPart{gent.TextContent{Text: text}}}}}
}

func TestCheckpointModelChunksGrouped(t *testing.T) {
	type input struct{ limit int }
	type expected struct {
		savedDuring uint64
		batches     [][]string
		err         error
	}
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	chunk := "model_stream_chunk"
	cases := []testCase{
		{"completion", input{16}, expected{2, [][]string{{"checkpoint"},
			{"model_call_started"}, {chunk, chunk, chunk, chunk, chunk, "model_call_finished"}}, nil}},
		{"early progress", input{2}, expected{6, [][]string{{"checkpoint"},
			{"model_call_started"}, {chunk, chunk}, {chunk, chunk},
			{chunk, "model_call_finished"}}, nil}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			store := newCheckpointStore("grouped")
			ctx := gent.NewExecutionContext(context.Background(), "test", nil,
				gent.WithRunId("grouped"), gent.WithRecorder(store),
				gent.WithCheckpointPolicy(gent.CheckpointPolicy{MaxEvents: test.input.limit}))
			defer ctx.Release()
			assert.NoError(t, ctx.CommitCheckpoint(gent.PhaseInitial))
			live, err := ctx.SubscribeAllWithConfig(gent.DeliveryConfig{})
			if !assert.NoError(t, err) {
				return
			}
			defer live.Close()
			finish := make(chan struct{})
			defer func() {
				select {
				case <-finish:
				default:
					close(finish)
				}
			}()
			response := &gent.ContentResponse{Info: &gent.GenerationInfo{
				InputTokens: 2, OutputTokens: 3, TotalTokens: 5}}
			model := checkpointModel(func(worker context.Context, _ gent.ModelCallRequest) (
				gent.Stream, error,
			) {
				stream, err := gent.NewBoundedStreamWithDuration(worker, 1, nil)
				if err != nil {
					return nil, err
				}
				go func() {
					for _, text := range []string{"one", "two", "three", "four", "five"} {
						if !stream.Send(checkpointChunk(text)) {
							break
						}
					}
					select {
					case <-finish:
					case <-worker.Done():
					}
					stream.Complete(response, nil)
				}()
				return stream, nil
			})
			stream, err := ctx.GenerateContentStream(model, gent.ModelCall{}, gent.NewModelCallRequest(nil))
			if !assert.NoError(t, err) {
				return
			}
			defer stream.Close()
			var text []string
			for range 5 {
				value := checkpointReceive(t, live.Chunks())
				text = append(text, gent.MetaText(value.MetaContent, gent.MetaContentIntermediary))
				value.MetaContent[0].Content[0] = gent.TextContent{Text: "changed by consumer"}
			}
			assert.Equal(t, []string{"one", "two", "three", "four", "five"}, text)
			during, err := ctx.Export()
			assert.NoError(t, err)
			assert.Equal(t, test.expected.savedDuring, during.LastAppliedEventNumber)
			assert.Equal(t, uint64(1), during.Checkpoint.EventNumber)
			close(finish)
			result, err := stream.Response()
			assert.Equal(t, test.expected.err, err)
			assert.Equal(t, response, result)
			assert.Equal(t, test.expected.batches, store.kinds())
			view, err := ctx.Export()
			assert.NoError(t, err)
			assert.NoError(t, gent.ValidateBundle(view))
			assert.Equal(t, uint64(8), view.LastAppliedEventNumber)
			assert.Equal(t, uint64(1), view.Checkpoint.EventNumber)
			var savedText []string
			for _, event := range view.Activity {
				if event.Type != chunk {
					continue
				}
				value, err := ctx.DecodeEvent(event)
				assert.NoError(t, err)
				savedText = append(savedText,
					gent.MetaText(value.(gent.StreamChunk).MetaContent, gent.MetaContentIntermediary))
			}
			assert.Equal(t, text, savedText)
			ctx.CloseStreams()
			var repeated []gent.StreamChunk
			for value := range live.Chunks() {
				repeated = append(repeated, value)
			}
			assert.Equal(t, []gent.StreamChunk(nil), repeated)
			assert.NoError(t, live.Err())
		})
	}
}

func TestCheckpointBlockedProgressKeepsLaterEvents(t *testing.T) {
	store := newCheckpointStore("blocked")
	started := make(chan *gent.SaveBatch, 1)
	release := make(chan struct{})
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	var saves atomic.Int32
	store.before = func(batch *gent.SaveBatch) error {
		if batch.Checkpoint == nil && saves.Add(1) == 1 {
			started <- batch
			<-release
		}
		return nil
	}
	ctx := gent.NewExecutionContext(context.Background(), "test", nil,
		gent.WithRunId("blocked"), gent.WithRecorder(store),
		gent.WithCheckpointPolicy(gent.CheckpointPolicy{MaxEvents: 2}))
	defer ctx.Release()
	assert.NoError(t, ctx.CommitCheckpoint(gent.PhaseInitial))
	live, err := ctx.SubscribeEvents(gent.DeliveryConfig{})
	assert.NoError(t, err)
	defer live.Close()
	assert.NoError(t, ctx.EmitChunk(checkpointChunk("one")))
	first := make(chan error, 1)
	go func() { first <- ctx.EmitChunk(checkpointChunk("two")) }()
	batch := checkpointReceive(t, started)
	assert.Equal(t, uint64(1), batch.PreviousEventNumber)
	assert.Nil(t, batch.Checkpoint)
	assert.Equal(t, uint64(2), checkpointReceive(t, live.Events()).EventNumber)
	assert.Equal(t, uint64(3), checkpointReceive(t, live.Events()).EventNumber)
	later := make(chan error, 1)
	go func() { later <- ctx.EmitChunk(checkpointChunk("three")) }()
	assert.Equal(t, uint64(4), checkpointReceive(t, live.Events()).EventNumber)
	view, err := ctx.Export()
	assert.NoError(t, err)
	assert.Equal(t, uint64(1), view.LastAppliedEventNumber)
	select {
	case <-first:
		t.Fatal("threshold save did not block")
	default:
	}
	close(release)
	assert.NoError(t, checkpointReceive(t, first))
	assert.NoError(t, checkpointReceive(t, later))
	assert.NoError(t, ctx.FlushProgress())
	assert.Equal(t, [][]string{{"checkpoint"}, {"model_stream_chunk", "model_stream_chunk"},
		{"model_stream_chunk"}}, store.kinds())
	view, err = ctx.Export()
	assert.NoError(t, err)
	assert.NoError(t, gent.ValidateBundle(view))
	assert.Equal(t, uint64(4), view.LastAppliedEventNumber)
	assert.Equal(t, uint64(1), view.Checkpoint.EventNumber)
}

func TestCheckpointModelRequestWaitsForSave(t *testing.T) {
	type input struct{ fail bool }
	type expected struct {
		calls int32
		err   error
	}
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	failure := errors.New("storage unavailable")
	for _, test := range []testCase{
		{"commit", input{}, expected{1, nil}},
		{"failure", input{true}, expected{0, failure}},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := newCheckpointStore("request")
			started := make(chan struct{}, 1)
			release := make(chan struct{})
			releaseSave := sync.OnceFunc(func() { close(release) })
			defer releaseSave()
			store.before = func(batch *gent.SaveBatch) error {
				started <- struct{}{}
				<-release
				if test.input.fail {
					return failure
				}
				return nil
			}
			ctx := gent.NewExecutionContext(context.Background(), "test", nil,
				gent.WithRunId("request"), gent.WithRecorder(store))
			defer ctx.Release()
			var calls atomic.Int32
			model := checkpointModel(func(context.Context, gent.ModelCallRequest) (gent.Stream, error) {
				calls.Add(1)
				return gent.NewCompletedStream(&gent.ContentResponse{}, nil), nil
			})
			done := make(chan error, 1)
			go func() {
				stream, err := ctx.GenerateContentStream(model, gent.ModelCall{},
					gent.NewModelCallRequest(nil))
				if err == nil {
					_, err = stream.Response()
					stream.Close()
				}
				done <- err
			}()
			checkpointReceive(t, started)
			assert.Equal(t, int32(0), calls.Load())
			releaseSave()
			err := checkpointReceive(t, done)
			assert.ErrorIs(t, err, test.expected.err)
			assert.Equal(t, test.expected.calls, calls.Load())
		})
	}
}

func TestCheckpointDeliveryLimits(t *testing.T) {
	type input struct {
		cfg   gent.DeliveryConfig
		count int
		close bool
	}
	type expected struct{ err error }
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	for _, test := range []testCase{
		{"count", input{gent.DeliveryConfig{MaxEvents: 1}, 2, false},
			expected{gent.ErrDeliveryOverflow}},
		{"bytes", input{gent.DeliveryConfig{MaxBytes: 1}, 1, false},
			expected{gent.ErrDeliveryOverflow}},
		{"timeout", input{gent.DeliveryConfig{Timeout: 10 * time.Millisecond}, 1, false},
			expected{gent.ErrDeliveryTimeout}},
		{"unsubscribe", input{gent.DeliveryConfig{}, 1, true}, expected{}},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx := gent.NewExecutionContext(context.Background(), "test", nil)
			defer ctx.Release()
			slow, err := ctx.SubscribeEvents(test.input.cfg)
			assert.NoError(t, err)
			defer slow.Close()
			fast, err := ctx.SubscribeEvents(gent.DeliveryConfig{})
			assert.NoError(t, err)
			defer fast.Close()
			for range test.input.count {
				ctx.PublishCommonEvent("app:progress", "Working", nil)
			}
			if test.input.close {
				slow.Close()
			}
			select {
			case <-slow.Done():
			case <-time.After(10 * time.Second):
				t.Fatal("slow subscription did not close")
			}
			assert.ErrorIs(t, slow.Err(), test.expected.err)
			assert.NoError(t, ctx.RecordingError())
			assert.NoError(t, ctx.FlushProgress())
			for i := range test.input.count {
				event := checkpointReceive(t, fast.Events())
				assert.Equal(t, uint64(i+1), event.EventNumber)
				assert.Equal(t, "app:progress", event.EventName)
			}
			view, err := ctx.Export()
			assert.NoError(t, err)
			assert.Equal(t, uint64(test.input.count), view.LastAppliedEventNumber)
			assert.NoError(t, gent.ValidateBundle(view))
		})
	}
}

func TestCheckpointOversizedProgressEvent(t *testing.T) {
	store := newCheckpointStore("oversized")
	ctx := gent.NewExecutionContext(context.Background(), "test", nil,
		gent.WithRunId("oversized"), gent.WithRecorder(store),
		gent.WithCheckpointPolicy(gent.CheckpointPolicy{MaxBytes: 1}))
	defer ctx.Release()
	assert.NoError(t, ctx.CommitCheckpoint(gent.PhaseInitial))
	assert.NoError(t, ctx.EmitChunk(checkpointChunk("saved whole")))
	view, err := ctx.Export()
	assert.NoError(t, err)
	assert.Equal(t, [][]string{{"checkpoint"}, {"model_stream_chunk"}}, store.kinds())
	assert.Equal(t, uint64(2), view.LastAppliedEventNumber)
	assert.Equal(t, uint64(1), view.Checkpoint.EventNumber)
	assert.NoError(t, gent.ValidateBundle(view))
}

func TestCheckpointRecoveryUsesSavedModelEvidence(t *testing.T) {
	type input struct{ complete bool }
	type expected struct {
		cursor        uint64
		providerCalls int32
		err           error
	}
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	for _, test := range []testCase{
		{"incomplete call", input{}, expected{4, 1, nil}},
		{"complete response", input{true}, expected{5, 0, nil}},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := newCheckpointStore("recovery")
			ctx := gent.NewExecutionContext(context.Background(), "test", nil,
				gent.WithRunId("recovery"), gent.WithRecorder(store),
				gent.WithCheckpointPolicy(gent.CheckpointPolicy{MaxEvents: 2}))
			defer ctx.Release()
			assert.NoError(t, ctx.CommitCheckpoint(gent.PhaseInitial))
			response := &gent.ContentResponse{
				Choices: []*gent.ContentChoice{{
					Completion: gent.ModelCompletion{Status: gent.ModelStatusAnswer},
					CandidateContent: []gent.ContentPart{
						gent.TextContent{Text: "The file follows."},
						gent.BinaryContent{MIMEType: "application/pdf", Data: []byte{1, 2, 3}},
					},
				}},
				Info: &gent.GenerationInfo{InputTokens: 2, OutputTokens: 3, TotalTokens: 5},
			}
			model := checkpointModel(func(worker context.Context, _ gent.ModelCallRequest) (
				gent.Stream, error,
			) {
				stream, err := gent.NewBoundedStreamWithDuration(worker, 1, nil)
				if err != nil {
					return nil, err
				}
				go func() {
					stream.Send(checkpointChunk("first"))
					stream.Send(checkpointChunk("second"))
					if test.input.complete {
						stream.Complete(response, nil)
						return
					}
					<-worker.Done()
					stream.Complete(nil, worker.Err())
				}()
				return stream, nil
			})
			stream, err := ctx.GenerateContentStream(model, gent.ModelCall{}, gent.NewModelCallRequest(nil))
			if !assert.NoError(t, err) {
				return
			}
			defer stream.Close()
			if test.input.complete {
				_, err = stream.Response()
				assert.NoError(t, err)
			} else {
				assert.Eventually(t, func() bool {
					view, err := ctx.Export()
					return err == nil && view.LastAppliedEventNumber == test.expected.cursor
				}, 10*time.Second, time.Millisecond)
			}
			saved, err := ctx.Export()
			assert.NoError(t, err)
			assert.Equal(t, test.expected.cursor, saved.LastAppliedEventNumber)
			assert.Equal(t, uint64(1), saved.Checkpoint.EventNumber)
			ctx.Release()
			if !test.input.complete {
				_, err = stream.Response()
				assert.ErrorIs(t, err, context.Canceled)
			}
			replacement := newCheckpointStore("recovery")
			replacement.view = saved
			fresh, err := gent.RestoreExecutionContext(context.Background(), saved,
				gent.RestoreBindings{Recorder: replacement})
			if !assert.NoError(t, err) {
				return
			}
			defer fresh.Release()
			var calls atomic.Int32
			retry := checkpointModel(func(context.Context, gent.ModelCallRequest) (gent.Stream, error) {
				calls.Add(1)
				return gent.NewCompletedStream(response, nil), nil
			})
			continued, err := fresh.GenerateContentStream(retry, gent.ModelCall{},
				gent.NewModelCallRequest(nil))
			if !assert.NoError(t, err) {
				return
			}
			defer continued.Close()
			var messageId string
			for chunk := range continued.Chunks() {
				messageId = chunk.MetaContent[0].MessageId
			}
			result, err := continued.Response()
			assert.Equal(t, test.expected.err, err)
			// Managed responses link the candidate to the preview identity of this physical call.
			response.Choices[0].CandidateSource = &gent.ContentSource{
				ModelCallId: "recovery:root:model:1", MessageIds: []string{messageId}}
			assert.Equal(t, response, result)
			assert.Equal(t, test.expected.providerCalls, calls.Load())
			assert.Equal(t, int64(5), fresh.Stats().GetCounter(gent.SCTotalTokens))
			assert.NoError(t, fresh.FlushProgress())
			after, err := fresh.Export()
			assert.NoError(t, err)
			assert.NoError(t, gent.ValidateBundle(after))
			assert.Equal(t, saved.Activity, after.Activity[:len(saved.Activity)])
			assert.Equal(t, saved.LastAppliedEventNumber+1,
				after.Activity[len(saved.Activity)].EventNumber)
		})
	}
}

func TestCheckpointFailedProgressKeepsExactRequest(t *testing.T) {
	store := newCheckpointStore("failed")
	failure := errors.New("commit acknowledgement lost")
	var attempts atomic.Int32
	var requests []*gent.SaveBatch
	store.before = func(batch *gent.SaveBatch) error {
		if batch.Checkpoint != nil {
			return nil
		}
		requests = append(requests, batch)
		return nil
	}
	store.after = func(batch *gent.SaveBatch) error {
		if batch.Checkpoint != nil {
			return nil
		}
		if attempts.Add(1) == 1 {
			return failure
		}
		return nil
	}
	ctx := gent.NewExecutionContext(context.Background(), "test", nil,
		gent.WithRunId("failed"), gent.WithRecorder(store),
		gent.WithCheckpointPolicy(gent.CheckpointPolicy{MaxEvents: 2}))
	defer ctx.Release()
	assert.NoError(t, ctx.CommitCheckpoint(gent.PhaseInitial))
	assert.NoError(t, ctx.EmitChunk(checkpointChunk("first")))
	err := ctx.EmitChunk(checkpointChunk("second"))
	assert.ErrorIs(t, err, failure)
	var failed *gent.PersistenceError
	if !assert.ErrorAs(t, err, &failed) {
		return
	}
	assert.Nil(t, failed.Batch.Checkpoint)
	assert.Equal(t, uint64(1), failed.Batch.PreviousEventNumber)
	assert.NoError(t, gent.ValidateBundle(store.view))
	assert.Equal(t, uint64(3), store.view.LastAppliedEventNumber)
	assert.ErrorIs(t, ctx.EmitChunk(checkpointChunk("must not be added")), failure)
	view, err := ctx.RetrySave(context.Background())
	assert.NoError(t, err)
	assert.Equal(t, 2, len(requests))
	assert.Equal(t, requests[0], requests[1])
	assert.Equal(t, failed.Batch, requests[1])
	assert.Equal(t, uint64(3), view.LastAppliedEventNumber)
	assert.Equal(t, uint64(1), view.Checkpoint.EventNumber)
	assert.ErrorIs(t, ctx.RecordingError(), failure)
	assert.NoError(t, gent.ValidateBundle(view))
}

func TestCheckpointPendingByteLimit(t *testing.T) {
	// Measure a real encoded event through the public feed. Fixed metadata keeps each size equal.
	chunk := checkpointChunk("a delta with a fixed size")
	chunk.Timestamp = time.Date(2026, 10, 30, 0, 0, 0, 0, time.UTC)
	probe := gent.NewExecutionContext(context.Background(), "test", nil, gent.WithRunId("bytes"))
	defer probe.Release()
	sub, err := probe.SubscribeEvents(gent.DeliveryConfig{})
	assert.NoError(t, err)
	defer sub.Close()
	assert.NoError(t, probe.EmitChunk(chunk))
	data, err := probe.Codecs().Marshal(checkpointReceive(t, sub.Events()))
	assert.NoError(t, err)

	store := newCheckpointStore("bytes")
	ctx := gent.NewExecutionContext(context.Background(), "test", nil,
		gent.WithRunId("bytes"), gent.WithRecorder(store),
		gent.WithCheckpointPolicy(gent.CheckpointPolicy{MaxEvents: 100, MaxBytes: 2 * len(data)}))
	defer ctx.Release()
	assert.NoError(t, ctx.EmitChunk(chunk))
	assert.Empty(t, store.kinds())
	assert.NoError(t, ctx.EmitChunk(chunk))
	assert.Equal(t, [][]string{{"model_stream_chunk", "model_stream_chunk"}}, store.kinds())
	assert.NoError(t, ctx.EmitChunk(chunk))
	assert.NoError(t, ctx.FlushProgress())
	assert.Equal(t, [][]string{{"model_stream_chunk", "model_stream_chunk"},
		{"model_stream_chunk"}}, store.kinds())
}

func TestCheckpointToolSaveBarriers(t *testing.T) {
	type input struct{ phase gent.ExecutionPhase }
	type expected struct{ models, tools int }
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	for _, test := range []testCase{
		{"before tool", input{gent.PhaseToolReady}, expected{1, 0}},
		{"after first tool", input{gent.PhaseToolCompleted}, expected{1, 1}},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := newCheckpointStore("tools")
			entered, release := make(chan struct{}), make(chan struct{})
			unblock := sync.OnceFunc(func() { close(release) })
			defer unblock()
			var first sync.Once
			store.before = func(batch *gent.SaveBatch) error {
				if batch.Checkpoint != nil && batch.Checkpoint.Phase == test.input.phase {
					first.Do(func() { close(entered); <-release })
				}
				return nil
			}
			model := &recoveryModel{}
			var calls []gent.ToolIdentity
			agent := newRecoveryAgent(model, &calls)
			ctx := gent.NewExecutionContext(context.Background(), "root",
				gent.NewBasicLoopData(gent.NewTask(gent.TextContent{Text: "Read policy P10 twice."})),
				gent.WithRunId("tools"), gent.WithRecorder(store), gent.WithCodecs(store.codecs))
			defer ctx.Release()
			done := make(chan error, 1)
			go func() {
				_, _, err := executor.NewRunner(agent, executor.DefaultConfig()).Execute(ctx)
				done <- err
			}()
			select {
			case <-entered:
			case <-time.After(10 * time.Second):
				t.Fatal("tool boundary was not reached")
			}
			// The recorder is holding the executor here. No model or tool can advance past it.
			assert.Equal(t, test.expected, expected{len(model.requests), len(calls)})
			unblock()
			assert.NoError(t, checkpointReceive(t, done))
			assert.Equal(t, expected{2, 2}, expected{len(model.requests), len(calls)})
			view, err := ctx.Export()
			assert.NoError(t, err)
			assert.NoError(t, gent.ValidateBundle(view, ctx.Codecs()))
		})
	}
}

func TestCheckpointTerminalWaitsForOutcome(t *testing.T) {
	type input struct{ fail bool }
	type expected struct {
		cursor uint64
		err    error
	}
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	failure := errors.New("terminal save failed")
	for _, test := range []testCase{
		{"commit", input{}, expected{4, nil}},
		{"failure", input{true}, expected{2, failure}},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := newCheckpointStore("terminal")
			entered, release := make(chan struct{}, 1), make(chan struct{})
			unblock := sync.OnceFunc(func() { close(release) })
			defer unblock()
			store.before = func(batch *gent.SaveBatch) error {
				if batch.Checkpoint != nil && batch.Checkpoint.Phase == gent.PhaseTerminal {
					entered <- struct{}{}
					<-release
					if test.input.fail {
						return failure
					}
				}
				return nil
			}
			ctx := gent.NewExecutionContext(context.Background(), "test", nil,
				gent.WithRunId("terminal"), gent.WithRecorder(store))
			defer ctx.Release()
			sub, err := ctx.SubscribeEvents(gent.DeliveryConfig{})
			assert.NoError(t, err)
			defer sub.Close()
			assert.NoError(t, ctx.CommitCheckpoint(gent.PhaseInitial))
			assert.Equal(t, "checkpoint", checkpointReceive(t, sub.Events()).Type)
			ctx.SetTermination(gent.TerminationSuccess, []gent.ContentPart{
				gent.TextContent{Text: "The accepted answer."}}, nil)
			ctx.SetAcceptedValue(genttest.Value(gent.ContentText(ctx.FinalResult())))
			ctx.PublishAfterExecution(gent.TerminationSuccess, nil)
			// A different context or observer can flush progress between the hooks and terminal save.
			ctx.PublishCommonEvent("app:progress", "Finishing", nil)
			assert.NoError(t, ctx.FlushProgress())
			assert.Equal(t, "common", checkpointReceive(t, sub.Events()).Type)
			assert.Equal(t, [][]string{{"checkpoint"}, {"common"}}, store.kinds())
			done := make(chan error, 1)
			go func() { done <- ctx.CommitCheckpoint(gent.PhaseTerminal) }()
			checkpointReceive(t, entered)
			select {
			case event := <-sub.Events():
				t.Fatalf("terminal event escaped before commit: %v", event)
			default:
			}
			view, err := ctx.Export()
			assert.NoError(t, err)
			assert.Equal(t, uint64(2), view.LastAppliedEventNumber)
			assert.Equal(t, gent.PhaseInitial, view.Checkpoint.Phase)
			unblock()
			assert.ErrorIs(t, checkpointReceive(t, done), test.expected.err)
			ctx.Release()
			var kinds []string
			for event := range sub.Events() {
				kinds = append(kinds, event.Type)
			}
			if test.input.fail {
				assert.Nil(t, kinds)
			} else {
				assert.Equal(t, []string{"execution_finished", "execution_content"}, kinds)
			}
			view, err = ctx.Export()
			assert.NoError(t, err)
			assert.Equal(t, test.expected.cursor, view.LastAppliedEventNumber)
			assert.NoError(t, gent.ValidateBundle(view))
		})
	}
}

func TestCheckpointCapturesChildStateWithEventPrefix(t *testing.T) {
	store := newCheckpointStore("child-prefix")
	root := gent.NewExecutionContext(context.Background(), "root", nil,
		gent.WithRunId("child-prefix"), gent.WithRecorder(store))
	defer root.Release()
	child := root.SpawnChild("child", nil)
	assert.NoError(t, child.AppendRecord(&gent.HistoryEntry{Origin: gent.RecordOriginal}, true))
	firstSelection := child.ContextDraft().Active
	entered, release := make(chan *gent.SaveBatch, 1), make(chan struct{})
	unblock := sync.OnceFunc(func() { close(release) })
	defer unblock()
	store.before = func(batch *gent.SaveBatch) error {
		if batch.Checkpoint != nil {
			entered <- batch
			<-release
		}
		return nil
	}
	done := make(chan error, 1)
	go func() { done <- root.CommitCheckpoint(gent.PhaseInitial) }()
	batch := checkpointReceive(t, entered)
	assert.NoError(t, child.AppendRecord(&gent.HistoryEntry{Origin: gent.RecordOriginal}, true))
	assert.Equal(t, firstSelection, batch.Checkpoint.Contexts[1].Active)
	assert.Equal(t, gent.HistoryRevision(1), batch.Checkpoint.Revision)
	assert.Equal(t, uint64(2), batch.Checkpoint.EventNumber)
	unblock()
	assert.NoError(t, checkpointReceive(t, done))
	firstSaved, err := root.Export()
	assert.NoError(t, err)
	assert.NoError(t, child.FlushProgress())
	view, err := root.Export()
	assert.NoError(t, err)
	assert.NoError(t, gent.ValidateBundle(view))
	assert.Equal(t, gent.HistoryRevision(2), view.History.Revision)
	assert.Equal(t, firstSaved.Checkpoint, view.Checkpoint)
	assert.Equal(t, uint64(3), view.LastAppliedEventNumber)
	assert.Equal(t, [][]string{{"common", "state_change"}, {"state_change"}}, store.kinds())
}

type checkpointRecorder func(context.Context, *gent.SaveBatch) error

func (save checkpointRecorder) Save(ctx context.Context, batch *gent.SaveBatch) error {
	return save(ctx, batch)
}

func TestCheckpointSaveTimeout(t *testing.T) {
	var calls atomic.Int32
	save := checkpointRecorder(func(worker context.Context, _ *gent.SaveBatch) error {
		<-worker.Done()
		return worker.Err()
	})
	ctx := gent.NewExecutionContext(context.Background(), "test", nil, gent.WithRecorder(save),
		gent.WithCheckpointPolicy(gent.CheckpointPolicy{SaveTimeout: 10 * time.Millisecond}))
	defer ctx.Release()
	model := checkpointModel(func(context.Context, gent.ModelCallRequest) (gent.Stream, error) {
		calls.Add(1)
		return gent.NewCompletedStream(&gent.ContentResponse{}, nil), nil
	})
	stream, err := ctx.GenerateContentStream(model, gent.ModelCall{}, gent.NewModelCallRequest(nil))
	assert.Nil(t, stream)
	assert.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Equal(t, int32(0), calls.Load())
	view, err := ctx.Export()
	assert.NoError(t, err)
	assert.Equal(t, uint64(0), view.LastAppliedEventNumber)
}

func TestCheckpointInvalidLimits(t *testing.T) {
	type input struct {
		policy   gent.CheckpointPolicy
		delivery gent.DeliveryConfig
	}
	type expected struct{ policyError, deliveryError string }
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	want := expected{"gent: checkpoint limits must not be negative",
		"gent: delivery limits must not be negative"}
	for _, test := range []testCase{
		{"count", input{gent.CheckpointPolicy{MaxEvents: -1},
			gent.DeliveryConfig{MaxEvents: -1}}, want},
		{"bytes", input{gent.CheckpointPolicy{MaxBytes: -1},
			gent.DeliveryConfig{MaxBytes: -1}}, want},
		{"timeout", input{gent.CheckpointPolicy{SaveTimeout: -1},
			gent.DeliveryConfig{Timeout: -1}}, want},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx := gent.NewExecutionContext(context.Background(), "test", nil,
				gent.WithCheckpointPolicy(test.input.policy))
			defer ctx.Release()
			assert.EqualError(t, ctx.FlushProgress(), test.expected.policyError)
			sub, err := ctx.SubscribeEvents(test.input.delivery)
			assert.Nil(t, sub)
			assert.EqualError(t, err, test.expected.deliveryError)
			chunks, err := ctx.SubscribeAllWithConfig(test.input.delivery)
			assert.Nil(t, chunks)
			assert.EqualError(t, err, test.expected.deliveryError)
		})
	}
}

func TestCheckpointTerminalPreservesChunkOrder(t *testing.T) {
	store := newCheckpointStore("terminal-order")
	entered, release := make(chan struct{}, 1), make(chan struct{})
	unblock := sync.OnceFunc(func() { close(release) })
	defer unblock()
	store.before = func(batch *gent.SaveBatch) error {
		for _, event := range batch.Events {
			if event.Type == "execution_content" {
				entered <- struct{}{}
				<-release
			}
		}
		return nil
	}
	root := gent.NewExecutionContext(context.Background(), "test", nil,
		gent.WithRunId("terminal-order"), gent.WithRecorder(store))
	defer root.Release()
	ctx := root.SpawnChild("child", nil)
	sub, err := root.SubscribeAllWithConfig(gent.DeliveryConfig{})
	assert.NoError(t, err)
	defer sub.Close()
	assert.NoError(t, root.CommitCheckpoint(gent.PhaseInitial))
	ctx.SetTermination(gent.TerminationSuccess, []gent.ContentPart{
		gent.TextContent{Text: "Accepted."}}, nil)
	ctx.SetAcceptedValue(genttest.Value(gent.ContentText(ctx.FinalResult())))
	ctx.PublishAfterExecution(gent.TerminationSuccess, nil)
	done := make(chan error, 1)
	go func() { done <- ctx.CommitCheckpoint(gent.PhaseTerminal) }()
	checkpointReceive(t, entered)
	later := checkpointChunk("concurrent progress")
	later.ContextId = "caller-supplied-source"
	assert.NoError(t, root.EmitChunk(later))
	select {
	case chunk := <-sub.Chunks():
		t.Fatalf("later chunk passed the blocked terminal save: %v", chunk)
	default:
	}
	unblock()
	assert.NoError(t, checkpointReceive(t, done))
	assert.NoError(t, root.FlushProgress())
	root.CloseStreams()
	var chunks []gent.StreamChunk
	for chunk := range sub.Chunks() {
		chunks = append(chunks, chunk)
	}
	view, err := root.Export()
	assert.NoError(t, err)
	assert.Equal(t, contentChunks(t, view.Activity, ctx.Codecs()), chunks)
	if assert.Len(t, chunks, 2) {
		assert.Equal(t, []gent.ContentPart{gent.TextContent{Text: "Accepted."}}, chunks[0].Content)
		assert.Equal(t, []uint64{3, 4}, []uint64{chunks[0].EventNumber, chunks[1].EventNumber})
	}
}

func TestCheckpointChunkErrorsAreOwned(t *testing.T) {
	ctx := gent.NewExecutionContext(context.Background(), "test", nil)
	defer ctx.Release()
	first, err := ctx.SubscribeAllWithConfig(gent.DeliveryConfig{})
	assert.NoError(t, err)
	defer first.Close()
	second, err := ctx.SubscribeAllWithConfig(gent.DeliveryConfig{})
	assert.NoError(t, err)
	defer second.Close()
	assert.NoError(t, ctx.EmitChunk(gent.StreamChunk{Err: errors.New("provider stopped")}))
	one := checkpointReceive(t, first.Chunks())
	two := checkpointReceive(t, second.Chunks())
	one.Err.(*gent.SavedError).Message = "consumer changed its copy"
	assert.Equal(t, &gent.SavedError{Message: "provider stopped"}, two.Err)
	assert.NoError(t, ctx.FlushProgress())
	view, err := ctx.Export()
	assert.NoError(t, err)
	assert.Equal(t, []gent.StreamChunk{two}, contentChunks(t, view.Activity, ctx.Codecs()))
}
