package executor

import (
	"context"
	"fmt"

	"github.com/rickchristie/gent"
)

func (e *Runner[T]) configureRecovery(ctx *gent.ExecutionContext) error {
	if ctx.Parent() != nil {
		return nil
	}
	manifest := ctx.Compatibility()
	if manifest.Components == nil {
		manifest.Components = map[string]string{}
	}
	manifest.Version = 1
	_, configuredLoop := any(e.loop).(interface {
		ConfigureExecution(*gent.ExecutionContext) error
	})
	if !configuredLoop {
		manifest.Components["gent:recovery"] = "unsupported"
		if _, ok := any(e.loop).(gent.CheckpointState); ok && e.config.RecoveryVersion != "" {
			manifest.Components["gent:recovery"] = "supported"
		}
	}
	configuration := map[string]any{"loop": fmt.Sprintf("%T", e.loop),
		"application": e.config.RecoveryVersion}
	components := map[string]any{"loop": e.loop, "data": ctx.Data(),
		"compaction_strategy": ctx.CompactionStrategy(),
		"compaction_trigger":  ctx.CompactionTrigger()}
	for name, component := range components {
		if component == nil {
			continue
		}
		if state, ok := component.(gent.CheckpointState); ok {
			if err := ctx.BindCheckpointState(name, state); err != nil {
				return err
			}
			configuration[name+"State"] = fmt.Sprintf("%s/%d", state.CheckpointType(),
				state.CheckpointVersion())
		}
		if configured, ok := component.(gent.RecoveryConfig); ok {
			configuration[name] = configured.RecoveryConfig()
		} else if name == "compaction_strategy" || name == "compaction_trigger" {
			configuration[name] = fmt.Sprintf("%T", component)
			if e.config.RecoveryVersion == "" {
				manifest.Components["gent:recovery"] = "unsupported"
			}
		}
	}
	if data := ctx.Data(); data != nil {
		if _, basic := data.(*gent.BasicLoopData); !basic {
			if _, saved := data.(gent.CheckpointState); !saved {
				manifest.Components["gent:recovery"] = "unsupported"
			}
		}
	}
	fingerprint, err := gent.RecoveryFingerprint(configuration)
	if err != nil {
		return err
	}
	manifest.Components["gent:executor"] = fingerprint
	return ctx.SetCompatibility(manifest)
}

func (e *Runner[T]) configureExecution(ctx *gent.ExecutionContext) error {
	if err := gent.PrepareOutput(ctx, e.loop.Output()); err != nil {
		return err
	}
	if configured, ok := any(e.loop).(interface {
		ConfigureExecution(*gent.ExecutionContext) error
	}); ok {
		if err := configured.ConfigureExecution(ctx); err != nil {
			return err
		}
	}
	return e.configureRecovery(ctx)
}

// Resume prepares the actual restored context with this executor's normal configuration, then
// continues from its accepted checkpoint. Applications supply new clients/recorders and any
// custom state; they never need a temporary execution context to calculate recovery bindings.
func (e *Runner[T]) Resume(
	worker context.Context, saved gent.RunView, bindings gent.RestoreBindings,
) (T, *gent.ExecutionResult, error) {
	configure := bindings.Configure
	bindings.Configure = func(ctx *gent.ExecutionContext) error {
		if configure != nil {
			if err := configure(ctx); err != nil {
				return err
			}
		}
		return e.configureExecution(ctx)
	}
	ctx, err := gent.RestoreExecutionContext(worker, saved, bindings)
	if err != nil {
		var zero T
		return zero, nil, err
	}
	// Configuration must precede restoration exactly once. Running it again could reset a
	// custom component's restored state or register the same application codec twice.
	return e.Continue(ctx)
}
