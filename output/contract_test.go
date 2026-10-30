package output_test

import (
	"encoding/json"
	"testing"

	"github.com/rickchristie/gent"
	"github.com/rickchristie/gent/output"
	"github.com/stretchr/testify/assert"
)

func TestJsonOutputOmissionTagsPreserveValueAndDelivery(t *testing.T) {
	type record struct {
		Name    string `json:"display_name"`
		Count   int    `json:"count,omitzero"`
		Note    string `json:"note,omitempty"`
		Ignored string `json:"-"`
	}
	type input struct{ candidate string }
	type expected struct{ result *gent.OutputResult[record] }
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	cases := []testCase{
		{"absent optional fields", input{`{"display_name":"Ada"}`}, expected{
			&gent.OutputResult[record]{Accepted: true, Value: record{Name: "Ada"},
				Content: text(`{"display_name":"Ada"}`)}}},
		{"explicit zeros become canonical omissions",
			input{`{"display_name":"Ada","count":0,"note":""}`}, expected{
				&gent.OutputResult[record]{Accepted: true, Value: record{Name: "Ada"},
					Content: text(`{"display_name":"Ada"}`)}}},
		{"nonzero optional fields remain", input{`{"display_name":"Ada","count":2,"note":"ready"}`},
			expected{&gent.OutputResult[record]{Accepted: true,
				Value:   record{Name: "Ada", Count: 2, Note: "ready"},
				Content: text(`{"display_name":"Ada","count":2,"note":"ready"}`)}}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			out, err := output.NewJsonOutput[record]()
			if !assert.NoError(t, err) {
				return
			}
			request := gent.NewModelCallRequest(nil)
			assert.NoError(t, out.Configure(&request))
			actual, err := json.Marshal(request.Options.ResponseSchema)
			assert.NoError(t, err)
			assert.JSONEq(t, `{"type":"object","additionalProperties":false,
"required":["display_name"],"properties":{"display_name":{"type":"string"},
"count":{"type":"integer"},"note":{"type":"string"}}}`, string(actual))
			result, err := out.Process(execution(t), text(test.input.candidate))
			assert.NoError(t, err)
			assert.Equal(t, test.expected.result, result)
		})
	}
}

func mutableJSONValidator[T any](
	t *testing.T, candidate string, change func(T),
) func(*gent.ExecutionContext) (any, error) {
	t.Helper()
	out, err := output.NewJsonOutput[T]()
	if !assert.NoError(t, err) {
		t.FailNow()
	}
	out.SetCustomValidator("read-only", func(_ *gent.ExecutionContext, value T) (
		*gent.OutputValidationResult, error,
	) {
		change(value)
		return &gent.OutputValidationResult{Accepted: true}, nil
	})
	return func(ctx *gent.ExecutionContext) (any, error) {
		return out.Process(ctx, text(candidate))
	}
}

func TestCustomValidatorCannotChangeAcceptedValues(t *testing.T) {
	image := imageFile(t, "image/png")
	original := gent.CloneContentParts([]gent.ContentPart{image})
	imageOutput := output.NewImageOutput().SetCustomValidator("read-only",
		func(_ *gent.ExecutionContext, images []gent.BinaryContent) (
			*gent.OutputValidationResult, error,
		) {
			images[0].Data[0] = 99
			return &gent.OutputValidationResult{Accepted: true}, nil
		})
	type input struct {
		process func(*gent.ExecutionContext) (any, error)
	}
	type expected struct {
		err        string
		rejections int64
	}
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	want := expected{err: "output: custom validator must not change the value"}
	cases := []testCase{
		{"pointer", input{mutableJSONValidator(t, `{"id":1,"quantity":2}`,
			func(value *order) { value.Quantity = 3 })}, want},
		{"map", input{mutableJSONValidator(t, `{"quantity":2}`,
			func(value map[string]int) { value["quantity"] = 3 })}, want},
		{"slice", input{mutableJSONValidator(t, `[2]`, func(value []int) { value[0] = 3 })}, want},
		{"image bytes", input{func(ctx *gent.ExecutionContext) (any, error) {
			return imageOutput.Process(ctx, []gent.ContentPart{image})
		}}, want},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			ctx := execution(t)
			result, err := test.input.process(ctx)
			assert.Nil(t, result)
			assert.EqualError(t, err, test.expected.err)
			assert.Equal(t, test.expected.rejections,
				ctx.Stats().GetCounter(gent.SCOutputCustomRejectedBy+"read-only"))
			assert.Equal(t, original, []gent.ContentPart{image})
		})
	}
}
