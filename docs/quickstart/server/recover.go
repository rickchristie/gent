package main

import (
	"context"
	"log"

	"github.com/rickchristie/gent"
	"github.com/rickchristie/gent/agents/react"
	"github.com/rickchristie/gent/executor"
	"github.com/rickchristie/gent/trace"
)

func resumeSaved(worker context.Context, saved gent.RunView, agent *react.Agent[string],
	writer gent.Recorder, seq *trace.Sequencer, codecs *gent.CodecRegistry,
) (string, *gent.ExecutionResult, error) {
	runner := executor.NewRunner(agent, executor.DefaultConfig())
	var feed *trace.LiveFeed
	answer, result, err := runner.Resume(worker, saved, gent.RestoreBindings{
		Recorder: writer, Codecs: codecs,
		Configure: func(ctx *gent.ExecutionContext) error {
			var err error
			feed, err = seq.Follow(ctx, gent.DeliveryConfig{})
			return err
		},
	})
	if feed != nil {
		if err := feed.Wait(); err != nil {
			log.Printf("live display stopped: %v", err)
		}
	}
	return answer, result, err
}
