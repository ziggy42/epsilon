---
name: security-audit
description: Use this skill to perform a security-focused audit of the codebase to identify vulnerabilities, sandbox escapes, and logic flaws.
---

# Security Audit

## Overview

Perform a rigorous security assessment of the Epsilon WebAssembly engine. The
objective is to identify exploitable vulnerabilities, verify sandbox isolation,
and ensure safe host interactions. Do not focus just on local changes, look at
the project as a whole.

## Procedure

### 1. Investigation Methodology

Your goal is to find flaws that automated tools or basic reviews miss. Do not
restrict yourself to a predefined checklist. Instead, employ a systematic,
deep-dive methodology:

1. **Select Entry Points**: Begin at the boundaries where untrusted input enters
   the system or interacts with the host. High-value starting points include the
   main execution loop (e.g., `epsilon/vm.go`), host interface boundaries (e.g.,
   `wasip1/`), or memory indexing operations.
2. **Spawn Specialized Subagents**: For complex files or distinct components,
   spawn focused subagents to audit them in isolation. Instruct these subagents
   to investigate specific mechanics deeply rather than doing a shallow read.
3. **Trace Execution Paths**: Follow the flow of untrusted data (instructions,
   indices, offsets, file descriptors) from ingestion to execution. Assume the
   input is malicious.
4. **Audit Specification Deviations**: Compare the implementation of complex
   instructions against the WebAssembly 2.0 formal execution rules. Any
   optimization that skips or defers a spec-mandated check is a potential
   vulnerability.

### 2. Verification & PoC Generation

**Never report a bug without actively triggering it locally.**

1. **Create a Vulnerability Directory**: For each confirmed flaw, create a new
   directory at `vulnerabilities/[id]/` (e.g., `vulnerabilities/VULN-01/`).
2. **Write the PoC**: Place a Go test or a minimal `.wasm` file that reliably
   triggers the flaw inside the specific vulnerability directory.
3. **Test Constraints**:
   - Do NOT write tests that invoke a single VM instance concurrently.
   - To prove state leakage, write tests that run multiple independent VMs
     concurrently using `go test -race`.
   - To prove parser crashes or bounds-check failures, write a standard Go fuzz
     test (`go test -fuzz`).

### 3. Reporting Format

If a vulnerability is confirmed locally, document it in a `README.md` file
located within the corresponding `vulnerabilities/[id]/` directory using this
exact format:

```markdown
# [Concise Title of the Issue]

**Severity:** [Critical | High | Medium | Low] **Affected Component:** [File
path and function name] **Bug Type:** [e.g., Path Traversal, OOB Slice Panic,
State Leakage]

## Root Cause

[Strictly technical explanation of the code flaw. Cite specific lines. Explain
the gap between intended logic and actual behavior.]

## Execution Flow

[Step-by-step breakdown of how the PoC triggers the bug.]

## Observable Impact

[Exact result: e.g., "Go panic: index out of range", "Race condition detected".]

## Proposed Remediation

[Provide the specific Go code diff to patch the vulnerability.]
```

## Critical Rules

1. **Isolation**: You are strictly prohibited from modifying any existing files
   in the repository. All work, including PoCs and documentation, must be
   contained within the `vulnerabilities/` directory.
2. **No Panics**: The engine must never `panic` on untrusted WASM or invalid
   WASI arguments. All bounds and inputs must be validated and returned as
   standard Go errors.
3. **No Speculation**: If you cannot write a failing Go test to prove the bug,
   do not report it.
