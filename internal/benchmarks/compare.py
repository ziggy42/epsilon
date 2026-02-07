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

"""Compares benchmark results between two git references using benchstat."""

import argparse
import subprocess
import sys
import tempfile
from pathlib import Path


_BENCHMARK_COUNT = 20
_BENCHTIME = "2s"


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
  """Resolve path for a ref (either worktree or current directory)."""
  if ref == ".":
    return root
  return _worktree(tmpdir, ref)


def _run_benchmarks(cwd: str, output_file: Path) -> None:
  """Run benchmarks and append results to file."""
  with open(output_file, "a") as f:
    # Use taskset to pin to a single core and -cpu=1 for stability.
    # We also increase benchtime to 2s to reduce variance.
    cmd = [
        "taskset", "-c", "0", "go", "test", "-bench=.", "-benchmem",
        f"-benchtime={_BENCHTIME}", "./internal/benchmarks"
    ]
    result = subprocess.run(
        cmd,
        cwd=cwd,
        stdout=f,
        stderr=subprocess.PIPE,
        text=True,
        check=False,
    )
  if result.returncode != 0:
    print(result.stderr, file=sys.stderr)
    sys.exit(1)


def _run_benchstat(base_file: Path, target_file: Path) -> None:
  """Run benchstat to compare benchmark results."""
  result = subprocess.run(
      ["go", "tool", "benchstat", base_file, target_file],
      capture_output=True,
      text=True,
      check=False,
  )
  if result.returncode != 0:
    print(result.stderr, file=sys.stderr)
    sys.exit(1)
  print(result.stdout)


def _main():
  parser = argparse.ArgumentParser(
      description="Compare benchmarks between two git references using benchstat."
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
    base_file = tmpdir / "base.txt"
    target_file = tmpdir / "target.txt"

    try:
      base_path = _resolve_path(args.base, root, tmpdir)
      target_path = _resolve_path(args.target, root, tmpdir)

      print(f"Base:   {args.base}")
      print(f"Target: {args.target}")

      for i in range(_BENCHMARK_COUNT):
        print(f"Iteration {i+1}/{_BENCHMARK_COUNT}...", end="\r")
        sys.stdout.flush()
        _run_benchmarks(base_path, base_file)
        _run_benchmarks(target_path, target_file)

      print(f"\nFinished {_BENCHMARK_COUNT} iterations.")
      print("\nBenchstat comparison:\n")
      _run_benchstat(base_file, target_file)
    finally:
      subprocess.run(["git", "worktree", "prune"], check=False)


if __name__ == "__main__":
  _main()
