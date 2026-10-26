package main

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestHistoryExample(t *testing.T) {
	var output bytes.Buffer
	if !assert.NoError(t, run(&output)) {
		return
	}
	assert.Equal(
		t,
		`tool_completed: P10 permits the refund.; tool executions: 1; saved terminal result reused.
tool_ready: P10 permits the refund.; tool executions: 1; saved terminal result reused.
`,
		output.String(),
	)
}
