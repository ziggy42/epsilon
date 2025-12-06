#!/usr/bin/env python3
"""Compares benchmark results between main branch and another branch."""

import subprocess
import sys
import re
import tempfile
from pathlib import Path
from dataclasses import dataclass


@dataclass
class BenchmarkResult:
  name: str
  ns_per_op: float
  bytes_per_op: int
  allocs_per_op: int


def _git_root() -> str:
  """Get git repository root directory."""
  return subprocess.run(
      ["git", "rev-parse", "--show-toplevel"],
      capture_output=True,
      text=True,
      check=True
  ).stdout.strip()


def _current_branch() -> str:
  """Get current git branch name."""
  return subprocess.run(
      ["git", "rev-parse", "--abbrev-ref", "HEAD"],
      capture_output=True,
      text=True,
      check=True
  ).stdout.strip()


def _worktree(tmp: Path, branch: str) -> str:
  """Create a worktree for branch, return its path."""
  path = tmp / branch
  result = subprocess.run(
      ["git", "worktree", "add", str(path), branch],
      capture_output=True,
      text=True
  )
  return str(path)


def _branch_exists(branch: str) -> bool:
  """Check if a git branch exists."""
  result = subprocess.run(
      ["git", "rev-parse", "--verify", branch],
      capture_output=True
  )
  return result.returncode == 0


def _run_benchmarks(cwd: str) -> dict[str, BenchmarkResult]:
  """Run benchmarks and parse results."""
  result = subprocess.run(
      ["go", "test", "-bench=.", "-benchmem", "./internal/benchmarks"],
      cwd=cwd,
      capture_output=True,
      text=True
  )
  if result.returncode != 0:
    print(result.stderr, file=sys.stderr)
    sys.exit(1)

  return _parse_output(result.stdout)


def _parse_output(output: str) -> dict[str, BenchmarkResult]:
  """Parse benchmark output."""
  pattern = r"Benchmark(\w+)-\d+\s+\d+\s+([\d.]+)\s+ns/op\s+([\d.]+)\s+B/op\s+([\d.]+)\s+allocs/op"
  results = {}
  for line in output.split('\n'):
    if match := re.search(pattern, line):
      results[match.group(1)] = BenchmarkResult(
          name=match.group(1),
          ns_per_op=float(match.group(2)),
          bytes_per_op=int(float(match.group(3))),
          allocs_per_op=int(float(match.group(4)))
      )
  return results


def _percent_change(old: float, new: float) -> float:
  """Calculate percentage change."""
  return ((new - old) / old * 100) if old != 0 else 0.0


def _format_change(value: float) -> str:
  """Format percentage change for display."""
  match value:
    case _ if abs(value) < 0.5:
      return f"⚪ {value:+.2f}%"
    case _ if value < 0:
      return f"🟢 {value:+.2f}%"
    case _:
      return f"🔴 {value:+.2f}%"


def _format_number(n: float | int) -> str:
  """Format number with thousand separators."""
  return f"{n:,.0f}" if isinstance(n, float) else f"{n:,}"


def _compare_benchmarks(
        main: dict[str, BenchmarkResult],
        branch: dict[str, BenchmarkResult],
        branch_name: str,
) -> str:
  """Generate comparison table."""
  lines = [
      f"# Benchmark: `main` vs `{branch_name}`\n",
      "| Benchmark | Time (ns/op) | Δ | Memory (B/op) | Δ | Allocs | Δ |",
      "|-----------|--------------|---|---------------|---|--------|---|"
  ]

  for name in sorted(set(main.keys()) | set(branch.keys())):
    if name not in main:
      lines.append(f"| {name} | - | ✨ NEW | - | ✨ NEW | - | ✨ NEW |")
    elif name not in branch:
      lines.append(f"| {name} | - | ❌ DEL | - | ❌ DEL | - | ❌ DEL |")
    else:
      m, b = main[name], branch[name]
      lines.append(
          f"| {name} "
          f"| {_format_number(m.ns_per_op)} → {_format_number(b.ns_per_op)} "
          f"| {_format_change(_percent_change(m.ns_per_op, b.ns_per_op))} "
          f"| {_format_number(m.bytes_per_op)} → {_format_number(b.bytes_per_op)} "
          f"| {_format_change(_percent_change(m.bytes_per_op, b.bytes_per_op))} "
          f"| {_format_number(m.allocs_per_op)} → {_format_number(b.allocs_per_op)} "
          f"| {_format_change(_percent_change(m.allocs_per_op, b.allocs_per_op))} |"
      )

  return "\n".join(lines)


def main():
  if len(sys.argv) != 2:
    print("usage: compare.py <branch>", file=sys.stderr)
    sys.exit(1)

  branch_name = sys.argv[1]
  current = _current_branch()
  root = _git_root()

  # Clean up any orphaned worktrees from interrupted runs
  subprocess.run(["git", "worktree", "prune"], capture_output=True)

  with tempfile.TemporaryDirectory() as tmp:
    tmpdir = Path(tmp)

    # Determine paths: use root if we're on that branch, else create worktree
    main_path = root if current == "main" else _worktree(tmpdir, "main")
    branch_path = (
        root if current == branch_name else _worktree(tmpdir, branch_name)
    )

    main_results = _run_benchmarks(main_path)
    branch_results = _run_benchmarks(branch_path)
    print(_compare_benchmarks(main_results, branch_results, branch_name))


if __name__ == "__main__":
  main()
