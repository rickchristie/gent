package trace

import (
	"encoding/json"
	"fmt"
)

func jsonSafeValue(value any) any {
	if value == nil {
		return nil
	}
	// Trace snapshots and events must always be JSON-marshalable for UI replay. Redactors can
	// return arbitrary values, so payload capture records a structured error instead of failing.
	data, err := json.Marshal(value)
	if err != nil {
		return PayloadCaptureError{Type: fmt.Sprintf("%T", value), Error: err.Error()}
	}
	var decoded any
	if err := json.Unmarshal(data, &decoded); err != nil {
		return PayloadCaptureError{Type: fmt.Sprintf("%T", value), Error: err.Error()}
	}
	return decoded
}

func cloneAny(value any) any {
	return jsonSafeValue(value)
}
