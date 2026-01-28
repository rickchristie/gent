<testing_standards>
**General Standards**
- Always use table-driven tests for functions with multiple scenarios.
- For table driven tests, create explicit `input`, `mocks` (if needed), and `expected` structs.
- The expected struct should contain all expected outputs, including errors.
- Expected output must be FULL matches, for example:
  - For strings, compare entire string, not substrings.
  - For structs, compare entire struct, not just individual fields.
  - For fields that contains timestamps, assert >= expected time instead of exact match, DO NOT use "not zero" assertion for timestamps!
- The process being tested MUST be close to real-world as possible.
  Avoid changing internals to simulate scenarios, i.e. call public methods instead of changing private fields.
- No vanity tests. Tests must validate real functionality/feature that user expects.
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
