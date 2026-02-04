---
name: code-review
description:
  Use this skill when the user asks for a code review. It automates checks and
  analysis.
---

# Code Review

## Overview

This skill provides a comprehensive code review process for the current branch.
It verifies the code builds and passes tests across platforms, checks for
performance regressions, and reviews the diff for bugs, improvements, and
adherence to project standards.

## Procedure

### 1. Preparation

1.  **Check Branch**: Run `git branch --show-current`. If the output is `main`,
    inform the user that the current branch is `main` and reviews on `main` are
    not supported yet.
2.  **Check Remote**: Run `git fetch origin main`. This ensures `origin/main` is
    up-to-date for accurate comparison.
3.  **Get Branch Point**: Determine the merge base with
    `git merge-base origin/main HEAD`. Store this for later use.

### 2. Build & Test Verification

1.  **Build for All Platforms**:
    - Run `GOOS=linux go build -o epsilon-linux ./cmd/epsilon`
    - Run `GOOS=darwin go build -o epsilon-darwin ./cmd/epsilon`
    - Run `GOOS=windows go build -o epsilon.exe ./cmd/epsilon`
    - Run `rm -f epsilon-linux epsilon-darwin epsilon.exe` to clean up.
    - **If any build fails, stop and report the error.**
2.  **Lint & Format**:
    - Run `go fmt ./...`. If any files are modified, **stop and report the
      error**.
    - Run `go vet ./...`. **If any issues are found, stop and report the
      error.**
3.  **Run All Tests**:
    - Run `go test ./...` to execute all tests.
    - **If tests fail, stop and report the errors.**
4.  **Run WASI Tests**:
    - Run `uv run --with-requirements requirements.txt wasip1/wasi_testsuite.py`
      to verify WASI implementation.
    - **If WASI tests fail, stop and report the errors.**

### 3. Performance Verification

1.  **Compare Benchmarks**: Run
    `./internal/benchmarks/compare.py --base main --target .` to compare
    performance against the main branch. **NOTE**: This can take several minutes
    as it runs the benchmark suite twice.
2.  **Review Results**: Present the benchmark results to the user. Flag any
    regressions where:
    - Time (ns/op) increased by more than 5%.
    - Memory (B/op) increased.
    - Allocations (allocs/op) increased.

### 4. Dependency Check

1.  **Detect New Dependencies**: Run
    `git diff origin/main..HEAD -- go.mod go.sum`.
2.  **Verify Module Consistency**:
    - Run `go mod tidy`.
    - Run `git diff --name-only go.mod go.sum`.
    - If there are changes, **stop and report that `go mod tidy` needs to be
      run**.
3.  **Report Changes**: If there are any changes to `go.mod` or `go.sum`:
    - **CRITICAL**: This project is **pure Go with no CGo**. New dependencies
      are strongly discouraged.
    - Report exactly what dependencies were added or modified.
    - Ask the user to confirm the dependency changes are intentional.

### 5. License Header Check

1.  **Find New Files**: Run
    `git diff --name-status origin/main..HEAD | grep '^A'` to list all newly
    added files.
2.  **Check Headers**: For each new source file (`.go`, `.py`, `.yml`, etc.),
    verify it contains the proper license header:

    ```
    // Copyright [Year] Google LLC
    //
    // Licensed under the Apache License, Version 2.0 (the "License");
    // you may not use this file except in compliance with the License.
    // You may obtain a copy of the License at
    //
    //     http://www.apache.org/licenses/LICENSE-2.0
    //
    // Unless required by applicable law or agreed to in writing, software
    // distributed under the License is distributed on an "AS IS" BASIS,
    // WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
    // See the License for the specific language governing permissions and
    // limitations under the License.
    ```

    Use the appropriate comment syntax for the file type (e.g., `#` for Python
    and YAML).

3.  **Report Missing Headers**: List any files missing the license header.

### 6. Code Review

1.  **Get the Diff**: Run `git diff origin/main..HEAD` to see all changes.
2.  **Review for Issues**: Analyze the diff and check for:

    **Bugs & Correctness**:
    - Logic errors and edge cases.
    - Improper error handling (errors must be handled or propagated, never
      ignored).
    - Data races or concurrency issues.
    - Off-by-one errors.

    **Performance** (especially in hot paths like the VM loop):
    - Unnecessary heap allocations.
    - Inefficient algorithms.
    - Missed opportunities to reuse buffers/slices.
    - Unnecessary copying of data.

    **Simplicity & Readability**:
    - Overly complex code that could be simplified.
    - Duplicate code that could be refactored.
    - Poor variable naming (names should be self-explanatory).
    - Unnecessary comments (code should be self-explanatory). Verify no "TODO",
      "FIXME", or placeholder comments were left behind.

    **Documentation**:
    - Ensure `README.md` and other documentation is updated if new features,
      flags, or configuration options were added.

    **Modern Go Features**:
    - Opportunities to use newer Go features (e.g., generics, improved slices
      package, range-over-int, etc.).
    - Deprecated patterns that should be updated.

    **Project Standards**:
    - Adherence to rules in `.rules`.
    - Consistent style with existing code.

3.  **Compile Findings**: Create a structured report with:
    - **Critical Issues**: Bugs, correctness problems, or security concerns that
      must be fixed.
    - **Suggestions**: Performance improvements, simplifications, or style
      enhancements.
    - **Notes**: Minor observations or questions.

### 7. Report

Present a summary to the user containing:

1.  ✅ or ❌ for each verification step (build, lint, tests, spec tests, WASI
    tests).
2.  Benchmark comparison results with any regressions highlighted.
3.  Dependency change report (if any).
4.  List of files missing license headers (if any).
5.  Code review findings organized by severity.
6.  An overall recommendation: **Ready to Merge** or **Needs Changes**.
