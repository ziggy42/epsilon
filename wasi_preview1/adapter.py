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

"""Adapter for running WASI testsuite with Epsilon."""

import subprocess
from pathlib import Path

# This is the path to the epsilon binary inside the Docker container
_EPSILON_BINARY = Path("/usr/local/bin/epsilon")


def get_name() -> str:
  return "epsilon"


def get_version() -> str:
  result = subprocess.run(
      [str(_EPSILON_BINARY), "--version"],
      capture_output=True,
      text=True,
      check=True
  )
  return result.stdout.strip()


def get_wasi_versions() -> list[str]:
  return ["wasm32-wasip1"]


# pylint: disable=unused-argument
def compute_argv(test_path: str,
                 args: list[str],
                 env: dict[str, str],
                 dirs: list[tuple[Path, str]],
                 wasi_version: str) -> list[str]:
  argv = [str(_EPSILON_BINARY)]
  for arg in args:
    argv.extend(["--arg", arg])
  for key, value in env.items():
    argv.extend(["--env", f"{key}={value}"])
  for host_path, guest_path in dirs:
    argv.extend(["--dir", f"{host_path}:{guest_path}"])
  argv.extend([test_path, "_start"])
  return argv
