// Package modelcontext connects provider preparation to the managed execution recorder.
// It has no dependency on Gent types, so adapters and the managed boundary can share it.
package modelcontext

import "context"

type recorderKey struct{}

// WithRecorder binds private preparation evidence to one model call. The recorder must accept
// the evidence before dependent provider work starts. Standalone adapters need no recorder.
func WithRecorder(ctx context.Context, record func(map[string]any) error) context.Context {
	return context.WithValue(ctx, recorderKey{}, record)
}

// Record saves a preparation decision when the call belongs to a managed execution.
func Record(ctx context.Context, evidence map[string]any) error {
	if record, ok := ctx.Value(recorderKey{}).(func(map[string]any) error); ok {
		return record(evidence)
	}
	return nil
}
