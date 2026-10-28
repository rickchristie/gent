package gent

import (
	"errors"
	"fmt"
)

const defaultRecoveryMessage = `The operation was interrupted. Its outcome is unknown.
It may have completed. Check the current state before you decide whether to repeat it.`

func validateToolRecovery(response *ToolRecoveryResponse) error {
	if response == nil {
		return errors.New("gent: tool recovery returned no response")
	}
	switch response.Result {
	case ToolRecoveryCompleted:
		if response.Output == nil {
			return errors.New("gent: completed tool recovery has no output")
		}
		if response.Output.Outcome != "" {
			return errors.New("gent: completed tool recovery cannot have an unknown or unstarted outcome")
		}
	case ToolRecoveryRetryAllowed, ToolRecoveryUnresolved:
		if response.Output != nil {
			return errors.New("gent: only completed tool recovery can return output")
		}
	default:
		return fmt.Errorf("gent: invalid tool recovery result %q", response.Result)
	}
	return nil
}

func joinSteering(before, next string) string {
	if before == "" {
		return next
	}
	if next == "" {
		return before
	}
	return before + "\n\n" + next
}

// Add steering before result evidence is frozen, so a resumed run sees the same observation.
func (ctx *ExecutionContext) addRecoverySteering(id string, output any) {
	plan := ctx.toolPlanFor(id)
	if plan == nil || plan.RecoverySteering == "" {
		return
	}
	result, ok := output.(*ToolCallResult)
	if !ok {
		return
	}
	result.Instructions = joinSteering(result.Instructions, plan.RecoverySteering)
	result.Text += "\n\nRecovery guidance:\n" + plan.RecoverySteering
	// Recovery instructions must remain visible if an older identical result is shortened.
	if result.DeduplicationSummary != "" {
		result.DeduplicationSummary += "\n\nRecovery guidance:\n" + plan.RecoverySteering
	}
}
