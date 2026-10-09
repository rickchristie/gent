package gent

import (
	"context"
	"errors"
	"time"
)

// GenerateContentStream is the managed model boundary. Hooks finish before the owned request is
// accepted; the adapter receives precisely those options and messages. Completion waits for the
// provider and accepted history, including when a consumer stops reading early.
func (ctx *ExecutionContext) GenerateContentStream(
	model Model, call ModelCall, request ModelCallRequest,
) (Stream, error) {
	if ctx == nil {
		ctx = NewExecutionContext(context.Background(), "model", NewBasicLoopData(nil))
	}
	if err := ctx.RecordingError(); err != nil {
		return nil, err
	}
	if model == nil {
		return nil, errors.New("gent: nil model adapter")
	}
	if call.Model == "" {
		if named, ok := model.(interface{ ModelName() string }); ok {
			call.Model = named.ModelName()
		}
	}
	if call.Provider == "" {
		if provider, ok := model.(interface{ Provider() string }); ok {
			call.Provider = provider.Provider()
		}
	}
	if request.Options.Model == "" {
		request.Options.Model = call.Model
	}
	ctx.run.mu.RLock()
	request.SelectionRevision = ctx.run.working.Revision
	request.ActiveEntryIds = append([]EntryId(nil), ctx.active...)
	ctx.run.mu.RUnlock()
	call.Id = ctx.modelCallId(call.Id)
	stream, err := ctx.replayModelCall(call.Id)
	if err != nil {
		return nil, err
	}
	if stream != nil {
		return stream, nil
	}
	before := ctx.PublishBeforeModelCall(call.Model, request, WithModelCallId(call.Id),
		WithModelStream(call.StreamId, call.StreamTopicId), WithModelCallSource(call.Source),
		WithModelProvider(call.Provider))
	if err := ctx.RecordingError(); err != nil {
		return nil, err
	}
	owned, err := ctx.Codecs().Clone(before.Request)
	if err != nil {
		return nil, ctx.stopRecording(err)
	}
	request = owned.(ModelCallRequest)
	if err := ctx.FlushProgress(); err != nil {
		return nil, err
	}
	if err := ctx.Context().Err(); err != nil {
		return nil, err
	}
	providerCtx, cancel := context.WithCancel(ctx.Context())
	provider, err := model.GenerateContentStream(providerCtx, request)
	if err != nil {
		cancel()
		ctx.PublishAfterModelCallForIteration(before.Iteration, before.Model, request, nil, 0, err,
			WithModelCallId(
				before.ModelCallId,
			), WithModelStream(
				before.StreamId,
				before.StreamTopicId,
			))
		return nil, errors.Join(err, ctx.FlushProgress())
	}
	if provider == nil {
		cancel()
		err := errors.New("gent: model adapter returned a nil stream")
		ctx.PublishAfterModelCallForIteration(before.Iteration, before.Model, request, nil, 0, err,
			WithModelCallId(
				before.ModelCallId,
			), WithModelStream(
				before.StreamId,
				before.StreamTopicId,
			))
		return nil, errors.Join(err, ctx.FlushProgress())
	}
	output := newStreamBufferWithClose(func() { cancel(); provider.Close() })
	started := time.Now()
	go func() {
		defer cancel()
		var recordingErr error
		for chunk := range provider.Chunks() {
			if chunk.Timestamp.IsZero() {
				chunk.Timestamp = time.Now()
			}
			chunk.ModelCallId = before.ModelCallId
			chunk.StreamId = before.StreamId
			chunk.StreamTopicId = before.StreamTopicId
			chunk.ContextId = before.ContextId
			chunk.ParentContextId = before.ParentContextId
			chunk.Iteration = before.Iteration
			chunk.Depth = before.Depth
			chunk.Source = before.Source
			if recordingErr == nil {
				recordingErr = ctx.EmitChunk(chunk)
			}
			if recordingErr != nil {
				cancel()
				provider.Close()
				continue
			}
			output.Send(chunk)
		}
		response, providerErr := provider.Response()
		if recordingErr == nil {
			ctx.PublishAfterModelCallForIteration(before.Iteration, before.Model, request, response,
				time.Since(started), providerErr, WithModelCallId(before.ModelCallId),
				WithModelStream(before.StreamId, before.StreamTopicId),
				WithModelCallSource(before.Source), WithModelProvider(before.Provider))
			recordingErr = ctx.FlushProgress()
		}
		if recordingErr != nil {
			output.Complete(nil, recordingErr)
			return
		}
		cloned, err := ctx.Codecs().Clone(response)
		if err != nil {
			output.Complete(nil, ctx.stopRecording(err))
			return
		}
		output.Complete(cloned.(*ContentResponse), providerErr)
	}()
	return output, nil
}
