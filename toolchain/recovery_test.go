package toolchain

import (
	"context"
	"testing"

	"github.com/rickchristie/gent"
	"github.com/stretchr/testify/assert"
)

func TestToolRecoveryRouting(t *testing.T) {
	type input struct{ name string }
	type expected struct {
		response *gent.ToolRecoveryResponse
		err      string
	}
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	cases := []testCase{
		{"first tool", input{"book"}, expected{&gent.ToolRecoveryResponse{
			Result: gent.ToolRecoveryUnresolved, SteeringMessage: "Check the booking service."}, ""}},
		{"second tool", input{"refund"}, expected{&gent.ToolRecoveryResponse{
			Result: gent.ToolRecoveryUnresolved, SteeringMessage: "Check the payment service."}, ""}},
		{"default recovery", input{"lookup"}, expected{&gent.ToolRecoveryResponse{
			Result: gent.ToolRecoveryUnresolved}, ""}},
		{"unknown tool", input{"missing"}, expected{nil, `cannot recover unknown tool "missing"`}},
	}
	type chainCase struct {
		name string
		new  func() gent.ToolChain
	}
	chains := []chainCase{
		{"Registry", func() gent.ToolChain { return New() }},
		{"SearchChain", func() gent.ToolChain {
			return NewSearch(SearchHintSimpleList).RegisterEngine(NewRegexToolSearcher())
		}},
	}
	for _, chainTest := range chains {
		t.Run(chainTest.name, func(t *testing.T) {
			chain := chainTest.new()
			messages := map[string]string{
				"book": "Check the booking service.", "refund": "Check the payment service.",
			}
			for _, name := range []string{"book", "refund", "lookup"} {
				builder := gent.DefineTool(name, func(context.Context, struct{}) (string, error) {
					return "saved", nil
				}).Description("Read saved data").Domain("records")
				if message := messages[name]; message != "" {
					builder.Recovery(func(_ context.Context, request *gent.ToolRecoveryRequest) (
						*gent.ToolRecoveryResponse, error,
					) {
						assert.Equal(t, name, request.Name)
						return &gent.ToolRecoveryResponse{Result: gent.ToolRecoveryUnresolved,
							SteeringMessage: message}, nil
					})
				}
				chain.RegisterTool(builder.MustBuild())
			}
			wrapper := NewJsToolChainWrapper(chain)
			for _, test := range cases {
				t.Run(test.name, func(t *testing.T) {
					for _, target := range []gent.ToolChain{chain, wrapper} {
						response, err := target.Recover(context.Background(),
							&gent.ToolRecoveryRequest{Name: test.input.name})
						errText := ""
						if err != nil {
							errText = err.Error()
						}
						assert.Equal(t, test.expected, expected{response, errText})
					}
				})
			}
			response, err := wrapper.Recover(context.Background(),
				&gent.ToolRecoveryRequest{Name: "run_code"})
			assert.NoError(t, err)
			assert.Equal(t, &gent.ToolRecoveryResponse{Result: gent.ToolRecoveryUnresolved,
				SteeringMessage: "Review the partial results and make a new plan."}, response)
		})
	}
}
