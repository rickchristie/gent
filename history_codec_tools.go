package gent

import (
	"encoding/json"
	"errors"
	"reflect"
)

// Private framework fields need an explicit codec. The wire structs retain typed accepted
// outcomes while diagnostic EncodedValues remain opaque. Restore never calls live preparation.
type frameworkValueCodec[T any] struct {
	name   string
	encode func(T) (EncodedValue, error)
	decode func(EncodedValue) (T, error)
}

func (c frameworkValueCodec[T]) Type() string { return c.name }
func (frameworkValueCodec[T]) Version() int   { return 1 }
func (c frameworkValueCodec[T]) Encode(value any) (json.RawMessage, error) {
	encoded, err := c.encode(value.(T))
	if err != nil {
		return nil, err
	}
	return json.Marshal(encoded)
}
func (c frameworkValueCodec[T]) Decode(data json.RawMessage) (any, error) {
	var encoded EncodedValue
	if err := json.Unmarshal(data, &encoded); err != nil {
		return nil, err
	}
	return c.decode(encoded)
}

func addFrameworkValueCodec[T any](r *CodecRegistry, encode func(T) (EncodedValue, error),
	decode func(EncodedValue) (T, error),
) {
	typ := reflect.TypeFor[T]()
	c := frameworkValueCodec[T]{name: typ.PkgPath() + "/" + typ.Name(), encode: encode, decode: decode}
	r.add(registeredValue{id: c.Type(), version: c.Version(), typ: typ, codec: c})
}

func registerToolStateCodecs(r *CodecRegistry) {
	for _, value := range []any{modelToolResponseData{}, toolCallRecordData{},
		ToolExecutionResponse{}, ToolRecoveryResponse{}, ToolSearchTrace{},
		ToolStatus(""), ToolResponseKind(""),
		ToolRecoveryStatus(""), SteeringMessages{}, PTCIntegerMode(0)} {
		typ := reflect.TypeOf(value)
		r.add(registeredValue{id: typ.PkgPath() + "/" + typ.Name(), version: 1, typ: typ})
	}
	addFrameworkValueCodec(r, func(value DataContent) (EncodedValue, error) {
		return r.Encode(value.value)
	}, func(value EncodedValue) (DataContent, error) {
		decoded, err := r.Decode(value)
		if err != nil {
			return DataContent{}, err
		}
		// Check portable content without constructing any original output Go type.
		if _, err := json.Marshal(decoded); err != nil {
			return DataContent{}, err
		}
		return DataContent{value: decoded}, nil
	})
	addFrameworkValueCodec(r, func(value ModelToolCallResponse) (EncodedValue, error) {
		return r.Encode(value.data)
	}, func(value EncodedValue) (ModelToolCallResponse, error) {
		data, err := decodeAs[modelToolResponseData](r, value)
		if err != nil {
			return ModelToolCallResponse{}, err
		}
		if err := validateSavedModelToolResponse(data); err != nil {
			return ModelToolCallResponse{}, err
		}
		return ModelToolCallResponse{data: data}, nil
	})
	addFrameworkValueCodec(r, func(value ToolCallRecord) (EncodedValue, error) {
		return r.Encode(value.data)
	}, func(value EncodedValue) (ToolCallRecord, error) {
		data, err := decodeAs[toolCallRecordData](r, value)
		if err != nil {
			return ToolCallRecord{}, err
		}
		if err := validateRecordOutcome(&data); err != nil {
			return ToolCallRecord{}, err
		}
		if data.Response == nil {
			return ToolCallRecord{}, errors.New("gent: saved reply is absent")
		}
		kind, execution, recovery := recordStatuses(&data)
		if got := data.Response.data; got.Kind != kind || got.ExecutionStatus != execution ||
			got.RecoveryStatus != recovery {
			return ToolCallRecord{}, errors.New("gent: saved reply metadata differs from its outcome")
		}
		return ToolCallRecord{data: data}, nil
	})
}

func validateSavedModelToolResponse(data modelToolResponseData) error {
	if data.ToolCallID == "" || data.Name == "" {
		return errors.New("gent: saved reply lacks identity")
	}
	execution := data.ExecutionStatus
	validExecution := execution == ToolExecuted || execution == ToolBlocked ||
		execution == ToolInvalidArguments
	valid := false
	switch data.Kind {
	case ToolResponseExecution:
		valid = validExecution && data.RecoveryStatus == ""
	case ToolResponseRecovery:
		valid = (data.RecoveryStatus == ToolRecoveryCompleted && validExecution) ||
			((data.RecoveryStatus == ToolRecoveryNotStarted ||
				data.RecoveryStatus == ToolRecoveryUnresolved) && execution == "")
	case ToolResponseCallError, ToolResponseRecoveryError:
		valid = execution == "" && data.RecoveryStatus == ""
	}
	if !valid {
		return errors.New("gent: invalid saved reply metadata")
	}
	return ValidateContentParts(data.Parts)
}
