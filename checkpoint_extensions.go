package gent

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reflect"
)

// RecoveryConfig describes declarative configuration. Application implementation changes belong
// in a caller-supplied recovery version; clients, credentials, and functions are never encoded.
type RecoveryConfig interface {
	RecoveryConfig() any
}

func RecoveryFingerprint(value any) (string, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:]), nil
}

// BindCheckpointState binds an explicit extension before execution. Save/Restore methods must
// only copy local state; they must not publish events or call external services.
func (ctx *ExecutionContext) BindCheckpointState(name string, component CheckpointState) error {
	if name == "" || component == nil || component.CheckpointType() == "" ||
		component.CheckpointVersion() < 1 {
		return fmt.Errorf("%w: invalid checkpoint component %q", ErrUnsupportedRecovery, name)
	}
	ctx.run.mu.Lock()
	defer ctx.run.mu.Unlock()
	if ctx.run.components == nil {
		ctx.run.components = map[string]CheckpointState{}
	}
	if existing := ctx.run.components[name]; existing != nil {
		if !reflect.ValueOf(existing).Comparable() || existing != component {
			return fmt.Errorf("gent: component %q already bound", name)
		}
	}
	ctx.run.components[name] = component
	return nil
}

func (ctx *ExecutionContext) captureExtensions() error {
	ctx.run.mu.RLock()
	components := make(map[string]CheckpointState, len(ctx.run.components))
	for name, component := range ctx.run.components {
		components[name] = component
	}
	ctx.run.mu.RUnlock()
	values := map[string]SavedCheckpointState{}
	for name, component := range components {
		value, err := component.SaveCheckpointState()
		if err != nil {
			return fmt.Errorf("gent: save component %q: %w", name, err)
		}
		if value.Type == "" || value.Version < 1 || !json.Valid(value.Data) {
			return fmt.Errorf("gent: invalid state from component %q", name)
		}
		values[name] = SavedCheckpointState{component.CheckpointType(),
			component.CheckpointVersion(), cloneWire(value)}
	}
	ctx.run.mu.Lock()
	ctx.run.extensions = values
	ctx.run.mu.Unlock()
	return nil
}

func restoreExtensions(state ContextCheckpoint, bindings RestoreBindings) error {
	components := make(map[string]CheckpointState, len(bindings.Components)+1)
	for name, component := range bindings.Components {
		components[name] = component
	}
	if data, ok := bindings.Data.(CheckpointState); ok {
		components["data"] = data
	}
	for name, saved := range state.Extensions {
		component := components[name]
		if component == nil || component.CheckpointType() != saved.Type ||
			component.CheckpointVersion() != saved.Version {
			return fmt.Errorf("%w: component %q", ErrIncompatibleCheckpoint, name)
		}
	}
	for name, saved := range state.Extensions {
		if err := components[name].RestoreCheckpointState(cloneWire(saved.Value)); err != nil {
			return fmt.Errorf("gent: restore component %q: %w", name, err)
		}
	}
	return nil
}
