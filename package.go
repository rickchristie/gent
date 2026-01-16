// Package gent provides a flexible framework for building LLM agents in Go.
//
// The library provides default implementations for common patterns like ReAct. However, the
// skeleton interface is generic enough to allow users to experiment with different agent loop
// patterns, toolchains, termination, or even custom graph or multi-agent pattern.
//
// The idea is this tool allows people to fully experiment everything, it doesn't impose on a
// specific way to do termination, tool call, or even the loop itself. The entire agent loop prompt
// should just be a blank canvas that the user can customize and experiment to the fullest extent.
package gent
