package main

import (
	"strconv"

	"github.com/rickchristie/gent"
	"github.com/rickchristie/gent/trace"
)

func displayConfig(codecs *gent.CodecRegistry) trace.Config {
	return trace.Config{Codecs: codecs, IncludeChunkText: true, IncludeRunOutput: true,
		MaxContentBytes: trace.NoContentLimit, MaxMetaContentBytes: trace.NoContentLimit}
}

type Preview struct {
	ID        string              `json:"id"`
	AttemptID string              `json:"attemptId"`
	ContextID string              `json:"contextId"`
	Status    trace.StepStatus    `json:"status"`
	Parts     []*gent.MetaContent `json:"parts"`
}

type Answer struct {
	ContextID        string             `json:"contextId"`
	MessageID        string             `json:"messageId"`
	SourceMessageIDs []string           `json:"sourceMessageIds"`
	Content          []gent.ContentPart `json:"content"`
}

// This is an application display view, not the lossless RunView used for recovery.
type BrowserState struct {
	RunID           string          `json:"runId"`
	LastEventNumber string          `json:"lastEventNumber"`
	Status          trace.RunStatus `json:"status"`
	Previews        []Preview       `json:"previews"`
	Answers         []Answer        `json:"answers"`
}

func browserState(snapshot *trace.Snapshot) BrowserState {
	state := BrowserState{RunID: snapshot.RunId, Status: snapshot.Status,
		LastEventNumber: strconv.FormatUint(snapshot.LastEventNumber, 10),
		Previews:        []Preview{}, Answers: []Answer{}}
	for _, call := range snapshot.ModelCalls {
		parts := []*gent.MetaContent{}
		if call.Stream != nil {
			parts = append(parts, call.Stream.MetaContent...)
		}
		state.Previews = append(state.Previews, Preview{ID: call.Id, AttemptID: call.AttemptId,
			ContextID: call.ContextId, Status: call.Status, Parts: parts})
	}
	for _, context := range snapshot.Contexts {
		if context.MessageId != "" {
			state.Answers = append(state.Answers, Answer{ContextID: context.Id,
				MessageID: context.MessageId, SourceMessageIDs: context.SourceMessageIds,
				Content: gent.CloneContentParts(context.Content)})
		}
	}
	return state
}

type Frame struct {
	Kind        string       `json:"kind"`
	RunID       string       `json:"runId"`
	EventNumber string       `json:"eventNumber,omitempty"`
	Event       *trace.Event `json:"event,omitempty"`
}

func eventFrame(event *trace.Event) Frame {
	return Frame{Kind: "event", RunID: event.RunId,
		EventNumber: strconv.FormatUint(event.EventNumber, 10), Event: event}
}
