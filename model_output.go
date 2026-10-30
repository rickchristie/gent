package gent

import (
	"bytes"
	"encoding/json"
	"errors"
)

// The request owns output controls before hooks run. Capture only these controls so hooks can
// change sampling, messages, and tools without weakening the executable output contract.
func captureOutputControls(options CallOptions) ([]byte, error) {
	if options.ResponseSchema == nil && options.ResponseMIMEType == "" &&
		len(options.ResponseModalities) == 0 && !options.JSONMode &&
		options.ImageMaxWidth == 0 && options.ImageMaxHeight == 0 &&
		options.ImageMaxFileSizeBytes == 0 && len(options.ImageMIMETypes) == 0 {
		return nil, nil
	}
	return json.Marshal(struct {
		Schema                map[string]any
		MIMEType              string
		Modalities            []string
		JSONMode              bool
		ImageMaxWidth         int
		ImageMaxHeight        int
		ImageMaxFileSizeBytes int64
		ImageMIMETypes        []string
	}{options.ResponseSchema, options.ResponseMIMEType, options.ResponseModalities,
		options.JSONMode, options.ImageMaxWidth, options.ImageMaxHeight,
		options.ImageMaxFileSizeBytes, options.ImageMIMETypes})
}

func checkOutputControls(expected []byte, options CallOptions) error {
	if expected == nil {
		return nil
	}
	actual, err := captureOutputControls(options)
	if err != nil {
		return err
	}
	if !bytes.Equal(expected, actual) {
		return errors.New("gent: model request hook changed required output controls")
	}
	return nil
}
