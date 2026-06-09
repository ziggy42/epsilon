# Changelog

All notable changes to this project will be documented in this file.

## [0.1.0] - 2026-05-19

This release fixes a significant number of security and correctness issues
throughout the runtime. Background on several of the vulnerabilities is
available in this
[article](https://andreapivetta.com/posts/all-the-bugs-they-found.html). Overall
performance is roughly unchanged from 0.0.4; one microbenchmark exercising
`call_indirect` is about 10% slower due to the additional safety checks.

### New Features

- Instruction fuel/gas metering for bounding guest execution (#40).
- Configurable resource limits on `Config` for table, memory, and per-function
  locals (`MaxTableElements`, `MaxMemoryPages`, `MaxLocalsPerFunction`) to bound
  attacker-controlled pre-allocations (#67).

### API Changes

- `Runtime` configuration must now be supplied at construction time. The
  builder-style `WithConfig` has been removed in favour of
  `NewRuntimeWithConfig` (#64).

  ```go
  // Before
  runtime := epsilon.NewRuntime().WithConfig(cfg)

  // After
  runtime := epsilon.NewRuntimeWithConfig(cfg)
  ```

- Imports API reshaped to enforce per-runtime ownership of `Memory`, `Table`,
  and `Global` instances (#64).

  ```go
  // Before
  imports := epsilon.NewModuleImportBuilder("env").
      AddMemory("memory", epsilon.NewMemory(memType)).
      AddGlobal("offset", int32(1024), false, epsilon.I32).
      Build()
  runtime.InstantiateModuleWithImports(wasm, imports)

  // After
  imports := epsilon.NewModuleImports("env").
      AddMemory("memory", runtime.NewMemory(memType)).
      AddGlobal("offset", runtime.NewGlobal(int32(1024), false, epsilon.I32))
  runtime.InstantiateModuleWithImports(wasm, imports)
  ```

- `Default*` constants are exposed for every non-zero `Config` default (#67).

- REPL removed (#38).

### Security & Correctness Fixes

- Cross-`Runtime` isolation: `Memory`, `Table`, `Global`, and function instances
  can no longer be shared across runtimes, and cross-runtime function imports
  are rejected (#50, #64).
- `ModuleInstance.Invoke` now validates reference-type arguments (#60).
- Cap attacker-controlled pre-allocations in `parseVector`, `parseUtf8String`,
  `parseCustomSection`, `readImmediateVector`, and the per-function locals slice
  (#66).
- Parser no longer desynchronizes when `bufio.Reader` over-reads past a function
  body (f2c764c).
- Block type validation rejects non-canonical SLEB128 encodings (c1f5802) and
  out-of-range block type indices (#52).
- `select t*` validation requires exactly one result type and rejects unknown
  type codes (ace9955).
- Reject improperly-encoded memory indices in load/store instructions (#53).
- Validate `funcref` arguments to prevent access to unexported functions (#49).
- Fix stack-corruption via `return` and host function entry/exit (#48, #61).
- Fix uninitialized reference local leak (#62).
- Improve end-of-function validation so immediates are not misinterpreted as
  `end` (#51).
- Remediate integer overflows in table and memory operations (#55).
- Correct `table.init` handling of dropped and expression-form element segments
  (da2b139).
- Tighten global import type validation (483df64) and several over-permissive
  validation paths (1cdb8e1).

### WASI

- Enforce missing rights: `RightsFdSeek` on `fd_pread`/`fd_pwrite`;
  per-event-type rights on `poll_oneoff`; gate the `Stat()` call in
  `poll_oneoff` on `RightsFdFilestatGet` (#65).
- Tighten input validation and fix an FD leak in WASI host functions (#57); fix
  rights escalation and capability bypass (#56).
- Fix clock implementation flaws and `poll_oneoff` timeout overflow (#59, #58).
- Document `poll_oneoff` busy-loop risk for sockets (e4e57f1).

## [0.0.4] - 2026-02-01

- Added experimental support for [WASI Preview 1](wasip1/README.md) on Linux and
  macOS.
- **API Changes**: Host functions now receive `ModuleInstance` as their first
  argument, enabling context-aware implementations (#32).
- CLI now invokes `_start` by default if no entry point is specified.

## [0.0.3] - 2025-12-20

- **API Changes**: `ExperimentalFeatures` has been replaced by `Config`, and
  `Runtime.WithFeatures` has been renamed to `WithConfig` (#28).
- `Config` now allows configuring pre-allocated cache sizes and the maximum call
  stack depth. It also retains support for enabling experimental features like
  `ExperimentalMultipleMemories` (#28).
- Major performance improvements (#25, #28, #30).

## [0.0.2] - 2025-12-14

- Major, multiple performance improvements (#21, #22, #24)
- Introduced a simple CLI (#19, #20)
- Added hello world example (#10 @deadprogram)
- Fixed malloc crash on Fedora when parsing invalid custom sections
- Fixed bounds check on amd64 (#13, #14)

## [0.0.1] - 2025-12-07

Initial release.
