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

"""Script to run the WASI testsuite with Epsilon.

This script wraps the upstream WASI test runner and configures it to use
Epsilon's runtime adapter. It acts as a replacement for the standard `run-tests`
script found in `wasip1/wasi-testsuite/run-tests`.
"""

import atexit
import subprocess
import sys
from pathlib import Path

SCRIPT_DIR = Path(__file__).parent.resolve()
REPO_ROOT = SCRIPT_DIR.parent
TEST_SUITE_DIR = SCRIPT_DIR / "wasi-testsuite"

# Add test-runner to sys.path so we can import from it
sys.path.insert(0, str(TEST_SUITE_DIR / "test-runner"))
# pylint: disable=wrong-import-position,import-error
from wasi_test_runner.harness import run_tests  # noqa: E402
from wasi_test_runner import runtime_adapter  # noqa: E402

EPSILON_BINARY = SCRIPT_DIR / ("epsilon.exe"
                               if sys.platform == "win32" else "epsilon")


def _build_epsilon() -> None:
  """Build the epsilon binary for the current platform."""
  subprocess.run(
      ["go", "build", "-o", str(EPSILON_BINARY), "./cmd/epsilon"],
      cwd=REPO_ROOT,
      check=True,
  )


def _cleanup_epsilon() -> None:
  """Remove the epsilon binary."""
  try:
    EPSILON_BINARY.unlink(missing_ok=True)
  except OSError:
    pass


def _find_test_dirs() -> list[Path]:
  """Find all test directories containing manifest.json."""
  test_dirs = []
  tests_root = TEST_SUITE_DIR / "tests"
  for root, _, files in tests_root.walk(on_error=print):
    if "manifest.json" in files:
      test_dirs.append(root)
  return test_dirs


if __name__ == "__main__":
  _build_epsilon()
  atexit.register(_cleanup_epsilon)

  sys.exit(
      run_tests(
          runtimes=[runtime_adapter.RuntimeAdapter(
              SCRIPT_DIR / "wasi_testsuite_adapter.py")],
          test_suite_paths=_find_test_dirs(),
          color=True, json_log_file=None, verbose=True,
          exclude_filters=[],))
