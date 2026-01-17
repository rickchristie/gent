package gent

// Termination is a [TextOutputSection] that signals the agent should stop.
// The parsed result represents the final output of the agent.
type Termination interface {
	TextOutputSection
}
