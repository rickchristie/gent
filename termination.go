package gent

// Termination is a [TextSection] that signals the agent should stop.
// The parsed result represents the final output of the agent.
type Termination interface {
	TextSection

	// ShouldTerminate checks if the given content indicates termination.
	// If it returns a non-empty slice, the agent should terminate with that content as the result.
	// If it returns nil or empty slice, the agent should continue.
	ShouldTerminate(content string) []ContentPart
}
