package gent

// SteeringMessages supplies authored model guidance for one execution. Empty fields use defaults.
// Overrides replace the default sentence, not required context, schema, or tool Instructions.
// These are plain text: phrases such as FULL STOP do not act as framework execution commands.
type SteeringMessages struct {
	ToolBlocked        string
	ToolUnavailable    string
	InvalidArguments   string
	ToolError          string
	RecoveryError      string
	RecoveryCompleted  string
	RecoveryNotStarted string
	RecoveryUnresolved string
	ImageURLLoadFailed string
	UnsupportedMedia   string
	PTCBytesOmitted    string
}

// DefaultSteeringMessages returns the built-in model guidance. Errors do not prove absence of
// effects. Recovery reports leave the next action to the model and never promise a safe retry.
func DefaultSteeringMessages() SteeringMessages {
	return SteeringMessages{
		ToolBlocked:     "The requested action was blocked.",
		ToolUnavailable: "This tool is not available. Review the available tools before continuing.",
		InvalidArguments: "The supplied arguments are invalid. " +
			"Correct them before requesting execution.",
		ToolError: "The tool could not provide its response. " +
			"Effects may have occurred. Decide how to continue.",
		RecoveryError: "The recovery check could not be completed. " +
			"The original outcome is not confirmed.",
		RecoveryCompleted: "This is the recovered original response. " +
			"It may no longer describe current state.",
		RecoveryNotStarted: "The original action did not start. " +
			"Review current state before choosing the next action.",
		RecoveryUnresolved: "The original outcome could not be confirmed. " +
			"Check its state before repeating the action.",
		ImageURLLoadFailed: "The image URL failed to load. Decide whether to request a new URL.",
		UnsupportedMedia:   "This model cannot receive this media type. Decide how to continue.",
		PTCBytesOmitted: "Byte data is not included automatically. " +
			"Use tool.forward in the same script to include it. " +
			"A later script must obtain new tool results.",
	}
}

// Resolved fills empty settings with defaults. It does not inspect or execute authored text.
func (s SteeringMessages) Resolved() SteeringMessages {
	d := DefaultSteeringMessages()
	fields := [][2]*string{
		{&s.ToolBlocked, &d.ToolBlocked}, {&s.ToolUnavailable, &d.ToolUnavailable},
		{&s.InvalidArguments, &d.InvalidArguments}, {&s.ToolError, &d.ToolError},
		{&s.RecoveryError, &d.RecoveryError}, {&s.RecoveryCompleted, &d.RecoveryCompleted},
		{&s.RecoveryNotStarted, &d.RecoveryNotStarted}, {&s.RecoveryUnresolved, &d.RecoveryUnresolved},
		{&s.ImageURLLoadFailed, &d.ImageURLLoadFailed}, {&s.UnsupportedMedia, &d.UnsupportedMedia},
		{&s.PTCBytesOmitted, &d.PTCBytesOmitted},
	}
	for _, pair := range fields {
		if *pair[0] == "" {
			*pair[0] = *pair[1]
		}
	}
	return s
}

// WithSteeringMessages sets execution guidance. Gent saves it for resume. Managed model calls
// use this setting; direct adapter calls use CallOptions.SteeringMessages.
func WithSteeringMessages(value SteeringMessages) ContextOption {
	return func(options *contextOptions) { options.steeringMessages = value }
}

// SteeringMessages returns resolved guidance for this execution, with empty settings filled.
func (ctx *ExecutionContext) SteeringMessages() SteeringMessages {
	return ctx.steeringMessages.Resolved()
}

// PTCIntegerMode selects the JavaScript representation of captured integer data, not byte arrays,
// floating-point values, or saved source data. Configure it before an execution starts.
type PTCIntegerMode uint8

const (
	// PTCIntegerNumber is the default. JavaScript numbers can round integers outside the safe range.
	PTCIntegerNumber PTCIntegerMode = iota
	// PTCIntegerBigInt represents all captured integers as bigint, including small integers.
	PTCIntegerBigInt
)

// WithPTCIntegerMode selects the per-execution representation. Gent saves it for resume.
// Invalid modes are rejected before a script runs. Separate runs can use one shared wrapper.
func WithPTCIntegerMode(mode PTCIntegerMode) ContextOption {
	return func(options *contextOptions) { options.ptcIntegerMode = mode }
}

// PTCIntegerMode returns the saved integer mode for this execution.
func (ctx *ExecutionContext) PTCIntegerMode() PTCIntegerMode { return ctx.ptcIntegerMode }

// PrepareExecutionParts selects safe accepted content and appends instructions once. Input
// rejection instructions must already contain safe correction details and the complete schema.
// The returned slice is new because it adds guidance; its read-only payloads share ownership.
func PrepareExecutionParts(response *ToolExecutionResponse,
	steering SteeringMessages,
) []ContentPart {
	steering = steering.Resolved()
	parts := make([]ContentPart, 0, len(response.Parts)+1)
	switch response.Status {
	case ToolExecuted:
		parts = append(parts, response.Parts...)
	case ToolBlocked:
		parts = append(parts, TextPart(steering.ToolBlocked))
	case ToolInvalidArguments:
		parts = append(parts, TextPart(steering.InvalidArguments))
	}
	if response.Instructions != "" {
		parts = append(parts, TextPart(response.Instructions))
	}
	return parts
}

// PrepareRecoveryParts retains recovered content, a recovery note, evidence, and both sets of
// instructions. It does not repeat or schedule the original action.
func PrepareRecoveryParts(response *ToolRecoveryResponse, steering SteeringMessages) []ContentPart {
	steering = steering.Resolved()
	var parts []ContentPart
	switch response.Status {
	case ToolRecoveryCompleted:
		parts = PrepareExecutionParts(response.Execution, steering)
		parts = append(parts, TextPart(steering.RecoveryCompleted))
	case ToolRecoveryNotStarted:
		parts = append(parts, TextPart(steering.RecoveryNotStarted))
	case ToolRecoveryUnresolved:
		parts = append(parts, TextPart(steering.RecoveryUnresolved))
	}
	parts = append(parts, response.Parts...)
	if response.Instructions != "" {
		parts = append(parts, TextPart(response.Instructions))
	}
	return parts
}
