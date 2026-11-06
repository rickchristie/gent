package gent

import (
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strings"
)

// TextPart supplies exact authored text. Gent does not parse it as JSON, even when it looks
// like structured data. Use DataPart when Gent must capture and encode a Go value.
func TextPart(text string) ContentPart { return TextContent{Text: text} }

// BytesPart supplies a file and its MIME type without conversion or format detection.
// The part takes ownership of data; do not modify the buffer after submission. Gent saves
// its bytes for recovery. Provider support is checked at delivery, with safe steering when
// unsupported. Direct tool responses include bytes; PTC requires same-script tool.forward.
func BytesPart(mimeType string, data []byte) ContentPart {
	return BinaryContent{MIMEType: mimeType, Data: data}
}

// ImagePart detects a supported image MIME type without changing the bytes. It returns an
// error for unknown formats. On success, the ownership and recovery rules of BytesPart apply.
func ImagePart(data []byte) (ContentPart, error) {
	mime := http.DetectContentType(data)
	switch mime {
	case "image/png", "image/jpeg", "image/gif", "image/webp":
		return BytesPart(mime, data), nil
	default:
		return nil, fmt.Errorf("gent: cannot detect a supported image format (%s)", mime)
	}
}

// PNGPart supplies PNG bytes. It does not validate or convert the file; see BytesPart ownership.
func PNGPart(data []byte) ContentPart { return BytesPart("image/png", data) }

// JPEGPart supplies JPEG bytes. It does not convert the file; see BytesPart ownership.
func JPEGPart(data []byte) ContentPart { return BytesPart("image/jpeg", data) }

// GIFPart supplies GIF bytes. Provider animation support can differ; see BytesPart ownership.
func GIFPart(data []byte) ContentPart { return BytesPart("image/gif", data) }

// WebPPart supplies WebP bytes without conversion. See BytesPart for ownership and delivery.
func WebPPart(data []byte) ContentPart { return BytesPart("image/webp", data) }

// PDFPart supplies a PDF file without extracting text. See BytesPart for ownership and delivery.
func PDFPart(data []byte) ContentPart { return BytesPart("application/pdf", data) }

// AudioPart supplies audio bytes with the caller's MIME type. It does not transcode audio.
// Provider support is checked at delivery. The ownership and recovery rules of BytesPart apply.
func AudioPart(mimeType string, data []byte) ContentPart { return BytesPart(mimeType, data) }

// VideoPart supplies video bytes with the caller's MIME type. It does not transcode video.
// Provider support is checked at delivery. The ownership and recovery rules of BytesPart apply.
func VideoPart(mimeType string, data []byte) ContentPart { return BytesPart(mimeType, data) }

// PartsBuilder captures ordered content for messages, tool responses, or recovery evidence.
// Data is captured when added. Byte buffers transfer ownership without a defensive copy.
// Build reports all construction errors together and returns no partial content on failure.
// A builder belongs to one goroutine and must not be changed after Build.
type PartsBuilder struct {
	parts  []ContentPart
	errors []error
}

// NewParts starts an empty content list. Provider compatibility is checked after construction.
func NewParts() *PartsBuilder { return &PartsBuilder{} }

// Text appends exact text, without JSON interpretation. See TextPart.
func (b *PartsBuilder) Text(value string) *PartsBuilder { return b.add(TextPart(value), nil) }

// Data captures a portable Go value now. Build reports unsupported values or cycles. See DataPart.
func (b *PartsBuilder) Data(value any) *PartsBuilder { return b.add(DataPart(value)) }

// ImageURL appends a remote reference. It does not promise stable or unexpired content.
// See ImageURLPart for URL ownership and load failures.
func (b *PartsBuilder) ImageURL(url string) *PartsBuilder { return b.add(ImageURLPart(url), nil) }

// Bytes appends owned bytes without conversion. See BytesPart for lifetime and provider limits.
func (b *PartsBuilder) Bytes(mime string, data []byte) *PartsBuilder {
	return b.add(BytesPart(mime, data), nil)
}

// Image detects the image format. Build reports detection failure. See ImagePart.
func (b *PartsBuilder) Image(data []byte) *PartsBuilder { return b.add(ImagePart(data)) }

// PNG appends PNG bytes without conversion or validation. See PNGPart.
func (b *PartsBuilder) PNG(data []byte) *PartsBuilder { return b.add(PNGPart(data), nil) }

// JPEG appends JPEG bytes without conversion or validation. See JPEGPart.
func (b *PartsBuilder) JPEG(data []byte) *PartsBuilder { return b.add(JPEGPart(data), nil) }

// GIF appends GIF bytes without conversion or validation. See GIFPart.
func (b *PartsBuilder) GIF(data []byte) *PartsBuilder { return b.add(GIFPart(data), nil) }

// WebP appends WebP bytes without conversion or validation. See WebPPart.
func (b *PartsBuilder) WebP(data []byte) *PartsBuilder { return b.add(WebPPart(data), nil) }

// PDF appends a PDF file without text extraction. See PDFPart.
func (b *PartsBuilder) PDF(data []byte) *PartsBuilder { return b.add(PDFPart(data), nil) }

// Audio appends audio bytes with their MIME type, without transcoding. See AudioPart.
func (b *PartsBuilder) Audio(mime string, data []byte) *PartsBuilder {
	return b.add(AudioPart(mime, data), nil)
}

// Video appends video bytes with their MIME type, without transcoding. See VideoPart.
func (b *PartsBuilder) Video(mime string, data []byte) *PartsBuilder {
	return b.add(VideoPart(mime, data), nil)
}

func (b *PartsBuilder) add(part ContentPart, err error) *PartsBuilder {
	if err != nil {
		b.errors = append(b.errors, err)
		return b
	}
	b.parts = append(b.parts, part)
	return b
}

// Build returns the complete ordered list, or one joined error and no list. It does not check
// provider media support or validate domain data against a tool's descriptive OutputSchema.
func (b *PartsBuilder) Build() ([]ContentPart, error) {
	if err := errors.Join(b.errors...); err != nil {
		return nil, err
	}
	return b.parts, nil
}

// ValidateContentParts rejects native call traffic inside tool content. Cached content retains
// the same restriction. This checks the framework contract, not the author's output schema.
func ValidateContentParts(parts []ContentPart) error {
	for i, part := range parts {
		if part == nil {
			return fmt.Errorf("gent: part %d is nil", i)
		}
		value := reflect.ValueOf(part)
		if value.Kind() == reflect.Pointer && value.IsNil() {
			return fmt.Errorf("gent: part %d is nil", i)
		}
		switch p := part.(type) {
		case TextContent, *TextContent, ImageURLContent, *ImageURLContent,
			BinaryContent, *BinaryContent, DataContent, *DataContent:
		case CachedContent:
			if err := ValidateContentParts([]ContentPart{p.ContentPart}); err != nil {
				return err
			}
		case *CachedContent:
			if err := ValidateContentParts([]ContentPart{p.ContentPart}); err != nil {
				return err
			}
		default:
			return fmt.Errorf("gent: part %d is not tool content (%T)", i, part)
		}
	}
	return nil
}

// ContentTextData renders text and structured data for text-only destinations and summaries.
// It does not flatten media. Callers must handle media separately instead of silently dropping it.
func ContentTextData(parts []ContentPart) string {
	var result []string
	for _, part := range parts {
		switch p := part.(type) {
		case TextContent:
			result = append(result, p.Text)
		case *TextContent:
			result = append(result, p.Text)
		case DataContent:
			result = append(result, p.String())
		case *DataContent:
			result = append(result, p.String())
		}
	}
	return strings.Join(result, "\n")
}
