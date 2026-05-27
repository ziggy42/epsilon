# Core Values

- **Correctness, Performance & Security**: Absolute top priorities. When in doubt, consult the user on which to prioritize.
- **Simplicity**: Secondary priority; must not be achieved at the expense of correctness, performance or security.

# General Rules

- **Dependencies**: Do not include any external dependencies. This is a **pure Go** project. **No CGo**.
- **Code Clarity**: Write self-explanatory code. Only add comments to explain genuinely non-obvious logic.
- **Scope**: Avoid creative additions unless explicitly requested.
- **Error Handling**: 
  - Never use `panic` unless explicitly instructed.
  - Do not ignore errors. Handle them or propagate them.
- **Naming Conventions**: Use self-explanatory variable names (e.g., use `name` instead of `n`).

# Git & Commits

- **Commit Messages**: Do not use conventional commit prefixes (e.g., `chore:`, `feat:`, `fix:`, `docs:`). Use plain, descriptive language.
- **Commit Authors**: Do not mark any commit as co-authored by agents.
  Commits should only include human authors. In particular, do not add
  `Co-Authored-By: Claude …` (or any other agent) trailers — this project
  is under Google's CLA, and any non-human co-author fails the `cla/google`
  check on the PR.

# Project Specifics

- **Build & test**: Use the root `Makefile` as the entry point. `make help`
  lists all targets. Common ones: `make test` (Go unit + spec tests),
  `make bench` (benchmarks), `make test-wasi` (WASI integration suite).
- **Testing**:
  - Verify core logic changes against spec tests: `make test-spec`
  - Ensure performance regressions are checked: `make bench`
- **Performance**:
  - Minimize heap allocations in the hot path (VM instruction loop).
  - Prefer reusing buffers/slices where possible.
