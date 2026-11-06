package jsruntime

import (
	"context"
	"testing"

	"github.com/rickchristie/gent"
	"github.com/stretchr/testify/assert"
)

func TestBridgeJSONNumericMeaningAfterRestore(t *testing.T) {
	type Deep struct {
		Number float64 `json:"number"`
	}
	type First struct{ Deep }
	type Second struct {
		Number int64 `json:"number"`
	}
	type Domain struct {
		First
		Second
		Floats map[int]float64 `json:"floats"`
	}
	type input struct {
		mode    gent.PTCIntegerMode
		restore bool
	}
	type expected struct{ log string }
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	cases := []testCase{
		{"number", input{gent.PTCIntegerNumber, false}, expected{"number number"}},
		{"bigint", input{gent.PTCIntegerBigInt, false}, expected{"number bigint"}},
		{"restored number", input{gent.PTCIntegerNumber, true}, expected{"number number"}},
		{"restored bigint", input{gent.PTCIntegerBigInt, true}, expected{"number bigint"}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			part, err := gent.DataPart(Domain{First{Deep{1.5}}, Second{7}, map[int]float64{7: 2}})
			if !assert.NoError(t, err) {
				return
			}
			record := bridgeRecord(t, gent.ToolResponseExecution, &gent.ToolExecutionResponse{
				Status: gent.ToolExecuted, Parts: []gent.ContentPart{part}}, nil, []gent.ContentPart{part})
			if test.input.restore {
				encoded, err := gent.NewCodecRegistry().Marshal(record)
				if !assert.NoError(t, err) {
					return
				}
				var restored *gent.ToolCallRecord
				if !assert.NoError(t, gent.NewCodecRegistry().Unmarshal(encoded, &restored)) {
					return
				}
				assert.Equal(t, record, restored)
				record = restored
			}
			rt := New(DefaultConfig())
			var arguments string
			RegisterToolBridge(rt, func(calls []*gent.ModelToolCallRequest) (
				[]*gent.ToolCallRecord, error,
			) {
				if calls[0].FunctionCall.Name == "next" {
					arguments = calls[0].FunctionCall.Arguments
				}
				return []*gent.ToolCallRecord{record}, nil
			}, test.input.mode)
			increment := "1"
			if test.input.mode == gent.PTCIntegerBigInt {
				increment = "1n"
			}
			result, err := rt.Execute(context.Background(), `const d=tool.call({tool:"fetch",args:{}})
.parts[0].data;
console.log(typeof d.floats[7], typeof d.number);
tool.call({tool:"next",args:{float:d.floats[7]+0.5,integer:d.number+`+increment+`}});`)
			if !assert.NoError(t, err) {
				return
			}
			assert.Equal(t, []string{test.expected.log}, result.ConsoleLog)
			assert.Equal(t, `{"float":2.5,"integer":8}`, arguments)
		})
	}
}
