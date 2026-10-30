package react

import (
	"context"

	"github.com/rickchristie/gent"
	agentutil "github.com/rickchristie/gent/agents"
)

func finishGuardedStream(ctx *gent.ExecutionContext, stream gent.Stream,
	acc *gent.StreamAccumulator, guard *agentutil.RepetitionResult,
) (*gent.ContentResponse, *agentutil.RepetitionResult, error) {
	stream.Close()
	response, err := stream.Response()
	// Closing a live stream can report cancellation. Only that expected error is recoverable;
	// a transport, recording, or cleanup failure still ends the run, including joined errors.
	if err != nil && !onlyStreamCancellation(err) {
		return nil, nil, err
	}
	if err := ctx.Context().Err(); err != nil {
		return nil, nil, err
	}
	if response != nil && len(response.Choices) > 0 && response.Choices[0] != nil {
		choice := response.Choices[0]
		if err := choice.ValidateCompletion(); err != nil {
			return nil, nil, err
		}
		if choice.Completion.Status == gent.ModelStatusRefused ||
			choice.Completion.Status == gent.ModelStatusFailed {
			return response, nil, nil
		}
	}
	return acc.ResponseWithInfo(response), guard, nil
}

func onlyStreamCancellation(err error) bool {
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		causes := joined.Unwrap()
		for _, cause := range causes {
			if !onlyStreamCancellation(cause) {
				return false
			}
		}
		return len(causes) > 0
	}
	if wrapped, ok := err.(interface{ Unwrap() error }); ok {
		return onlyStreamCancellation(wrapped.Unwrap())
	}
	return err == context.Canceled
}
