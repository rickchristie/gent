package gent_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/rickchristie/gent"
	"github.com/rickchristie/gent/executor"
	"github.com/stretchr/testify/assert"
)

type failedSetupLoop struct{ reviewLoop }

func (failedSetupLoop) ConfigureExecution(*gent.ExecutionContext) error {
	return errors.New("setup failed")
}

func TestReliabilitySetupFailureReleasesContext(t *testing.T) {
	parent, cancel := context.WithCancel(context.Background())
	defer cancel()
	for i := 0; i < 32; i++ {
		ctx := gent.NewExecutionContext(parent, "test", nil)
		_, result, err := executor.NewRunner(failedSetupLoop{}, executor.Config{}).Execute(ctx)
		assert.Nil(t, result)
		assert.EqualError(t, err, "setup failed")
		assert.ErrorIs(t, ctx.Context().Err(), context.Canceled)
		assert.False(t, ctx.IsWorkerInterrupted())
		assert.NoError(t, parent.Err())
	}
}

func TestReliabilityCompleteChildReleasesWork(t *testing.T) {
	root := gent.NewExecutionContext(context.Background(), "parent", nil)
	defer root.Release()
	for i := 0; i < 32; i++ {
		child := root.SpawnChild("child", nil)
		grandchild := child.SpawnChild("grandchild", nil)
		assert.NoError(t, child.CompleteChild(grandchild))
		assert.NoError(t, root.CompleteChild(child))
		assert.ErrorIs(t, grandchild.Context().Err(), context.Canceled)
		assert.ErrorIs(t, child.Context().Err(), context.Canceled)
		assert.False(t, child.IsWorkerInterrupted())
		assert.NoError(t, root.Context().Err())
	}
	view, err := root.Export()
	assert.NoError(t, err)
	assert.NoError(t, gent.ValidateBundle(view))
}

func TestReliabilityRestoreFailureReleasesConfiguredWork(t *testing.T) {
	parent, cancel := context.WithDeadline(context.Background(), time.Now().Add(time.Hour))
	defer cancel()
	ctx := gent.NewExecutionContext(parent, "test", nil)
	defer ctx.Release()
	if !assert.NoError(t, ctx.CommitCheckpoint(gent.PhaseInitial)) {
		return
	}
	view, err := ctx.Export()
	if !assert.NoError(t, err) {
		return
	}
	var configured *gent.ExecutionContext
	restored, err := gent.RestoreExecutionContext(parent, view, gent.RestoreBindings{
		Configure: func(ctx *gent.ExecutionContext) error {
			configured = ctx
			return errors.New("restore setup failed")
		},
	})
	assert.Nil(t, restored)
	assert.EqualError(t, err, "restore setup failed")
	if assert.NotNil(t, configured) {
		assert.ErrorIs(t, configured.Context().Err(), context.Canceled)
	}
	assert.NoError(t, parent.Err())
}

func TestReliabilityExportDuringAcceptance(t *testing.T) {
	ctx := gent.NewExecutionContext(context.Background(), "test", nil,
		gent.WithCheckpointPolicy(gent.CheckpointPolicy{MaxEvents: 16}))
	defer ctx.Release()
	if !assert.NoError(t, ctx.EmitChunk(gent.StreamChunk{
		MetaContent: []*gent.MetaContent{
			{
				Type: gent.MetaContentIntermediary,
				Content: []gent.ContentPart{
					gent.TextContent{Text: "first"},
				},
			},
		},
	})) {
		return
	}
	assert.NoError(t, ctx.FlushProgress())
	first, err := ctx.Export()
	if !assert.NoError(t, err) {
		return
	}
	var wg sync.WaitGroup
	start := make(chan struct{})
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		for i := 0; i < 128; i++ {
			assert.NoError(t, ctx.EmitChunk(gent.StreamChunk{
				MetaContent: []*gent.MetaContent{
					{
						Type: gent.MetaContentIntermediary,
						Content: []gent.ContentPart{
							gent.TextContent{Text: "next"},
						},
					},
				},
			}))
		}
	}()
	go func() {
		defer wg.Done()
		<-start
		for i := 0; i < 32; i++ {
			view, err := ctx.Export()
			if !assert.NoError(t, err) {
				return
			}
			assert.NoError(t, gent.ValidateBundle(view))
			assert.Len(t, view.Activity, int(view.LastAppliedEventNumber))
			assert.Len(t, view.Batches, 1+int(view.LastAppliedEventNumber-1)/16)
			// Public snapshots are caller-owned, including the growing indexes and payloads.
			clear(view.Batches)
			view.Activity[0].Type = "changed by caller"
		}
	}()
	close(start)
	wg.Wait()
	last, err := ctx.Export()
	assert.NoError(t, err)
	assert.NoError(t, gent.ValidateBundle(last))
	assert.Len(t, last.Activity, 129)
	assert.Len(t, last.Batches, 9)
	assert.Equal(t, first.Activity, last.Activity[:1])
	assert.Len(t, first.Activity, 1)
	assert.Len(t, first.Batches, 1)
}
