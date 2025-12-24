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

FROM --platform=linux/amd64 debian:trixie-slim

ENV DEBIAN_FRONTEND=noninteractive

RUN apt-get update && apt-get install -y --no-install-recommends \
  build-essential \
  curl \
  ca-certificates \
  git \
  nodejs \
  npm \
  python3 \
  python3-pip \
  && rm -rf /var/lib/apt/lists/*

# Install Rust and WASI targets
RUN curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh -s -- -y
ENV PATH="/root/.cargo/bin:${PATH}"
RUN rustup target add wasm32-wasip1 \
  && rustup toolchain install nightly \
  && rustup target add wasm32-wasip2 --toolchain nightly \
  && rm -rf /root/.cargo/registry /root/.cargo/git

# Install WASI-SDK
RUN curl -LO https://github.com/WebAssembly/wasi-sdk/releases/download/wasi-sdk-29/wasi-sdk-29.0-x86_64-linux.deb \
  && dpkg -i wasi-sdk-29.0-x86_64-linux.deb || apt-get install -f -y \
  && rm wasi-sdk-29.0-x86_64-linux.deb
ENV PATH="/opt/wasi-sdk/bin:${PATH}"

RUN git clone --depth 1 https://github.com/WebAssembly/wasi-testsuite.git /opt/wasi-testsuite

WORKDIR /opt/wasi-testsuite

RUN pip3 install -r test-runner/requirements.txt --break-system-packages \
  && rm -rf /root/.cache/pip

RUN ./tests/c/build.py && \
  ./tests/rust/build.py && \
  ./tests/assemblyscript/build.py

CMD ["./run-tests", "--verbose", "--runtime", "adapters/epsilon.py"]