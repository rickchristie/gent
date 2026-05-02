# CLI Release-Ready Plan

This plan makes the `gent` CLI ready for a `v0.1.0` release. The goal is that a typical user can
install the CLI, install CPU embedding dependencies, download an embedding model, and copy working
Go code from the CLI/docs without hitting stale APIs or incorrect model names.

## Current State

Gent already has the intended CLI entrypoints:

- `cmd/gent/main.go`: command routing for `setup onnx`, `model list`, and `model download`.
- `cmd/gent/cmd_setup.go`: installs native ONNX/tokenizer dependencies into `~/.gent/lib/`.
- `cmd/gent/cmd_model.go`: lists registered embedding models and downloads ONNX/tokenizer files.
- `common/models.go`: registry of downloadable physical models and standard model directory logic.
- `common/model_configs.go`: tested runtime configurations for ONNX embedder usage.
- `common/download.go`: shared model download helper used by CLI and tests.
- `search/embedder_onnx.go`: CPU ONNX embedder using `common.ModelConfig` plus `search.OnnxOptions`.

The intended post-release user flow is:

```bash
go install github.com/rickchristie/gent/cmd/gent@v0.1.0
gent setup onnx
gent model list
gent model download multilingual-e5-small
```

Then in Go:

```go
cfg := common.FindConfig("multilingual-e5-small")
dir, err := common.ModelDir(cfg.Model.Name)
if err != nil {
    return err
}

embedder, err := search.NewOnnxEmbedder(*cfg, search.OnnxOptions{
    ModelPath:     filepath.Join(dir, cfg.Model.ModelFile),
    TokenizerPath: filepath.Join(dir, "tokenizer.json"),
})
if err != nil {
    return err
}
defer embedder.Close()
```

## Release Blockers

These must be fixed before tagging `v0.1.0`.

- `cmd/gent/cmd_setup.go` recommends `gent model download multilingual-e5-small-int8`, but that
  model does not exist. The valid model name is `multilingual-e5-small`.
- `cmd/gent/cmd_model.go` prints `search.EmbedderConfig{...}`, but `search.EmbedderConfig` no
  longer exists. The current API is `common.ModelConfig` plus `search.OnnxOptions`.
- `search/README.md` still uses `go run ./cmd/gent ...` as the primary setup path. After release,
  it should lead with `go install github.com/rickchristie/gent/cmd/gent@v0.1.0` and `gent ...`.
- `search/README.md` says all 11 registered models are auto-downloaded, but the registry contains
  10 physical models and 11 runtime configs.
- Some Go comments still show stale `NewOnnxEmbedder` examples, including examples that pass a
  single config object or reference the removed `search.EmbedderConfig` type.
- `cmd/gent` has no tests, so stale CLI output can regress without detection.

## Non-Blocking Improvements

These are not required for `v0.1.0`, but they improve first-user experience.

- Add a `gent doctor` command that checks native libraries, model files, and a warm-up embed.
- Add `gent model path <name>` to print the model directory and resolved files.
- Add `gent model config <name-or-config>` to print only the copy-paste Go snippet.
- Add SHA256 checksums for native dependency archives and downloaded model files.
- Add non-interactive setup flags such as `gent setup onnx --yes --profile none` for CI/Docker.
- Add `--dir` to `gent model download` for users who do not want `~/.gent/models`.

## Implementation Plan

### 1. Fix Setup Output

Update `cmd/gent/cmd_setup.go` so the final next-step command uses a valid model name.

Expected output after setup should end with:

```text
Next step: download an embedding model:
  gent model list
  gent model download multilingual-e5-small
```

Acceptance criteria:

- `printf 'n\n' | go run ./cmd/gent setup onnx` still exits without installing.
- The setup guidance never references `multilingual-e5-small-int8`.
- The recommended model exists in `common.ModelRegistry`.

### 2. Fix Model Download Snippet

Update `cmd/gent/cmd_model.go` so `gent model download <name>` prints copy-paste code that matches
the current API.

The snippet should show:

- Imports needed by a typical user: `path/filepath`, `github.com/rickchristie/gent/common`, and
  `github.com/rickchristie/gent/search`.
- `cfg := common.FindConfig("<config-name>")`.
- A nil check for `cfg`.
- `dir, err := common.ModelDir(cfg.Model.Name)`.
- `search.NewOnnxEmbedder(*cfg, search.OnnxOptions{...})`.
- `ModelPath: filepath.Join(dir, cfg.Model.ModelFile)`.
- `TokenizerPath: filepath.Join(dir, "tokenizer.json")`.
- Optional runtime settings such as `NumThreads` and `MaxConcurrency`.
- `defer embedder.Close()`.

For models with multiple configs, such as `nomic-embed-text-v1.5`, print each config separately
and make the config name explicit.

Acceptance criteria:

- No CLI output references `search.EmbedderConfig`.
- Every printed snippet compiles after being inserted into a small Go program with the printed
  imports.
- The printed snippet does not duplicate model semantic fields that already live in
  `common.ModelConfig`.

### 3. Improve `model list` Output

Keep the current model/config table, but make the distinction clearer.

Suggested changes:

- Rename `Available embedding models` to `Downloadable embedding model files`.
- Rename `Configurations per model` to `Runtime configurations`.
- Add one short note that one physical model may have multiple runtime configs.
- Add a release-install hint for `go install github.com/rickchristie/gent/cmd/gent@v0.1.0`.

Acceptance criteria:

- `go run ./cmd/gent model list` makes it obvious why there are 10 model files and 11 configs.
- The command remains readable in a normal terminal width.
- The output still includes memory estimates and best-for guidance.

### 4. Update Search README

Update `search/README.md` to be release-oriented.

Required content:

- CLI installation with `go install github.com/rickchristie/gent/cmd/gent@v0.1.0`.
- Native dependency setup with `gent setup onnx`.
- Model discovery with `gent model list`.
- Model download with `gent model download multilingual-e5-small`.
- Current embedder creation code using `common.FindConfig`, `common.ModelDir`, and
  `search.NewOnnxEmbedder(*cfg, search.OnnxOptions{...})`.
- Correct distinction between 10 registered physical models and 11 runtime configs.
- Test command without `-v`, unless a specific manual/debug command explicitly explains why it
  uses `-v`.

Acceptance criteria:

- README setup commands work for users outside the repo after `v0.1.0` is tagged.
- README code examples compile against current public APIs.
- The old TODO about replacing `go run ./cmd/gent` is removed.

### 5. Update Stale Go Comments

Search for stale references and update them to current APIs.

Known candidates:

- `toolchain/search_engine_fused.go`: comment uses `search.EmbedderConfig{...}`.
- `toolchain/search_engine_index.go`: comment shows `search.NewOnnxEmbedder(cfg)`.
- Any comment found by searching `EmbedderConfig`, `NewOnnxEmbedder(`, `go run ./cmd/gent`, and
  `multilingual-e5-small-int8`.

Acceptance criteria:

- `grep` for `EmbedderConfig` finds no stale user-facing references.
- `grep` for `multilingual-e5-small-int8` finds no references.
- Go examples in comments use the current `common.ModelConfig` plus `search.OnnxOptions` API.

### 6. Add CLI Tests

Add tests under `cmd/gent` for output and command routing that do not perform network downloads.

Recommended test coverage:

- `runModelList()` prints all registered physical models.
- `runModelList()` prints all registered runtime configs.
- `runModelList()` includes the current download command.
- `printConfigs()` prints `search.NewOnnxEmbedder(*cfg, search.OnnxOptions{...})`.
- `printConfigs()` does not print `search.EmbedderConfig`.
- `runModelDownload()` unknown model path lists available models and exits non-zero.
- Setup cancellation path does not install anything and prints the corrected next-step command when
  exercised through a testable helper.

Implementation guidance:

- Prefer refactoring print functions to accept an `io.Writer` instead of capturing `stdout` from
  global `fmt.Println` calls.
- Keep tests network-free by testing print/render functions and unknown model behavior.
- If testing `os.Exit` paths directly is awkward, split command validation from process exit.

Acceptance criteria:

- `go test ./cmd/gent -count=1` passes.
- Tests fail if `search.EmbedderConfig` is reintroduced in CLI output.
- Tests fail if the setup recommendation references an unknown model.

### 7. Add Compile Tests For Documentation Snippets

Add at least one small test or example that compiles the recommended embedder creation pattern.

Options:

- Add an `Example` in `search` using `common.FindConfig` and `search.OnnxOptions` but do not run
  inference.
- Add a command-output test that writes the generated snippet into a temporary Go module and runs
  `go test` on it. This is stronger but slower.
- Add a minimal static compile test in the repo that constructs `OnnxOptions` and validates paths
  without opening ONNX Runtime.

Acceptance criteria:

- The main user-facing snippet cannot drift from the public API silently.
- The test does not download models or require ONNX Runtime unless explicitly marked integration.

### 8. Validate Release Install Path

After the CLI/docs fixes are committed, validate the install path locally before tagging.

Commands:

```bash
go install ./cmd/gent
printf 'n\n' | gent setup onnx
```

Acceptance criteria:

- `go install ./cmd/gent` succeeds.
- `gent model list` output is correct and release-oriented.
- Setup cancellation exits cleanly without writing shell profile changes.
- Unknown model errors list valid model names.

### 9. Optional Real Download Smoke Test

Run one real model download if network and disk are acceptable.

Recommended model:

```bash
gent model download bge-micro-v2
```

Then run a minimal embedder smoke test if native libraries are installed:

```bash
go test ./search -run TestOnnxEmbedder_AllModels -count=1 -timeout 300s
```

Acceptance criteria:

- Download resumes/skips correctly when files already exist.
- The CLI prints correct config after download.
- Tests skip gracefully if ONNX Runtime is not installed.

## Release Readiness Checklist

Before tagging `v0.1.0`, confirm all items are complete.

- `cmd/gent/cmd_setup.go` recommends `multilingual-e5-small`.
- `cmd/gent/cmd_model.go` prints current API snippets.
- `search/README.md` uses release install commands.
- Stale `EmbedderConfig` references are removed or intentionally explained as historical.
- Stale `go run ./cmd/gent` setup commands are removed from release docs.
- CLI tests exist and pass.
- `go test ./cmd/gent -count=1` passes.
- `go test ./... -count=1 -timeout 300s` passes.
- `go install ./cmd/gent` succeeds.
- `gent model list` output is manually reviewed.
- `printf 'n\n' | gent setup onnx` is manually reviewed.
- `scripts/release.sh v0.1.0 --allow-dirty` dry-run output is manually reviewed before execute.

## Future Work After v0.1.0

Do not block the initial release on these unless we decide CLI onboarding is the release headline.

- `gent doctor`: validate `CGO_LDFLAGS`, `LD_LIBRARY_PATH`, `GENT_ORT_LIB`, native libraries,
  downloaded model files, and one warm-up embedding.
- `gent setup onnx --yes`: non-interactive install for Docker and CI.
- `gent setup onnx --print-env`: print environment exports without mutating shell profiles.
- `gent model download --all-small`: download a curated small set for local experimentation.
- `gent model verify <name>`: check files exist and optionally hash them.
- `gent model remove <name>`: clean downloaded model files.
- `gent model config <config-name>`: print only one config snippet.
- Checksums and signed release metadata for native dependency downloads.
