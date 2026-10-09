package gent_test

import (
	"context"
	"testing"

	"github.com/rickchristie/gent"
	"github.com/rickchristie/gent/agents/react"
	"github.com/rickchristie/gent/compaction"
	"github.com/rickchristie/gent/executor"
	"github.com/rickchristie/gent/format"
	"github.com/stretchr/testify/assert"
)

type historySequenceModel struct {
	texts    []string
	requests []gent.ModelCallRequest
}

func (m *historySequenceModel) GenerateContentStream(
	_ context.Context,
	request gent.ModelCallRequest,
) (
	gent.Stream, error,
) {
	text := m.texts[len(m.requests)]
	m.requests = append(m.requests, request)
	return gent.NewCompletedStream(&gent.ContentResponse{
		Choices: []*gent.ContentChoice{{Content: text}},
		Info:    &gent.GenerationInfo{InputTokens: 2, OutputTokens: 1, TotalTokens: 3},
	}, nil), nil
}

func TestHistoryCompaction_PreservesBothSummariesAndResumes(t *testing.T) {
	worker, cancel := context.WithCancel(context.Background())
	defer cancel()
	store := &recoveryStore{
		codecs: gent.NewCodecRegistry(),
		view:   gent.RunView{SchemaVersion: gent.RunSchemaVersion, RunId: "summaries"},
		cancel: cancel,
		phase:  gent.PhaseCompactionCompleted,
		after:  2,
	}
	model := &historySequenceModel{texts: []string{recoveryAction, recoveryAction}}
	summaries := &historySequenceModel{texts: []string{
		"Policy P10 permits refunds within 30 days.", "Two policy lookups were completed.",
	}}
	newAgent := func(model gent.Model) *react.Agent {
		return react.NewAgent(model).WithRecoveryVersion("summaries/v1").RegisterTool(
			gent.NewToolFunc("policy", "Read policy text", nil,
				func(context.Context, recoveryInput) (string, error) {
					return "Policy P10 permits refunds within 30 days.", nil
				}))
	}
	configure := func(ctx *gent.ExecutionContext, model gent.Model) {
		ctx.SetLimits(nil)
		ctx.SetCompaction(
			compaction.NewStatThresholdTrigger().OnCounter(gent.SCIterations.Self(), 1),
			compaction.NewSummarization(model, format.NewXML()).WithKeepRecent(0),
		)
	}
	agent := newAgent(model)
	ctx := gent.NewExecutionContext(worker, "root", gent.NewBasicLoopData(
		&gent.Task{Text: "Check the policy twice."}), gent.WithRunId("summaries"),
		gent.WithRecorder(store), gent.WithCodecs(store.codecs))
	configure(ctx, summaries)
	_, err := executor.New[gent.LoopData](agent, executor.DefaultConfig()).Execute(ctx)
	assert.ErrorIs(t, err, context.Canceled)
	bundle, err := ctx.Export()
	assert.NoError(t, err)
	assert.NoError(t, gent.ValidateBundle(bundle, ctx.Codecs()))
	assert.Equal(t, gent.PhaseCompactionCompleted, bundle.Checkpoint.Phase)
	var summaryEntries []*gent.HistoryEntry
	var markers []*gent.CompactionRecord
	var originals []*gent.HistoryEntry
	for _, entry := range bundle.History.Entries {
		switch entry.Kind {
		case gent.HistorySummary:
			summaryEntries = append(summaryEntries, entry)
		case gent.HistoryCompaction:
			markers = append(markers, entry.Compaction)
		case gent.HistoryMessages:
			originals = append(originals, entry)
		}
	}
	if !assert.Len(t, summaryEntries, 2) || !assert.Len(t, markers, 2) ||
		!assert.Len(t, originals, 2) {
		return
	}
	assert.Equal(t, []gent.EntryId{originals[0].Id}, markers[0].InputIds)
	assert.Equal(t, []gent.EntryId{summaryEntries[0].Id, originals[1].Id}, markers[1].InputIds)
	assert.Equal(t, []gent.EntryId{summaryEntries[1].Id}, markers[1].Active)
	assert.Len(t, markers[0].ModelCallIds, 1)
	assert.Len(t, markers[1].ModelCallIds, 1)
	// The first summary remains fully decodable even though the second summary lost the fact.
	first, second := summaryEntries[0], summaryEntries[1]
	assert.Equal(t, `<prior_work_summary>
Policy P10 permits refunds within 30 days.
</prior_work_summary>`, first.Messages[0].Parts[0].(interface{ String() string }).String())
	assert.Equal(t, `<prior_work_summary>
Two policy lookups were completed.
</prior_work_summary>`, second.Messages[0].Parts[0].(interface{ String() string }).String())
	assert.NotEqual(t, summaries.requests[0].Messages, summaries.requests[1].Messages)
	freshModel := &historySequenceModel{texts: []string{"<answer>Done.</answer>"}}
	freshSummary := &historySequenceModel{}
	freshAgent := newAgent(freshModel)
	bindings := recoveryBindings(t, freshAgent)
	bindings.Configure = func(ctx *gent.ExecutionContext) error {
		configure(ctx, freshSummary)
		return nil
	}
	fresh, _, err := resumeRecovery(freshAgent, bundle, bindings)
	if !assert.NoError(t, err) {
		return
	}
	assert.Empty(t, freshSummary.requests)
	assert.Len(t, freshModel.requests, 1)
	assert.Equal(t, int64(2), fresh.Stats().GetCounter(gent.SCCompactions))
	assert.Equal(t, 3, fresh.Iteration())
}
