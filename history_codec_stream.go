package gent

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// A chunk has a fixed public shape. Encoding each scalar with a separate type envelope adds
// work to every token. Keep the error in the registry because application errors can use a
// custom codec. Content uses registry envelopes to preserve concrete part types and owned bytes.
type streamChunkCodec struct {
	registry *CodecRegistry
}

type savedStreamChunk struct {
	Chunk       *StreamChunk  `json:"chunk"`
	Error       *EncodedValue `json:"error,omitempty"`
	Content     *EncodedValue `json:"content,omitempty"`
	MetaContent *EncodedValue `json:"metaContent,omitempty"`
}

func (streamChunkCodec) Type() string { return "github.com/rickchristie/gent/StreamChunk" }
func (streamChunkCodec) Version() int { return 3 }

func (c streamChunkCodec) Encode(value any) (json.RawMessage, error) {
	chunk := value.(StreamChunk)
	wire := savedStreamChunk{Chunk: &chunk}
	if chunk.Content != nil {
		encoded, err := c.registry.Encode(chunk.Content)
		if err != nil {
			return nil, err
		}
		wire.Content = &encoded
		chunk.Content = nil
	}
	if chunk.MetaContent != nil {
		encoded, err := c.registry.Encode(chunk.MetaContent)
		if err != nil {
			return nil, err
		}
		wire.MetaContent = &encoded
		chunk.MetaContent = nil
	}
	if wire.Chunk.Err != nil {
		encoded, err := c.registry.Encode(wire.Chunk.Err)
		if err != nil {
			return nil, err
		}
		wire.Error = &encoded
		wire.Chunk.Err = nil
	}
	return json.Marshal(wire)
}

func (c streamChunkCodec) Decode(data json.RawMessage) (any, error) {
	var wire savedStreamChunk
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&wire); err != nil {
		return nil, err
	}
	if wire.Chunk == nil {
		return nil, fmt.Errorf("gent: missing saved stream chunk")
	}
	if wire.Content != nil {
		parts, err := decodeAs[[]ContentPart](c.registry, *wire.Content)
		if err != nil {
			return nil, err
		}
		wire.Chunk.Content = parts
	}
	if wire.MetaContent != nil {
		meta, err := decodeAs[[]*MetaContent](c.registry, *wire.MetaContent)
		if err != nil {
			return nil, err
		}
		wire.Chunk.MetaContent = meta
	}
	if wire.Error != nil {
		value, err := c.registry.Decode(*wire.Error)
		if err != nil {
			return nil, err
		}
		failure, ok := value.(error)
		if !ok {
			return nil, fmt.Errorf("gent: stream error decoded as %T", value)
		}
		wire.Chunk.Err = failure
	}
	return *wire.Chunk, nil
}
