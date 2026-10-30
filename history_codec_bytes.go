package gent

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// CodecLimits bounds a saved record and each decoded binary buffer. Zero means no limit.
// Custom codecs must enforce their own decoded allocation limits for application-specific data.
type CodecLimits struct {
	MaxEncodedBytes int
	MaxBinaryBytes  int
}

// CodecOption configures a registry before it is shared by executions.
type CodecOption func(*CodecRegistry)

func WithCodecLimits(limits CodecLimits) CodecOption {
	if limits.MaxEncodedBytes < 0 || limits.MaxBinaryBytes < 0 {
		panic("gent: codec limits must not be negative")
	}
	return func(r *CodecRegistry) { r.limits = limits }
}

func (r *CodecRegistry) checkEncodedSize(size int) error {
	if limit := r.limits.MaxEncodedBytes; limit > 0 && size > limit {
		return fmt.Errorf("gent: encoded value exceeds %d bytes", limit)
	}
	return nil
}

func (r *CodecRegistry) checkBinarySize(size int) error {
	if limit := r.limits.MaxBinaryBytes; limit > 0 && size > limit {
		return fmt.Errorf("gent: binary value exceeds %d bytes", limit)
	}
	return nil
}

func (r *CodecRegistry) decodeBytes(raw json.RawMessage) ([]byte, error) {
	raw = bytes.TrimSpace(raw)
	if bytes.Equal(raw, []byte("null")) {
		return nil, nil
	}
	if len(raw) == 0 || raw[0] != '"' {
		return nil, errors.New("gent: byte payload must be a base64 string or null")
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, err
	}
	if strings.ContainsAny(value, "\r\n") {
		return nil, errors.New("gent: byte payload must use canonical base64")
	}
	// Check the decoded size before allocating it. Padding removes up to two bytes from the
	// upper bound. Strict decoding then rejects invalid padding and nonzero trailing bits.
	size := base64.StdEncoding.DecodedLen(len(value))
	if len(value) > 0 && value[len(value)-1] == '=' {
		size--
	}
	if len(value) > 1 && value[len(value)-2] == '=' {
		size--
	}
	if err := r.checkBinarySize(size); err != nil {
		return nil, err
	}
	return base64.StdEncoding.Strict().DecodeString(value)
}
