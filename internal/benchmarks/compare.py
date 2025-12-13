#!/usr/bin/env python3
# Copyright 2025 Google LLC
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

"""Compares benchmark results between main branch and another branch."""

import subprocess
import sys
import re
import tempfile
from pathlib import Path
from dataclasses import dataclass


@dataclass
class BenchmarkResult:
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
  path = str(tmp / branch)
  subprocess.run(
      ["git", "worktree", "add", path, branch],
      capture_output=True,
      check=True
  )
  return path


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


def _format_table(headers: list[str], rows: list[list[str]]) -> str:
  """Format data as a markdown table."""
  widths = [len(h) for h in headers]
  for row in rows:
    for i, cell in enumerate(row):
      widths[i] = max(widths[i], len(cell))

  # Format header
  header_row = "| " + " | ".join(h.ljust(widths[i])
                                 for i, h in enumerate(headers)) + " |"
  separator = "|" + "|".join("-" * (w + 2) for w in widths) + "|"

  # Format data rows
  data_rows = []
  for row in rows:
    data_rows.append(
        "| " + " | ".join(cell.ljust(widths[i]) for i, cell in enumerate(row)) +
        " |")

  return "\n".join([header_row, separator] + data_rows)


def _compare_benchmarks(
    main: dict[str, BenchmarkResult], branch: dict[str, BenchmarkResult],
) -> str:
  """Generate comparison table."""
  headers = ['Benchmark', 'Time (ns/op)', 'Δ',
             'Memory (B/op)', 'Δ', 'Allocs', 'Δ']
  rows = []
  for name in sorted(set(main.keys()) & set(branch.keys())):
    m, b = main[name], branch[name]
    rows.append([
        name,
        f"{m.ns_per_op:,.0f} → {b.ns_per_op:,.0f}",
        _format_change(_percent_change(m.ns_per_op, b.ns_per_op)),
        f"{m.bytes_per_op:,} → {b.bytes_per_op:,}",
        _format_change(_percent_change(m.bytes_per_op, b.bytes_per_op)),
        f"{m.allocs_per_op:,} → {b.allocs_per_op:,}",
        _format_change(_percent_change(m.allocs_per_op, b.allocs_per_op)),
    ])
  return _format_table(headers, rows)


def _main():
  if len(sys.argv) != 2:
    print("usage: compare.py <branch>", file=sys.stderr)
    sys.exit(1)

  branch_name = sys.argv[1]
  current = _current_branch()
  root = _git_root()

  with tempfile.TemporaryDirectory() as tmp:
    tmpdir = Path(tmp)

    # Determine paths: use root if we're on that branch, else create worktree
    main_path = root if current == "main" else _worktree(tmpdir, "main")
    branch_path = (
        root if current == branch_name else _worktree(tmpdir, branch_name)
    )

    main_results = _run_benchmarks(main_path)
    branch_results = _run_benchmarks(branch_path)
    print(_compare_benchmarks(main_results, branch_results))

  subprocess.run(["git", "worktree", "prune"], check=False)


if __name__ == "__main__":
  _main()
