package react

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/rickchristie/gent"
	"github.com/rickchristie/gent/format"
	"github.com/rickchristie/gent/termination"
	"github.com/rickchristie/gent/toolchain"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ----------------------------------------------------------------------------
// Mock Model for testing
// ----------------------------------------------------------------------------

type mockModel struct {
	responses        []*gent.ContentResponse
	errors           []error
	callCount        int
	capturedOptions  [][]gent.CallOption
	capturedMessages [][]gent.ModelMessage
}

func newMockModel(responses ...*gent.ContentResponse) *mockModel {
	return &mockModel{responses: responses}
}

func (m *mockModel) WithErrors(errs ...error) *mockModel {
	m.errors = errs
	return m
}

func (m *mockModel) response(request *gent.ModelCallRequest) (*gent.ContentResponse, error) {
	m.capturedMessages = append(m.capturedMessages, request.Messages)
	options := []gent.CallOption{gent.WithOptions(&request.Options)}
	m.capturedOptions = append(m.capturedOptions, options)
	idx := m.callCount
	m.callCount++

	if idx < len(m.errors) && m.errors[idx] != nil {
		return nil, m.errors[idx]
	}

	if idx < len(m.responses) {
		return m.responses[idx], nil
	}

	return &gent.ContentResponse{Choices: []*gent.ContentChoice{{CandidateContent: ""}}}, nil
}

func (m *mockModel) GenerateContentStream(
	_ context.Context, request gent.ModelCallRequest,
) (gent.Stream, error) {
	resp, err := m.response(&request)
	if err != nil {
		return gent.NewCompletedStream(nil, err), nil
	}
	return gent.NewCompletedStream(resp, nil), nil
}

// ----------------------------------------------------------------------------
// Mock ToolChain for testing
// ----------------------------------------------------------------------------

func newMockToolChain() *toolchain.Registry { return toolchain.New() }

// ----------------------------------------------------------------------------
// Helper to create ExecutionContext for tests
// ----------------------------------------------------------------------------

func newTestExecCtx(data gent.LoopData) *gent.ExecutionContext {
	return gent.NewExecutionContext(context.Background(), "test", data)
}

// ----------------------------------------------------------------------------
// Agent Tests
// ----------------------------------------------------------------------------

func TestAgent_Next_ModelError(t *testing.T) {
	model := newMockModel().WithErrors(errors.New("model failed"))
	format := format.NewXML()
	tc := newMockToolChain()
	term := termination.NewText("answer")

	loop := NewAgent(model).
		WithFormat(format).
		WithToolChain(tc).
		WithTermination(term)

	data := gent.NewBasicLoopData(&gent.Task{Text: "Hello"})
	execCtx := newTestExecCtx(data)
	_, err := loop.Next(execCtx)

	require.Error(t, err)
	assert.EqualError(t, err, "model call failed: model failed")
}

func TestAgent_WithTimeProvider(t *testing.T) {
	model := newMockModel()
	mockTime := gent.NewMockTimeProvider(time.Date(2025, 6, 15, 14, 30, 0, 0, time.UTC))

	loop := NewAgent(model).WithTimeProvider(mockTime)

	assert.Equal(t, mockTime, loop.TimeProvider())
	assert.Equal(t, "2025-06-15", loop.TimeProvider().Today())
	assert.Equal(t, "Sunday", loop.TimeProvider().Weekday())
}

// mockNamerModel implements Model with ModelNamer for
// testing model-specific sampling behavior.
type mockNamerModel struct {
	mockModel
	name string
}

func (m *mockNamerModel) ModelName() string {
	return m.name
}

func TestAgent_WithCallOptions(t *testing.T) {
	// The ReAct agent prepends DefaultSamplingParams before
	// user options. mockModel does not implement ModelNamer,
	// so defaults are temp=0.2, top-p=1.0. User options
	// applied after override the defaults.

	type input struct {
		callOptions []gent.CallOption
	}

	type expected struct {
		temperature float64
		topP        float64
		maxTokens   int
	}

	tests := []struct {
		name     string
		input    input
		expected expected
	}{
		{
			name: "no user options uses default " +
				"temperature and top-p",
			input: input{
				callOptions: nil,
			},
			expected: expected{
				temperature: 0.2,
				topP:        1.0,
			},
		},
		{
			name: "user temperature overrides " +
				"default",
			input: input{
				callOptions: []gent.CallOption{
					gent.WithTemperature(0.7),
				},
			},
			expected: expected{
				temperature: 0.7,
				topP:        1.0,
			},
		},
		{
			name: "user top-p overrides default",
			input: input{
				callOptions: []gent.CallOption{
					gent.WithTopP(0.9),
				},
			},
			expected: expected{
				temperature: 0.2,
				topP:        0.9,
			},
		},
		{
			name: "user overrides both temperature " +
				"and top-p",
			input: input{
				callOptions: []gent.CallOption{
					gent.WithTemperature(0.5),
					gent.WithTopP(0.8),
					gent.WithMaxTokens(512),
				},
			},
			expected: expected{
				temperature: 0.5,
				topP:        0.8,
				maxTokens:   512,
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			model := newMockModel(
				&gent.ContentResponse{
					Choices: []*gent.ContentChoice{
						{CandidateContent: "done"},
					},
				},
			)

			format := format.NewXML()
			term := termination.NewText("answer")

			agent := NewAgent(model).
				WithFormat(format).
				WithToolChain(newMockToolChain()).
				WithTermination(term)

			if tc.input.callOptions != nil {
				agent.WithCallOptions(
					tc.input.callOptions...,
				)
			}

			data := gent.NewBasicLoopData(
				&gent.Task{Text: "test"},
			)
			execCtx := gent.NewExecutionContext(
				context.Background(), "test", data,
			)
			execCtx.SetLimits(nil)

			_, err := agent.Next(execCtx)
			require.NoError(t, err)
			require.Len(t, model.capturedOptions, 1)

			captured := model.capturedOptions[0]
			var opts gent.CallOptions
			for _, opt := range captured {
				opt(&opts)
			}
			assert.InDelta(t,
				tc.expected.temperature,
				samplingValue(opts.Temperature), 0.001,
				"temperature",
			)
			assert.InDelta(t,
				tc.expected.topP,
				samplingValue(opts.TopP), 0.001,
				"top-p",
			)
			if tc.expected.maxTokens > 0 {
				assert.Equal(t,
					tc.expected.maxTokens,
					opts.MaxTokens,
				)
			}
		})
	}
}

func TestAgent_SamplingParams_ForbiddenModel(
	t *testing.T,
) {
	// Reasoning models omit sampling parameters unless the user sets them.

	type input struct {
		modelName   string
		callOptions []gent.CallOption
	}

	type expected struct {
		temperature float64
		topP        float64
	}

	tests := []struct {
		name     string
		input    input
		expected expected
	}{
		{
			name: "reasoning model omits forbidden params",
			input: input{
				modelName:   "openai/o3",
				callOptions: nil,
			},
			expected: expected{
				temperature: 0,
				topP:        0,
			},
		},
		{
			name: "user can still force params " +
				"on forbidden model",
			input: input{
				modelName: "o4-mini",
				callOptions: []gent.CallOption{
					gent.WithTemperature(0.5),
				},
			},
			expected: expected{
				temperature: 0.5,
				topP:        0,
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			inner := newMockModel(
				&gent.ContentResponse{
					Choices: []*gent.ContentChoice{
						{CandidateContent: "done"},
					},
				},
			)
			model := &mockNamerModel{
				mockModel: *inner,
				name:      tc.input.modelName,
			}

			format := format.NewXML()

			agent := NewAgent(model).
				WithFormat(format).
				WithToolChain(newMockToolChain()).
				WithTermination(termination.NewText("answer"))

			if tc.input.callOptions != nil {
				agent.WithCallOptions(
					tc.input.callOptions...,
				)
			}

			data := gent.NewBasicLoopData(
				&gent.Task{Text: "test"},
			)
			execCtx := gent.NewExecutionContext(
				context.Background(), "test", data,
			)
			execCtx.SetLimits(nil)

			_, err := agent.Next(execCtx)
			require.NoError(t, err)
			require.Len(t, model.capturedOptions, 1)

			captured := model.capturedOptions[0]
			var opts gent.CallOptions
			for _, opt := range captured {
				opt(&opts)
			}
			assert.InDelta(t,
				tc.expected.temperature,
				samplingValue(opts.Temperature), 0.001,
				"temperature",
			)
			assert.InDelta(t,
				tc.expected.topP,
				samplingValue(opts.TopP), 0.001,
				"top-p",
			)
		})
	}
}

func TestAgent_SamplingWarnings(t *testing.T) {
	type input struct {
		modelName   string
		callOptions []gent.CallOption
	}

	type expected struct {
		restrictiveWarning bool
		forbiddenWarnings  int
	}

	tests := []struct {
		name     string
		input    input
		expected expected
	}{
		{
			name: "restrictive temp and top-p " +
				"emits warning",
			input: input{
				callOptions: []gent.CallOption{
					gent.WithTemperature(0.1),
					gent.WithTopP(0.5),
				},
			},
			expected: expected{
				restrictiveWarning: true,
			},
		},
		{
			name: "restrictive temp only does " +
				"not warn",
			input: input{
				callOptions: []gent.CallOption{
					gent.WithTemperature(0.1),
				},
			},
			expected: expected{
				restrictiveWarning: false,
			},
		},
		{
			name: "restrictive top-p with normal " +
				"temp does not warn",
			input: input{
				callOptions: []gent.CallOption{
					gent.WithTemperature(0.5),
					gent.WithTopP(0.5),
				},
			},
			expected: expected{
				restrictiveWarning: false,
			},
		},
		{
			name: "normal values do not warn",
			input: input{
				callOptions: nil,
			},
			expected: expected{
				restrictiveWarning: false,
			},
		},
		{
			name: "forbidden model with user " +
				"temp emits forbidden warning",
			input: input{
				modelName: "openai/o3",
				callOptions: []gent.CallOption{
					gent.WithTemperature(0.5),
				},
			},
			expected: expected{
				forbiddenWarnings: 1,
			},
		},
		{
			name: "forbidden model with user " +
				"temp and top-p emits two " +
				"forbidden warnings",
			input: input{
				modelName: "o4-mini",
				callOptions: []gent.CallOption{
					gent.WithTemperature(0.5),
					gent.WithTopP(0.9),
				},
			},
			expected: expected{
				forbiddenWarnings: 2,
			},
		},
		{
			name: "forbidden model without " +
				"user params emits no warnings",
			input: input{
				modelName:   "gpt-5",
				callOptions: nil,
			},
			expected: expected{
				forbiddenWarnings: 0,
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var model gent.Model
			if tc.input.modelName != "" {
				inner := newMockModel(
					&gent.ContentResponse{
						Choices: []*gent.ContentChoice{
							{CandidateContent: "ok"},
						},
					},
				)
				model = &mockNamerModel{
					mockModel: *inner,
					name:      tc.input.modelName,
				}
			} else {
				model = newMockModel(
					&gent.ContentResponse{
						Choices: []*gent.ContentChoice{
							{CandidateContent: "ok"},
						},
					},
				)
			}

			format := format.NewXML()

			agent := NewAgent(model).
				WithFormat(format).
				WithToolChain(newMockToolChain()).
				WithTermination(termination.NewText("answer"))

			if tc.input.callOptions != nil {
				agent.WithCallOptions(
					tc.input.callOptions...,
				)
			}

			data := gent.NewBasicLoopData(
				&gent.Task{Text: "test"},
			)
			execCtx := gent.NewExecutionContext(
				context.Background(), "test", data,
			)
			execCtx.SetLimits(nil)

			_, err := agent.Next(execCtx)
			require.NoError(t, err)

			// Count warning events by name.
			var restrictive, forbidden int
			for _, evt := range execCtx.Events() {
				ce, ok := evt.(*gent.CommonEvent)
				if !ok {
					continue
				}
				switch ce.EventName {
				case "gent:sampling_params_restrictive":
					restrictive++
				case "gent:sampling_param_forbidden":
					forbidden++
				}
			}

			assert.Equal(t,
				tc.expected.restrictiveWarning,
				restrictive > 0,
				"restrictive warning",
			)
			assert.Equal(t,
				tc.expected.forbiddenWarnings,
				forbidden,
				"forbidden warnings",
			)
		})
	}
}

func samplingValue(value *float64) float64 {
	if value == nil {
		return 0
	}
	return *value
}
