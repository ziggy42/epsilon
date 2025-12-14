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

import argparse
import subprocess
import sys
import re
import tempfile
from pathlib import Path
from dataclasses import dataclass


_BENCHMARK_LINE_PATTERN = (
    r"Benchmark(\w+)-\d+\s+\d+\s+([\d.]+)\s+ns/op\s+([\d.]+)\s+B/op\s+([\d.]+)\s+allocs/op"
)


@dataclass
class _BenchmarkResult:
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


def _worktree(tmp: Path, ref: str) -> str:
  """Create a detached worktree for ref, return its path."""
  path = str(tmp / ref.replace("/", "_"))
  subprocess.run(
      ["git", "worktree", "add", "--detach", path, ref],
      capture_output=True,
      check=True
  )
  return path


def _resolve_path(ref: str, root: str, tmpdir: Path) -> str:
  """Resolve path for a ref (either Worktree or CWD)."""
  if ref == ".":
    return root
  return _worktree(tmpdir, ref)


def _run_benchmarks(cwd: str) -> dict[str, _BenchmarkResult]:
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

  results = {}
  for line in result.stdout.split('\n'):
    if match := re.search(_BENCHMARK_LINE_PATTERN, line):
      results[match.group(1)] = _BenchmarkResult(
          ns_per_op=float(match.group(2)),
          bytes_per_op=int(float(match.group(3))),
          allocs_per_op=int(float(match.group(4)))
      )
  return results


def _format_change(old: float, new: float) -> str:
  """Format percentage change for display."""
  percentage = ((new - old) / old * 100) if old != 0 else 0.0
  if abs(percentage) < 0.5:
    return f"⚪ {percentage:+.2f}%"
  elif percentage < 0:
    return f"🟢 {percentage:+.2f}%"
  else:
    return f"🔴 {percentage:+.2f}%"


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
    base: dict[str, _BenchmarkResult], target: dict[str, _BenchmarkResult],
) -> str:
  """Generate comparison table."""
  headers = ['Benchmark', 'Time (ns/op)', 'Δ',
             'Memory (B/op)', 'Δ', 'Allocs', 'Δ']
  rows = []
  for name in sorted(set(base.keys()) & set(target.keys())):
    b, t = base[name], target[name]
    rows.append([
        name,
        f"{b.ns_per_op:,.0f} → {t.ns_per_op:,.0f}",
        _format_change(b.ns_per_op, t.ns_per_op),
        f"{b.bytes_per_op:,} → {t.bytes_per_op:,}",
        _format_change(b.bytes_per_op, t.bytes_per_op),
        f"{b.allocs_per_op:,} → {t.allocs_per_op:,}",
        _format_change(b.allocs_per_op, t.allocs_per_op),
    ])
  return _format_table(headers, rows)


def _main():
  parser = argparse.ArgumentParser(
      description="Compare benchmarks between two git references."
  )
  parser.add_argument(
      "--base",
      default="main",
      help="Base reference (branch, commit, or '.'). Defaults to 'main'.",
  )
  parser.add_argument(
      "--target",
      required=True,
      help="Target reference (branch, commit, or '.').",
  )
  args = parser.parse_args()

  root = _git_root()

  with tempfile.TemporaryDirectory() as tmp:
    tmpdir = Path(tmp)

    try:
      base_path = _resolve_path(args.base, root, tmpdir)
      target_path = _resolve_path(args.target, root, tmpdir)

      base_results = _run_benchmarks(base_path)
      target_results = _run_benchmarks(target_path)

      print("\n" + _compare_benchmarks(base_results, target_results))
    finally:
      subprocess.run(["git", "worktree", "prune"], check=False)


if __name__ == "__main__":
  _main()
