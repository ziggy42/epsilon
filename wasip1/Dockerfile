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

# This Dockerfile creates a test environment for running the WASI testsuite 
# (https://github.com/WebAssembly/wasi-testsuite) against the epsilon runtime. 
# It sets up a Linux amd64 environment to run WASI conformance tests.

FROM --platform=linux/amd64 debian:trixie-slim

ENV DEBIAN_FRONTEND=noninteractive

# Install system packages (only Python and Git are needed for the runner)
RUN apt-get update && apt-get install -y --no-install-recommends \
  ca-certificates \
  git \
  python3 \
  python3-pip \
  && rm -rf /var/lib/apt/lists/*

WORKDIR /opt/wasi-testsuite

# Clone the testsuite with pre-compiled tests
RUN git clone --depth 1 --branch prod/testsuite-base https://github.com/WebAssembly/wasi-testsuite.git . \
  && python3 -m pip install -r test-runner/requirements.txt --break-system-packages \
  && rm -rf /root/.cache/pip .git

CMD ["./run-tests", "--verbose", "--runtime", "adapters/epsilon.py"]
 