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

# Build epsilon targeting Linux amd64
cd "$REPO_ROOT"
env GOOS=linux GOARCH=amd64 go build -o "$SCRIPT_DIR/epsilon" ./cmd/epsilon
trap "rm '$SCRIPT_DIR/epsilon'" EXIT

# Build and run Docker container
cd "$SCRIPT_DIR"
docker build -q --platform linux/amd64 -t wasi-suite .
docker run \
  --platform linux/amd64 \
  --rm -it \
  -v "$SCRIPT_DIR/adapter.py:/opt/wasi-testsuite/adapters/epsilon.py" \
  -v "$SCRIPT_DIR/epsilon:/usr/local/bin/epsilon" wasi-suite:latest