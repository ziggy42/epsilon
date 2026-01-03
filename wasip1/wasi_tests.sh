#!/usr/bin/env bash
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

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
TEST_SUITE_DIR="$SCRIPT_DIR/wasi-testsuite"

# Ensure the submodule is initialized and updated
if [ ! -f "$TEST_SUITE_DIR/run-tests" ]; then
  echo "Initializing wasi-testsuite submodule..."
  git submodule update --init --recursive "$SCRIPT_DIR/wasi-testsuite"
fi

# Build epsilon for the current host
echo "Building epsilon..."
cd "$REPO_ROOT"
go build -o "$SCRIPT_DIR/epsilon" ./cmd/epsilon
trap "rm '$SCRIPT_DIR/epsilon'" EXIT

# Run the tests using uv
echo "Running WASI testsuite..."
cd "$TEST_SUITE_DIR"
uv run \
  --with-requirements test-runner/requirements/common.txt \
  ./run-tests \
  --runtime "$SCRIPT_DIR/adapter.py" \
  "$@"
