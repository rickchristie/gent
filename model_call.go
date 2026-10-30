package gent

import (
	"context"
	"errors"
	"fmt"
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
	// Hooks may change nested options and messages. Give them a request owned by this call so
	// reusable agent settings and requests from other runs cannot change through shared pointers.
	request, err = cloneOwned(ctx.Codecs(), request)
	if err != nil {
		return nil, ctx.stopRecording(err)
	}
	expectedOutput, err := captureOutputControls(request.Options)
	if err != nil {
		return nil, err
	}
	before := ctx.PublishBeforeModelCall(call.Model, request, WithModelCallId(call.Id),
		WithModelStream(call.StreamId, call.StreamTopicId), WithModelCallSource(call.Source),
		WithModelProvider(call.Provider))
	// A physical retry may reuse the logical call ID within the same worker attempt.
	// Its preview must have a new identity; saved replay keeps this identity unchanged.
	messagePrefix := before.ContextId + ":message:" + randomIdentity() + ":"
	if err := ctx.RecordingError(); err != nil {
		return nil, err
	}
	owned, err := ctx.Codecs().Clone(before.Request)
	if err != nil {
		return nil, ctx.stopRecording(err)
	}
	request = owned.(ModelCallRequest)
	if err := checkOutputControls(expectedOutput, request.Options); err != nil {
		return nil, err
	}
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
			chunk = cloneStreamChunk(chunk)
			// A model cannot accept the execution result. Even custom adapters that send
			// Content are shown as provisional text until the executor commits success.
			if len(chunk.Content) > 0 {
				chunk.MetaContent = append(chunk.MetaContent, &MetaContent{
					Type: MetaContentIntermediary, Content: chunk.Content})
				chunk.Content = nil
			}
			for _, item := range chunk.MetaContent {
				item.MessageId = scopedMessageId(messagePrefix, item.MessageId)
			}
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
				recordingErr = ctx.emitChunk(&chunk)
			}
			if recordingErr != nil {
				cancel()
				provider.Close()
				continue
			}
			output.Send(chunk)
		}
		response, providerErr := provider.Response()
		if response != nil && recordingErr == nil {
			response, recordingErr = cloneOwned(ctx.Codecs(), response)
			if recordingErr == nil {
				scopeResponseMessages(before, messagePrefix, response)
			}
		}
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
		output.Complete(response, providerErr)
	}()
	return output, nil
}

func scopedMessageId(prefix, local string) string {
	if local == "" {
		local = "message:0"
	}
	return prefix + local
}

func scopeResponseMessages(
	call *BeforeModelCallEvent, prefix string, response *ContentResponse,
) {
	for index, choice := range response.Choices {
		for _, item := range choice.MetaContent {
			item.MessageId = scopedMessageId(prefix, item.MessageId)
		}
		if len(choice.CandidateContent) == 0 {
			continue
		}
		source := cloneContentSource(choice.CandidateSource)
		if source == nil {
			source = &ContentSource{}
		}
		if len(source.MessageIds) == 0 {
			source.MessageIds = []string{fmt.Sprintf("message:%d", index)}
		}
		source.ModelCallId, source.StreamId = call.ModelCallId, call.StreamId
		source.StreamTopicId = call.StreamTopicId
		for i, id := range source.MessageIds {
			source.MessageIds[i] = scopedMessageId(prefix, id)
		}
		choice.CandidateSource = source
	}
}
