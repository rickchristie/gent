package toolchain

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/rickchristie/gent"
	"github.com/stretchr/testify/assert"
)

type programReplyStore struct {
	view     gent.RunView
	codecs   *gent.CodecRegistry
	accepted *gent.ToolCallRecord
	snapshot gent.RunView
	failure  error
}

func (s *programReplyStore) Save(_ context.Context, batch *gent.SaveBatch) error {
	next, err := gent.ApplyBatch(s.view, batch, s.codecs)
	if err != nil {
		return err
	}
	s.view = next
	for _, event := range batch.Events {
		if event.Type != "tool_call_finished" {
			continue
		}
		value, err := s.codecs.DecodeEvent(event)
		if err != nil {
			return err
		}
		finished := value.(*gent.AfterToolCallEvent)
		if finished.ToolName != nativeCodeTool {
			continue
		}
		s.accepted = finished.Output.(*gent.ToolCallRecord)
		// Preserve the progress/control gap even when the original worker later completes.
		value, err = s.codecs.Clone(next)
		if err != nil {
			return err
		}
		s.snapshot = value.(gent.RunView)
		return s.failure
	}
	return nil
}

func TestProgramReusesAcceptedOuterReply(t *testing.T) {
	type input struct{ saveError error }
	type expected struct{ calls, recoveries int }
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	cases := []testCase{
		{"progress before control checkpoint", input{}, expected{1, 0}},
		{"lost acknowledgement", input{errors.New("outer reply acknowledgement lost")}, expected{1, 0}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			calls, recoveries := 0, 0
			build := func() *JsToolChainWrapper {
				tool := gent.DefineTool("fetch", func(context.Context, struct{}) (
					*gent.ToolExecutionResponse, error,
				) {
					calls++
					return &gent.ToolExecutionResponse{Status: gent.ToolExecuted,
						Parts: []gent.ContentPart{gent.PDFPart([]byte("%PDF-test"))}}, nil
				}).Recovery(func(context.Context, *gent.ToolRecoveryRequest[struct{}]) (
					*gent.ToolRecoveryResponse, error,
				) {
					recoveries++
					return &gent.ToolRecoveryResponse{Status: gent.ToolRecoveryUnresolved}, nil
				}).MustBuild()
				return NewJsToolChainWrapper(New().RegisterTool(tool))
			}
			chain := build()
			codecs := gent.NewCodecRegistry()
			assert.NoError(t, chain.RegisterCodecs(codecs))
			store := &programReplyStore{codecs: codecs, failure: test.input.saveError,
				view: gent.RunView{SchemaVersion: gent.RunSchemaVersion, RunId: "saved-program"}}
			ctx := gent.NewExecutionContext(context.Background(), "program", gent.NewBasicLoopData(nil),
				gent.WithRunId("saved-program"), gent.WithCodecs(codecs), gent.WithRecorder(store),
				gent.WithCheckpointPolicy(gent.CheckpointPolicy{MaxEvents: 1}))
			defer ctx.Release()
			// Normal loop setup records this gauge. Restore also computes it from saved history.
			ctx.Stats().SetGauge(gent.SGScratchpadLength, 0)
			raw, err := json.Marshal(codeInput{Code: `const reply=tool.call({tool:"fetch",args:{}});
tool.forward(reply.parts.filter(p=>p.kind==="bytes")); console.log("computed value");`})
			assert.NoError(t, err)
			plan, err := chain.PrepareCalls(ctx,
				[]*gent.ModelToolCallRequest{nativeCall(nativeCodeTool, string(raw))})
			if !assert.NoError(t, err) {
				return
			}
			_, err = chain.ExecuteNext(ctx, plan, testFormat())
			assert.ErrorIs(t, err, test.input.saveError)
			if !assert.NotNil(t, store.accepted) {
				return
			}
			parts := store.accepted.Response().Parts()
			assert.Equal(t, []gent.ContentPart{
				gent.TextPart("Tool fetch (call saved-program:root:tool:2):"),
				gent.PDFPart([]byte("%PDF-test")), gent.TextPart(`<output>
computed value
</output>`),
			}, parts)
			// Restore with a new chain and registry, as another server would do.
			chain, codecs = build(), gent.NewCodecRegistry()
			assert.NoError(t, chain.RegisterCodecs(codecs))
			fresh, err := gent.RestoreExecutionContext(context.Background(), store.snapshot,
				gent.RestoreBindings{Codecs: codecs})
			if !assert.NoError(t, err) {
				return
			}
			defer fresh.Release()
			_, found, err := fresh.CheckpointState("gent:program")
			assert.NoError(t, err)
			assert.True(t, found, "the saved control checkpoint must still contain the program")
			saved, found, err := fresh.SavedToolPlan()
			if !assert.NoError(t, err) || !assert.True(t, found) {
				return
			}
			actual, err := chain.ExecuteNext(fresh, saved, testFormat())
			if !assert.NoError(t, err) {
				return
			}
			assert.Equal(t, store.accepted, actual)
			assert.Equal(t, test.expected, expected{calls, recoveries})
			assertProgramStateCleared(t, fresh)
			view, err := fresh.Export()
			assert.NoError(t, err)
			assert.Equal(t, store.snapshot.Runtime, view.Runtime)
			assert.Equal(t, store.snapshot.Activity, view.Activity[:len(store.snapshot.Activity)])
			var newToolEvents []string
			for _, event := range view.Activity[len(store.snapshot.Activity):] {
				if event.ToolCallId != "" {
					newToolEvents = append(newToolEvents, event.Type)
				}
			}
			assert.Nil(t, newToolEvents)
			assert.Equal(t, []*gent.ToolCallRecord{store.accepted}, saved.Results)
		})
	}
}
