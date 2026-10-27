package toolchain

import (
	"errors"
	"sync"

	"github.com/rickchristie/gent"
)

// SupportsParallelCalls permits concurrent execution of independent plans.
func (c *Registry) SupportsParallelCalls() bool { return true }

// SupportsParallelCalls permits concurrent execution with separate search state for each plan.
func (c *SearchChain) SupportsParallelCalls() bool { return true }

func executeParallelToolPlans(ctx *gent.ExecutionContext, chain gent.ToolChain,
	plans []*gent.ToolPlan, format gent.TextFormat,
) (*gent.ToolChainResult, error) {
	// Each worker owns its plan. The VM and result collector remain on the caller goroutine.
	// Wait on every exit path so tools cannot change the run after the program has returned.
	var workers sync.WaitGroup
	var firstPanic sync.Once
	var panicValue any
	errorsByPlan := make([]error, len(plans))
	for i, plan := range plans {
		workers.Add(1)
		go func() {
			defer workers.Done()
			// Stop context-aware siblings before waiting. A panic during their cleanup must not
			// replace the first panic. Keep the accepted checkpoint available for recovery.
			defer func() {
				if value := recover(); value != nil {
					firstPanic.Do(func() {
						panicValue = value
						ctx.Interrupt(errors.New("gent: parallel tool call panicked"))
					})
				}
			}()
			for plan.NextCall < len(plan.Calls) {
				if _, err := chain.ExecuteNext(ctx, plan, format); err != nil {
					errorsByPlan[i] = err
					return
				}
			}
		}()
	}
	workers.Wait()
	if panicValue != nil {
		panic(panicValue)
	}
	if err := errors.Join(errorsByPlan...); err != nil {
		return nil, err
	}
	result := &gent.ToolChainResult{Results: make([]*gent.ToolCallResult, 0, len(plans))}
	for _, plan := range plans {
		result.Results = append(result.Results, plan.Results...)
	}
	return result, nil
}

var _ gent.ParallelToolChain = (*Registry)(nil)
var _ gent.ParallelToolChain = (*SearchChain)(nil)
