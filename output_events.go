package gent

const EventNameOutputValidation = "gent:output:validation"

// OutputValidationStage separates a built-in check, a custom callback, and the loop's final
// processing decision. Only the processing stage counts a candidate once for overall limits.
type OutputValidationStage string

const (
	OutputValidationLocal   OutputValidationStage = "local"
	OutputValidationCustom  OutputValidationStage = "custom"
	OutputValidationProcess OutputValidationStage = "process"
)

// OutputValidationEvent records the decision without copying the complete typed value. Steering
// belongs to a rejected candidate. Error means processing failed and does not count as rejection.
type OutputValidationEvent struct {
	BaseEvent
	Stage    OutputValidationStage
	Name     string
	Accepted bool
	Steering []ContentPart
	Error    error
}

type OutputValidationSubscriber interface {
	OnOutputValidation(*ExecutionContext, *OutputValidationEvent)
}

const (
	SCOutputRejectedTotal             StatKey = "gent:output_rejected_total"
	SGOutputRejectedConsecutive       StatKey = "gent:output_rejected_consecutive"
	SCOutputLocalRejectedTotal        StatKey = "gent:output_local_rejected_total"
	SGOutputLocalRejectedConsecutive  StatKey = "gent:output_local_rejected_consecutive"
	SCOutputCustomRejectedTotal       StatKey = "gent:output_custom_rejected_total"
	SCOutputCustomRejectedBy          StatKey = "gent:output_custom_rejected_by:"
	SGOutputCustomRejectedConsecutive StatKey = "gent:output_custom_rejected_consecutive"
)

// PublishOutputValidation queues the decision with its steering. The loop keeps a rejected
// candidate and feedback before it accepts the next checkpoint, including when a limit stops it.
func (ctx *ExecutionContext) PublishOutputValidation(
	stage OutputValidationStage, name string, accepted bool, steering []ContentPart, err error,
) {
	ctx.publish(&OutputValidationEvent{
		BaseEvent: BaseEvent{EventName: EventNameOutputValidation}, Stage: stage, Name: name,
		Accepted: accepted, Steering: CloneContentParts(steering), Error: err,
	})
}

// updateOutputStats runs under the same context lock as the other event statistics.
func (ctx *ExecutionContext) updateOutputStats(event *OutputValidationEvent) {
	if event.Error != nil {
		return
	}
	var total, consecutive StatKey
	switch event.Stage {
	case OutputValidationProcess:
		total, consecutive = SCOutputRejectedTotal, SGOutputRejectedConsecutive
	case OutputValidationLocal:
		total, consecutive = SCOutputLocalRejectedTotal, SGOutputLocalRejectedConsecutive
	case OutputValidationCustom:
		total, consecutive = SCOutputCustomRejectedTotal, SGOutputCustomRejectedConsecutive
	default:
		return
	}
	if event.Accepted {
		ctx.stats.ResetGauge(consecutive)
		return
	}
	ctx.stats.incrCounterDirect(total, 1)
	ctx.stats.incrGaugeInternal(consecutive, 1)
	if event.Stage == OutputValidationCustom {
		ctx.stats.incrCounterDirect(SCOutputCustomRejectedBy+StatKey(event.Name), 1)
	}
}
