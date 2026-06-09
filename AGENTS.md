# Project Overview

Epsilon is a pure-Go, zero-CGo Wasm 2.0 runtime, usable both as an embeddable
library and as a CLI. Layout:

- `epsilon/` — the public, importable package: parser, validation, the VM
  instruction loop, and memory/table/global/host-import machinery. Exported
  symbols here are the library's public API; treat signature changes as
  breaking.
- `wasip1/` — WASI Preview 1 host implementation.
- `cmd/epsilon/` — the CLI entry point.
- `internal/spec_tests/` — Wasm spec conformance runner.
- `internal/benchmarks/` — the benchmark suite.

# Core Values

- **Correctness, Performance & Security**: Absolute top priorities. When in
  doubt, consult the user on which to prioritize.
- **Simplicity**: Secondary priority; must not be achieved at the expense of
  correctness, performance or security.

# General Rules

- **Dependencies**: No third-party dependencies except `golang.org/x`, and **no
  CGo**.
- **Comments**: Write self-explanatory code; only add comments for non-obvious
  logic. Doc comments state _what_ a symbol is or does, not how, where, or by
  whom it is used.
- **Line Width**: Keep lines within 80 columns, counting a tab as 2 columns.
  Fill comment lines greedily — don't wrap early.
- **Security**: Treat Wasm modules and host inputs as untrusted. The validator
  is the trust boundary the VM relies on — never weaken or skip its checks.
  Invalid input must surface as a Go error, never a panic.
- **Naming Conventions**: Use self-explanatory variable names (e.g., use `name`
  instead of `n`).

# Git & Commits

- **Commit Messages**: Do not use conventional commit prefixes (e.g., `chore:`,
  `feat:`, `fix:`, `docs:`). Use plain, descriptive language.
- **Commit Authors**: No agent co-author trailers (e.g.
  `Co-Authored-By: Claude …`).

# Project Specifics

- **Build & test**: The root `Makefile` is the entry point (`make help` lists
  all targets). Before considering a change complete, run `make fmt`,
  `make vet`, and `make test-all` and make sure they pass; if you changed
  Markdown, also run `make fmt-md`.
- **Performance**:
  - Minimize heap allocations in the hot path (VM instruction loop).
  - Prefer reusing buffers/slices where possible.
  - Check for regressions with `make bench` when touching hot paths.
