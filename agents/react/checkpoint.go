package react

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"

	"github.com/rickchristie/gent"
	agentutil "github.com/rickchristie/gent/agents"
)

type iterationFrame struct {
	Iteration      int
	Response       *gent.ContentResponse
	Repetition     *agentutil.RepetitionResult
	ToolPlanActive bool
}

func (r *Agent) Next(ctx *gent.ExecutionContext) (*gent.AgentLoopResult, error) {
	result, err := r.next(ctx)
	if err == nil {
		ctx.ClearCheckpointState("gent:react_frame", "gent:tool_plan", "gent:search_seen")
	}
	return result, err
}

// WithRecoveryVersion identifies application behavior that cannot be derived from declarative
// configuration, including custom prompts, clocks, hooks, validators, and tool implementations.
func (r *Agent) WithRecoveryVersion(version string) *Agent {
	r.recoveryVersion = version
	return r
}

func (r *Agent) RegisterCodecs(registry *gent.CodecRegistry) error {
	if err := registry.RegisterValueType(reflect.TypeOf(iterationFrame{})); err != nil {
		return err
	}
	if codecs, ok := r.termination.(interface {
		RegisterCodecs(*gent.CodecRegistry) error
	}); ok {
		if err := codecs.RegisterCodecs(registry); err != nil {
			return err
		}
	}
	return r.toolChain.RegisterCodecs(registry)
}

func (r *Agent) ConfigureExecution(ctx *gent.ExecutionContext) error {
	if err := r.RegisterCodecs(ctx.Codecs()); err != nil {
		return err
	}
	if ctx.Parent() != nil {
		return nil
	}
	request := gent.NewModelCallRequest(nil, r.callOptions...)
	options, err := ctx.Codecs().Encode(request.Options)
	if err != nil {
		return err
	}
	configuration := map[string]any{
		"version": 2, "application": r.recoveryVersion, "behavior": r.behaviorAndContext,
		"rules":      r.criticalRules,
		"formatType": fmt.Sprintf("%T", r.format), "tools": r.toolChain.RecoveryConfig(),
		"toolchainType": fmt.Sprintf("%T", r.toolChain), "termination": r.termination.Guidance(),
		"terminationType": fmt.Sprintf(
			"%T",
			r.termination,
		), "modelType": fmt.Sprintf(
			"%T",
			r.model,
		),
		"options": options, "maxResponseChars": r.maxResponseChars,
		"tooLongMessage": r.responseTooLongMessage,
		"repetition":     r.repetitionConfiguration(),
	}

	if configured, ok := r.model.(gent.RecoveryConfig); ok {
		configuration["modelConfig"] = configured.RecoveryConfig()
	}
	if named, ok := r.model.(interface{ ModelName() string }); ok {
		configuration["model"] = named.ModelName()
	}
	if provider, ok := r.model.(interface{ Provider() string }); ok {
		configuration["provider"] = provider.Provider()
	}
	data, err := json.Marshal(configuration)
	if err != nil {
		return err
	}
	digest := sha256.Sum256(data)
	manifest := ctx.Compatibility()
	manifest.Version = 1
	if manifest.Components == nil {
		manifest.Components = map[string]string{}
	}
	manifest.Components["gent:react"] = hex.EncodeToString(digest[:])
	manifest.Components["gent:recovery"] = "supported"
	if r.recoveryVersion == "" {
		// Prompt/schema hashes cannot identify changes inside application tool or validator code.
		manifest.Components["gent:recovery"] = "unsupported"
	}
	return ctx.SetCompatibility(manifest)
}

func (r *Agent) repetitionConfiguration() map[string]any {
	configuration := map[string]any{}
	value := reflect.ValueOf(r.repetitionConfig)
	for i := 0; i < value.NumField(); i++ {
		// The filter implementation is covered by WithRecoveryVersion, like other application code.
		if value.Field(i).Kind() != reflect.Func {
			configuration[value.Type().Field(i).Name] = value.Field(i).Interface()
		}
	}
	return configuration
}

func (r *Agent) loadFrame(ctx *gent.ExecutionContext) (*iterationFrame, error) {
	if err := r.RegisterCodecs(ctx.Codecs()); err != nil {
		return nil, err
	}
	value, ok, err := ctx.CheckpointState("gent:react_frame")
	if err != nil {
		return nil, err
	}
	if !ok {
		return &iterationFrame{Iteration: ctx.Iteration()}, nil
	}
	frame, ok := value.(*iterationFrame)
	if !ok || frame == nil {
		return nil, errors.New("gent: invalid ReAct frame")
	}
	if frame.Iteration != ctx.Iteration() {
		return nil, fmt.Errorf("%w: ReAct iteration frame", gent.ErrIncompatibleCheckpoint)
	}
	return frame, nil
}

func saveFrame(ctx *gent.ExecutionContext, frame *iterationFrame) error {
	if err := ctx.SetCheckpointState("gent:react_frame", frame); err != nil {
		return err
	}
	return ctx.CommitCheckpoint(gent.PhaseDecisionReady)
}

// executeToolPlan keeps the saved execution path for native calls.
func (r *Agent) executeToolPlan(
	ctx *gent.ExecutionContext, frame *iterationFrame,
	prepare func() (*gent.ToolPlan, error),
	execute func(
		*gent.ExecutionContext, *gent.ToolPlan, gent.TextFormat,
	) (*gent.ToolCallResult, error),
) (*gent.ToolChainResult, error) {
	var plan *gent.ToolPlan
	var err error
	if frame.ToolPlanActive {
		var found bool
		plan, found, err = ctx.SavedToolPlan()
		if err != nil {
			return nil, err
		}
		if !found {
			return nil, errors.New("gent: ReAct frame has no tool plan")
		}
	} else {
		plan, err = prepare()
		if err != nil {
			return nil, err
		}
		frame.ToolPlanActive = true
		if err := ctx.SetToolPlan(plan); err != nil {
			return nil, err
		}
		if err := saveFrame(ctx, frame); err != nil {
			return nil, err
		}
	}
	for plan.NextCall < len(plan.Calls) {
		if _, err := execute(ctx, plan, r.format); err != nil {
			return nil, err
		}
	}
	return &gent.ToolChainResult{Results: plan.Results}, nil
}
