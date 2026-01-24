# Testing Standards
- Always use table-driven tests for functions with multiple scenarios.
- For table driven tests, create explicit `input`, `mocks` (if needed), and `expected` structs.
- The expected struct should contain all expected outputs, including errors.
- Expected output must be FULL matches, e.g. for strings, compare entire string, not substrings.
- Use testify's `assert` package for assertions.

# Standards & Conventions
- MAX line length: 100 characters.
- NEVER swallow errors. Errors must be either returned, or logged.
