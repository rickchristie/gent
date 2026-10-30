package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/rickchristie/gent"
	"github.com/rickchristie/gent/executor"
	"github.com/rickchristie/gent/trace"
)

var errBusy = errors.New("run already has a worker")

type liveRun struct {
	seq *trace.Sequencer
}

type API struct {
	store   *Store
	model   func() gent.Model
	worker  context.Context
	mu      sync.Mutex
	running map[string]*liveRun
	wait    sync.WaitGroup
}

func (a *API) start(runID, question string, resume bool) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.running[runID] != nil {
		return errBusy
	}
	if len(a.running) >= 16 {
		return errors.New("all 16 worker slots are in use")
	}
	if err := a.worker.Err(); err != nil {
		return err
	}
	agent := newAgent(a.model())
	if err := agent.RegisterCodecs(a.store.codecs); err != nil {
		return err
	}
	saved, err := a.store.load(runID)
	if resume && err != nil {
		return err
	}
	if !resume && !errors.Is(err, errMissing) {
		if err != nil {
			return err
		}
		return fmt.Errorf("run already exists")
	}
	writer, err := a.store.claim(runID)
	if err != nil {
		return err
	}
	var seq *trace.Sequencer
	if resume {
		seq, err = trace.RestoreSequencer(saved, displayConfig(a.store.codecs))
	} else {
		seq, err = trace.NewSequencer(runID, displayConfig(a.store.codecs))
	}
	if err != nil {
		return err
	}
	live := &liveRun{seq: seq}
	a.running[runID] = live
	a.wait.Add(1)
	go func() {
		defer a.wait.Done()
		defer seq.Close()
		defer func() {
			a.mu.Lock()
			delete(a.running, runID)
			a.mu.Unlock()
		}()
		worker, cancel := context.WithTimeout(a.worker, 2*time.Minute)
		defer cancel()
		var runErr error
		if resume {
			_, _, runErr = resumeSaved(worker, saved, agent, writer, seq, a.store.codecs)
		} else {
			runner := executor.NewRunner(agent, executor.DefaultConfig())
			data := gent.NewBasicLoopData(&gent.Task{Messages: []*gent.ModelMessage{
				gent.TextParts(gent.ChatMessageTypeSystem, agentPrompt),
				gent.TextParts(gent.ChatMessageTypeHuman, question),
			}})
			ctx := gent.NewExecutionContext(worker, "order-helper", data,
				gent.WithRunId(runID), gent.WithCodecs(a.store.codecs),
				gent.WithRecorder(writer), gent.WithObserver(seq))
			limits := gent.DefaultLimits()
			for i := range limits {
				if limits[i].Key == gent.SCIterations.Self() {
					limits[i].MaxValue = 6
				}
			}
			ctx.SetLimits(limits)
			_, _, runErr = runner.Execute(ctx)
		}
		if runErr != nil {
			log.Printf("run %s stopped: %v", runID, runErr)
		}
	}()
	return nil
}

func (a *API) active(runID string) *liveRun {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.running[runID]
}

func (a *API) snapshot(runID string) (BrowserState, error) {
	saved, err := a.store.load(runID)
	if err != nil {
		return BrowserState{}, err
	}
	seq, err := trace.RestoreSequencer(saved, displayConfig(a.store.codecs))
	if err != nil {
		return BrowserState{}, err
	}
	defer seq.Close()
	return browserState(seq.Snapshot()), nil
}
