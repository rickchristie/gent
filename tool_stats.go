package gent

// Only newly published observations reach this path. Replay reuses the saved runtime and does
// not publish old tool events. Recovery outcomes never reset fresh-call error gauges.
func (ctx *ExecutionContext) updateToolRecordStats(record *ToolCallRecord) {
	name := StatKey(record.Name())
	switch record.Kind() {
	case ToolResponseRecoveryError:
		ctx.stats.incrCounterDirect(SCToolRecoveryErrorTotal, 1)
		ctx.stats.incrCounterDirect(SCToolRecoveryErrorFor+name, 1)
	case ToolResponseRecovery:
		return
	case ToolResponseCallError:
		ctx.addToolError(record.Name())
	case ToolResponseExecution:
		if record.Execution().Status == ToolInvalidArguments {
			ctx.stats.incrCounterDirect(SCToolCallsInvalidArgumentsTotal, 1)
			ctx.stats.incrCounterDirect(SCToolCallsInvalidArgumentsFor+name, 1)
			return
		}
		ctx.stats.ResetGauge(SGToolCallsErrorConsecutive)
		ctx.stats.ResetGauge(SGToolCallsErrorConsecutiveFor + name)
	}
}

func (ctx *ExecutionContext) addToolError(name string) {
	ctx.stats.incrCounterDirect(SCToolCallsErrorTotal, 1)
	ctx.stats.incrGaugeInternal(SGToolCallsErrorConsecutive, 1)
	if name == "" {
		return
	}
	ctx.stats.incrCounterDirect(SCToolCallsErrorFor+StatKey(name), 1)
	ctx.stats.incrGaugeInternal(SGToolCallsErrorConsecutiveFor+StatKey(name), 1)
}
