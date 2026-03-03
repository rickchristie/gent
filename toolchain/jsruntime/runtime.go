package jsruntime

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/grafana/sobek"
)

// Config holds configuration for the JS runtime.
type Config struct {
	Timeout      time.Duration // default 30s
	MaxCallStack int           // default 1024
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() Config {
	return Config{
		Timeout:      30 * time.Second,
		MaxCallStack: 1024,
	}
}

// Result holds the output of a successful JS execution.
type Result struct {
	ConsoleLog []string // captured console.log() calls
}

// Runtime wraps a Sobek VM with timeout, cancellation,
// and console.log capture.
type Runtime struct {
	vm         *sobek.Runtime
	config     Config
	consoleLog []string
}

// New creates a new Runtime with the given config.
func New(config Config) *Runtime {
	vm := sobek.New()
	r := &Runtime{
		vm:         vm,
		config:     config,
		consoleLog: make([]string, 0),
	}

	vm.SetMaxCallStackSize(config.MaxCallStack)

	// Register console.log
	console := vm.NewObject()
	err := console.Set(
		"log",
		func(call sobek.FunctionCall) sobek.Value {
			args := make(
				[]string, len(call.Arguments),
			)
			for i, arg := range call.Arguments {
				args[i] = arg.String()
			}
			var sb strings.Builder
			for i, a := range args {
				if i > 0 {
					sb.WriteString(" ")
				}
				sb.WriteString(a)
			}
			r.consoleLog = append(
				r.consoleLog, sb.String(),
			)
			return sobek.Undefined()
		},
	)
	if err != nil {
		panic(fmt.Sprintf(
			"jsruntime: failed to set console.log: %v",
			err,
		))
	}
	err = vm.Set("console", console)
	if err != nil {
		panic(fmt.Sprintf(
			"jsruntime: failed to set console: %v", err,
		))
	}

	return r
}

// RegisterFunc registers a synchronous Go function as
// a JS global.
func (r *Runtime) RegisterFunc(
	name string,
	fn func(call sobek.FunctionCall) sobek.Value,
) {
	err := r.vm.Set(name, fn)
	if err != nil {
		panic(fmt.Sprintf(
			"jsruntime: failed to register func %q: %v",
			name, err,
		))
	}
}

// RegisterObject registers a Go object with methods as
// a JS global (e.g., tool.call, tool.parallelCall).
func (r *Runtime) RegisterObject(
	name string,
	methods map[string]func(sobek.FunctionCall) sobek.Value,
) {
	obj := r.vm.NewObject()
	for methodName, fn := range methods {
		err := obj.Set(methodName, fn)
		if err != nil {
			panic(fmt.Sprintf(
				"jsruntime: failed to set %s.%s: %v",
				name, methodName, err,
			))
		}
	}
	err := r.vm.Set(name, obj)
	if err != nil {
		panic(fmt.Sprintf(
			"jsruntime: failed to register obj %q: %v",
			name, err,
		))
	}
}

// VM returns the underlying Sobek runtime. Used by the
// bridge to register tool functions directly.
func (r *Runtime) VM() *sobek.Runtime {
	return r.vm
}

// Execute runs source with timeout and context
// cancellation. Returns Result or an error with
// LLM-friendly formatting (via FormatError).
func (r *Runtime) Execute(
	ctx context.Context, source string,
) (*Result, error) {
	// Set up timeout
	timer := time.AfterFunc(r.config.Timeout, func() {
		r.vm.Interrupt("timeout")
	})
	defer timer.Stop()

	// Watch for context cancellation
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			r.vm.Interrupt("cancelled")
		case <-done:
		}
	}()

	// Clear console log for this execution
	r.consoleLog = r.consoleLog[:0]

	_, err := r.vm.RunString(source)
	if err != nil {
		return nil, fmt.Errorf(
			"%s", FormatError(source, err),
		)
	}

	// Copy console log
	logs := make([]string, len(r.consoleLog))
	copy(logs, r.consoleLog)

	return &Result{ConsoleLog: logs}, nil
}
