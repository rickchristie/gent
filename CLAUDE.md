<testing_standards>
**General Standards**
- Always use table-driven tests for functions with multiple scenarios.
- For table driven tests, create explicit `input`, `mocks` (if needed), and `expected` structs.
- The expected struct should contain all expected outputs, including errors.
- Expected output must be FULL matches, e.g. for strings, compare entire string, not substrings.
- Use testify's `assert` package for assertions.

**Testing Stats & Limits**
- Whenever we create new stats (`*ExecutionStats`), we need to create tests on:
  - Stat field is incremented correctly.
  - If consecutive, stat field is reset correctly.
  - New limit test at `agent/agent_executor_test.go` with mocks.
- Limit tests must cover:
  - Limit is exceeded in first iteration.
  - Limit is exceeded in Nth iteration.
- If the stat is based on prefix, then the limit test must:
  - Have multiple prefixed stats, e.g. `gent:tool_calls:tool1`, `gent:tool_calls:tool2`.
  - Test must verify that when both limits are reached, only the correct one triggers the limit error.
</testing_standards>

<standards_convention>
- MAX line length: 100 characters.
- NEVER swallow errors. Errors must be either returned, or logged.
</standards_convention>
