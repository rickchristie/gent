package models

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"

	"github.com/rickchristie/gent"
)

const maxOutputMediaBytes = 32 << 20

// Only provider output references are resolved here. Input URLs stay URLs. The model response
// owns the resulting bytes, so checkpoint restore never depends on a temporary download URL.
func resolveOutputImage(ctx context.Context, client *http.Client, reference string) (
	gent.BinaryContent, error,
) {
	if strings.HasPrefix(reference, "data:") {
		header, encoded, ok := strings.Cut(reference[5:], ",")
		if !ok || !strings.HasSuffix(header, ";base64") {
			return gent.BinaryContent{}, errors.New("output image requires a base64 data URL")
		}
		mimeType := strings.TrimSuffix(header, ";base64")
		return decodeOutputImage(mimeType, encoded)
	}
	parsed, err := url.Parse(reference)
	if err != nil {
		return gent.BinaryContent{}, err
	}
	if (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.Host == "" ||
		parsed.User != nil {
		return gent.BinaryContent{}, errors.New("invalid output image URL")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, reference, nil)
	if err != nil {
		return gent.BinaryContent{}, err
	}
	// Never copy the model API authorization header to an image host.
	response, err := client.Do(request)
	if err != nil {
		return gent.BinaryContent{}, err
	}
	readErr := checkHTTPResponse(response)
	var data []byte
	if readErr == nil {
		data, readErr = io.ReadAll(io.LimitReader(response.Body, maxOutputMediaBytes+1))
	}
	if err := errors.Join(readErr, response.Body.Close()); err != nil {
		return gent.BinaryContent{}, err
	}
	if len(data) > maxOutputMediaBytes {
		return gent.BinaryContent{}, errors.New("output image exceeds 32 MiB")
	}
	mimeType := response.Header.Get("Content-Type")
	if mimeType == "" || mimeType == "application/octet-stream" {
		mimeType = http.DetectContentType(data)
	}
	mimeType, _, err = mime.ParseMediaType(mimeType)
	if err != nil {
		return gent.BinaryContent{}, err
	}
	if !strings.HasPrefix(mimeType, "image/") {
		return gent.BinaryContent{}, fmt.Errorf("output reference returned %q", mimeType)
	}
	return gent.BinaryContent{MIMEType: mimeType, Data: data}, nil
}

func decodeOutputImage(mimeType, encoded string) (gent.BinaryContent, error) {
	if !strings.HasPrefix(mimeType, "image/") {
		return gent.BinaryContent{}, fmt.Errorf("unsupported output media %q", mimeType)
	}
	if base64.StdEncoding.DecodedLen(len(encoded)) > maxOutputMediaBytes+2 {
		return gent.BinaryContent{}, errors.New("output image exceeds 32 MiB")
	}
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return gent.BinaryContent{}, err
	}
	if len(data) > maxOutputMediaBytes {
		return gent.BinaryContent{}, errors.New("output image exceeds 32 MiB")
	}
	return gent.BinaryContent{MIMEType: mimeType, Data: data}, nil
}
