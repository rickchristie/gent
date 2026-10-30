package output

import "github.com/rickchristie/gent"

// TextOutput joins text parts in their supplied order without adding separators. Delivery retains
// the individual parts. Literal answer tags are ordinary text and have no control meaning.
type TextOutput struct {
	validator customValidator[string]
}

func (o *TextOutput) RecoveryConfig() any {
	return map[string]any{"validator": o.validator.name}
}

func NewTextOutput() *TextOutput { return &TextOutput{} }

func (o *TextOutput) SetCustomValidator(
	name string, callback gent.OutputCustomValidator[string],
) *TextOutput {
	o.validator = customValidator[string]{name: name, call: callback}
	return o
}

func (*TextOutput) Instructions() string { return "Return the answer as text." }

func (o *TextOutput) Configure(request *gent.ModelCallRequest) error {
	if err := o.validator.check(); err != nil {
		return err
	}
	request.Options.ResponseSchema = nil
	request.Options.ResponseMIMEType = "text/plain"
	request.Options.ResponseModalities = []string{"text"}
	return nil
}

func (o *TextOutput) Process(
	ctx *gent.ExecutionContext, content []gent.ContentPart,
) (processed *gent.OutputResult[string], processErr error) {
	local := true
	defer func() {
		if local {
			publishLocalResult(ctx, "text", processed, processErr)
		}
	}()
	if err := o.validator.check(); err != nil {
		return nil, err
	}
	if err := ctx.Context().Err(); err != nil {
		return nil, err
	}
	value, owned, ok := textContent(content)
	if !ok {
		return reject[string]("The answer must contain text only. Return a text answer."), nil
	}
	local = false
	ctx.PublishOutputValidation(gent.OutputValidationLocal, "text", true, nil, nil)
	if result, err := o.validator.validate(ctx, value); result != nil || err != nil {
		return result, err
	}
	return &gent.OutputResult[string]{Accepted: true, Value: value, Content: owned}, nil
}
