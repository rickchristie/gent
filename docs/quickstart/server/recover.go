package main

import (
	"context"

	"github.com/rickchristie/gent"
	"github.com/rickchristie/gent/agents/react"
	"github.com/rickchristie/gent/executor"
	"github.com/rickchristie/gent/trace"
)

func resumeSaved(worker context.Context, saved gent.RunView, agent *react.Agent,
	writer gent.Recorder, seq *trace.Sequencer, codecs *gent.CodecRegistry,
) (*gent.ExecutionResult, error) {
	runner := executor.NewRunner(agent, executor.DefaultConfig())
	return runner.Resume(worker, saved, gent.RestoreBindings{
		Recorder: writer, Codecs: codecs,
		Observers: []gent.RecordedEventObserver{seq},
	})
}
