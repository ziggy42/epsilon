---
name: release
description: Use this skill when the user wants to cut a new release.
---

# Release

## Overview

This skill guides the agent through the process of cutting a new release for
this project. It handles version determination, changelog generation, file
updates, and git tagging.

## Procedure

### 1. Preparation & Version Determination

1.  **Check Branch**: Run `git branch --show-current`. If the output is not
    `main`, stop and inform the user that releases must be cut from the `main`
    branch.
2.  **Sync with Remote**: Run `git fetch origin main`. Then verify local is
    up-to-date by running `git rev-list HEAD..origin/main --count`. If the count
    is non-zero, stop and inform the user that their local `main` is behind
    `origin/main` and they need to pull first.
3.  **Check Working Tree**: Run `git status --porcelain`.
    - **CRITICAL**: If the command produces **ANY** output (even for untracked
      files or documentation changes), you **MUST ABORT IMMEDIATELY**.
    - **DO NOT** attempt to analyze the changes.
    - **DO NOT** ask the user if they want to proceed.
    - **STOP** and inform the user: "Working tree is not clean. Please commit or
      stash changes before releasing."
4.  **Check Current Version**: Read `epsilon/version.go` to find the current
    version (e.g., "0.0.3").
5.  **Determine Target Version**:
    - **User Provided**: If the user specified a version, validate it. It must
      be semantically greater than the current version. If invalid, reject it
      and explain why.
    - **Auto-Increment**: If no version was provided, increment the **patch**
      level of the current version (e.g., 0.0.3 -> 0.0.4).
6.  **Confirm**: Briefly mention the plan to the user (e.g., "Preparing release
    for v0.0.4...").

### 2. Verification

1.  **Build**: Run `go build ./...`. If the build fails, stop and report the
    error.
2.  **Run Tests**: Run `go test ./...`. If tests fail, stop and report the
    errors.
3.  **Compare Benchmarks**: Find the previous release tag using
    `git describe --tags --abbrev=0`. Then run
    `./internal/benchmarks/compare.py --base <last_tag> --target .` to compare
    performance against the last release. Present the results to the user and
    flag any significant regressions. **NOTE**: This process can take several
    minutes as it runs the full benchmark suite twice; ensure a reasonable
    timeout (e.g., 10m) is applied.

### 3. Changelog Generation

**Only proceed if verification passes.**

1.  **Identify Range**: Find the previous release tag using
    `git describe --tags --abbrev=0`.
2.  **Fetch Commits**: Run
    `git log --no-merges --pretty=format:"%h %s%n%b" <last_tag>..HEAD`.
3.  **Draft Content**: Analyze the commit messages (subject and body) to create
    a new CHANGELOG entry.
    - **Deep Inspection**: Look at the commit **body** for additional context.
      Squashed PRs often include a detailed list of changes, bullet points, or
      rationale in the description. Use this to create a more informative
      summary.
    - **Audience**: The changelog is for **end users**. Focus on changes that
      affect how users interact with the project.
    - **Style**: Mimic the existing style in `CHANGELOG.md`.
    - **Filtering**: Include only changes that matter to users: new features,
      bug fixes, API changes. Exclude chores, typos, refactoring, CI tweaks, and
      internal tooling changes.
    - **Grouping**: Group by category if appropriate (e.g., API Changes,
      Performance, Fixes).
    - **Breaking Changes**: Explicitly highlight any breaking changes.
    - **Format**:

      ```markdown
      ## [Version] - YYYY-MM-DD

      - Description of change (#PR or commit hash if available).
      - ...
      ```

4.  **Review**: Present the drafted CHANGELOG entry to the user and ask for
    confirmation. **Do not proceed without user approval.**

### 4. Execution

**Only proceed after User Confirmation of the changelog.**

1.  **Update CHANGELOG.md**:
    - Read `CHANGELOG.md`.
    - Insert the new entry at the top of the changelog (after the header
      section).
    - Write the file.
2.  **Update Version**:
    - Update `epsilon/version.go` with the new version string.
3.  **Commit & Tag**:
    - Run `git add CHANGELOG.md epsilon/version.go`.
    - Run `git commit -m "Release version <Version>"`.
    - Run `git tag v<Version>`.
4.  **Push**:
    - Run `git push origin main --tags`.
5.  **Finalize**:
    - Inform the user the release is committed, tagged, and pushed.
