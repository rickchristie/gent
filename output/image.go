package output

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"slices"
	"strings"

	"github.com/rickchristie/gent"
)

// ImageOutput validates complete PNG and JPEG files and returns all images in order. It does not
// resize or repair images. The default decoded pixel limit bounds decoder memory use even when a
// very small compressed file declares large dimensions. Configuration is fixed before use.
type ImageOutput struct {
	maxFileSizeBytes int64
	maxWidth         int
	maxHeight        int
	maxDecodedPixels uint64
	allowedMIMETypes []string
	validator        customValidator[[]gent.BinaryContent]
}

func (o *ImageOutput) RecoveryConfig() any {
	return map[string]any{"maxFileSizeBytes": o.maxFileSizeBytes, "maxWidth": o.maxWidth,
		"maxHeight": o.maxHeight, "maxDecodedPixels": o.maxDecodedPixels,
		"allowedMIMETypes": slices.Clone(o.allowedMIMETypes), "validator": o.validator.name}
}

func NewImageOutput() *ImageOutput {
	return &ImageOutput{maxDecodedPixels: 64 << 20}
}

func (o *ImageOutput) SetMaxFileSizeBytes(value int64) *ImageOutput {
	o.maxFileSizeBytes = value
	return o
}

func (o *ImageOutput) SetMaxWidth(value int) *ImageOutput {
	o.maxWidth = value
	return o
}

func (o *ImageOutput) SetMaxHeight(value int) *ImageOutput {
	o.maxHeight = value
	return o
}

// SetMaxDecodedPixels bounds pixels per image before full decoding. Zero disables this resource
// limit. Width and height constraints remain separate and are always checked when configured.
func (o *ImageOutput) SetMaxDecodedPixels(value uint64) *ImageOutput {
	o.maxDecodedPixels = value
	return o
}

func (o *ImageOutput) SetAllowedMIMETypes(values ...string) *ImageOutput {
	o.allowedMIMETypes = slices.Clone(values)
	return o
}

func (o *ImageOutput) SetCustomValidator(
	name string, callback gent.OutputCustomValidator[[]gent.BinaryContent],
) *ImageOutput {
	o.validator = customValidator[[]gent.BinaryContent]{name: name, call: callback}
	return o
}

func (o *ImageOutput) Instructions() string {
	parts := []string{"Return complete PNG or JPEG images."}
	if o.maxFileSizeBytes > 0 {
		parts = append(parts, fmt.Sprintf("Each file must be at most %d bytes.", o.maxFileSizeBytes))
	}
	if o.maxWidth > 0 {
		parts = append(parts, fmt.Sprintf("Each image must be at most %d pixels wide.", o.maxWidth))
	}
	if o.maxHeight > 0 {
		parts = append(parts, fmt.Sprintf("Each image must be at most %d pixels high.", o.maxHeight))
	}
	if len(o.allowedMIMETypes) > 0 {
		parts = append(parts, "Allowed MIME types: "+strings.Join(o.allowedMIMETypes, ", ")+".")
	}
	return strings.Join(parts, " ")
}

func (o *ImageOutput) check() error {
	if o.maxFileSizeBytes < 0 || o.maxWidth < 0 || o.maxHeight < 0 {
		return errors.New("output: image limits must not be negative")
	}
	for _, mime := range o.allowedMIMETypes {
		if mime != "image/png" && mime != "image/jpeg" {
			return fmt.Errorf("output: unsupported image MIME type %q", mime)
		}
	}
	return o.validator.check()
}

func (o *ImageOutput) Configure(request *gent.ModelCallRequest) error {
	if err := o.check(); err != nil {
		return err
	}
	request.Options.ResponseSchema = nil
	request.Options.ResponseMIMEType = ""
	request.Options.ResponseModalities = []string{"image"}
	request.Options.ImageMaxFileSizeBytes = o.maxFileSizeBytes
	request.Options.ImageMaxWidth = o.maxWidth
	request.Options.ImageMaxHeight = o.maxHeight
	request.Options.ImageMIMETypes = slices.Clone(o.allowedMIMETypes)
	if len(request.Options.ImageMIMETypes) == 0 {
		request.Options.ImageMIMETypes = []string{"image/png", "image/jpeg"}
	}
	return nil
}

func (o *ImageOutput) Process(
	ctx *gent.ExecutionContext, content []gent.ContentPart,
) (processed *gent.OutputResult[[]gent.BinaryContent], processErr error) {
	local := true
	defer func() {
		if local {
			publishLocalResult(ctx, "image", processed, processErr)
		}
	}()
	if err := o.check(); err != nil {
		return nil, err
	}
	if err := ctx.Context().Err(); err != nil {
		return nil, err
	}
	if len(content) == 0 {
		return reject[[]gent.BinaryContent]("No images were returned. Return at least one image."), nil
	}
	images := make([]gent.BinaryContent, 0, len(content))
	for i, part := range content {
		var value gent.BinaryContent
		switch p := part.(type) {
		case gent.BinaryContent:
			value = p
		case *gent.BinaryContent:
			value = *p
		default:
			return reject[[]gent.BinaryContent](fmt.Sprintf(
				"Answer part %d is not image bytes. Return only complete image files.", i+1)), nil
		}
		mime, steering, err := o.validateImage(ctx.Context(), i+1, value)
		if err != nil {
			return nil, err
		}
		if steering != "" {
			return reject[[]gent.BinaryContent](steering), nil
		}
		images = append(images, gent.BinaryContent{MIMEType: mime, Data: bytes.Clone(value.Data)})
	}
	local = false
	ctx.PublishOutputValidation(gent.OutputValidationLocal, "image", true, nil, nil)
	if result, err := o.validator.validate(ctx, images); result != nil || err != nil {
		return result, err
	}
	delivery := make([]gent.ContentPart, len(images))
	for i, value := range images {
		delivery[i] = gent.BinaryContent{MIMEType: value.MIMEType, Data: bytes.Clone(value.Data)}
	}
	return &gent.OutputResult[[]gent.BinaryContent]{
		Accepted: true, Value: images, Content: delivery,
	}, nil
}

func (o *ImageOutput) validateImage(
	ctx context.Context, index int, value gent.BinaryContent,
) (mime, steering string, err error) {
	if err := ctx.Err(); err != nil {
		return "", "", err
	}
	if limit := o.maxFileSizeBytes; limit > 0 && int64(len(value.Data)) > limit {
		return "", fmt.Sprintf(
			"Image %d is %d bytes. The maximum is %d. Return a smaller image file.",
			index, len(value.Data), limit), nil
	}
	config, format, decodeErr := image.DecodeConfig(contextReader{
		ctx: ctx, reader: bytes.NewReader(value.Data),
	})
	if err := ctx.Err(); err != nil {
		return "", "", err
	}
	if decodeErr != nil || config.Width <= 0 || config.Height <= 0 {
		return "", invalidImage(index), nil
	}
	mime = "image/" + format
	if mime != "image/png" && mime != "image/jpeg" {
		return "", invalidImage(index), nil
	}
	if value.MIMEType != "" && value.MIMEType != mime {
		return "", fmt.Sprintf("Image %d declares %s but contains %s. Return matching image data.",
			index, value.MIMEType, mime), nil
	}
	if len(o.allowedMIMETypes) > 0 && !slices.Contains(o.allowedMIMETypes, mime) {
		return "", fmt.Sprintf("Image %d is %s. Return one of these types: %s.",
			index, mime, strings.Join(o.allowedMIMETypes, ", ")), nil
	}
	if o.maxWidth > 0 && config.Width > o.maxWidth {
		return "", fmt.Sprintf(
			"Image %d is %d pixels wide. The maximum is %d. Return an image no wider than %d pixels.",
			index, config.Width, o.maxWidth, o.maxWidth), nil
	}
	if o.maxHeight > 0 && config.Height > o.maxHeight {
		return "", fmt.Sprintf(
			"Image %d is %d pixels high. The maximum is %d. Return an image no higher than %d pixels.",
			index, config.Height, o.maxHeight, o.maxHeight), nil
	}
	if limit := o.maxDecodedPixels; limit > 0 && uint64(config.Height) > limit/uint64(config.Width) {
		return "", fmt.Sprintf(
			"Image %d exceeds the decoder limit of %d pixels. Return a smaller image.", index, limit), nil
	}
	_, _, decodeErr = image.Decode(contextReader{ctx: ctx, reader: bytes.NewReader(value.Data)})
	if err := ctx.Err(); err != nil {
		return "", "", err
	}
	if decodeErr != nil {
		return "", invalidImage(index), nil
	}
	return mime, "", nil
}

func invalidImage(index int) string {
	return fmt.Sprintf("Image %d is not a valid PNG or JPEG file. Return a complete image.", index)
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}
