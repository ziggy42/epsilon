# Epsilon — single entry point for build, test, bench, and toolchain setup.
#
# Run `make` (or `make help`) to list available targets. Variables can be
# overridden via the environment, e.g.:
#   make WASI_SDK_DIR=/opt/wasi-sdk-33 build-wasm
#   make BENCHTIME=2s bench
#
# Every target carries a `## description` that `make help` extracts. No
# interactive prompts. Errors point at the fix.

.DEFAULT_GOAL := help

WASI_SDK_VERSION ?= 33
WABT_VERSION ?= 1.0.41

# ----- platform detection -----------------------------------------------------

UNAME_S := $(shell uname -s)
UNAME_M := $(shell uname -m)

ifeq ($(UNAME_S),Darwin)
  WASI_SDK_OS := macos
endif
ifeq ($(UNAME_S),Linux)
  WASI_SDK_OS := linux
endif

ifeq ($(UNAME_M),arm64)
  WASI_SDK_ARCH := arm64
else ifeq ($(UNAME_M),aarch64)
  WASI_SDK_ARCH := arm64
else
  WASI_SDK_ARCH := x86_64
endif

# WABT platform detection (prebuilt binaries for macos-arm64, linux-arm64/x64)
ifeq ($(UNAME_S),Darwin)
  ifeq ($(UNAME_M),arm64)
    WABT_NAME := wabt-$(WABT_VERSION)-macos-arm64
  endif
endif
ifeq ($(UNAME_S),Linux)
  ifeq ($(UNAME_M),arm64)
    WABT_NAME := wabt-$(WABT_VERSION)-linux-arm64
  else ifeq ($(UNAME_M),aarch64)
    WABT_NAME := wabt-$(WABT_VERSION)-linux-arm64
  else
    WABT_NAME := wabt-$(WABT_VERSION)-linux-x64
  endif
endif

# ----- variables --------------------------------------------------------------

WASI_SDK_DIR ?= .toolchain/wasi-sdk
WASI_SDK_CLANG := $(WASI_SDK_DIR)/bin/clang
WASI_SDK_NAME := wasi-sdk-$(WASI_SDK_VERSION).0-$(WASI_SDK_ARCH)-$(WASI_SDK_OS)
WASI_SDK_H := https://github.com/WebAssembly/wasi-sdk/releases/download
WASI_SDK_P := wasi-sdk-$(WASI_SDK_VERSION)/$(WASI_SDK_NAME).tar.gz
WASI_SDK_URL := $(WASI_SDK_H)/$(WASI_SDK_P)

WABT_DIR ?= .toolchain/wabt
WABT_WAT2WASM := $(WABT_DIR)/bin/wat2wasm
WABT_H := https://github.com/WebAssembly/wabt/releases/download
WABT_P := $(WABT_VERSION)/$(WABT_NAME).tar.gz
WABT_URL := $(WABT_H)/$(WABT_P)

BENCH_PATTERN  ?= .
BENCH_TIME     ?= 1s
BENCH_COUNT    ?= 3

WASM_SRC_DIR := internal/benchmarks/src
WASM_OUT_DIR := internal/benchmarks/wasm

WASM_TARGETS := $(basename $(notdir $(wildcard $(WASM_SRC_DIR)/*.c)))

# Exports are declared via __attribute__((export_name(...))) in the .c source.
# Only memory_access needs an extra linker flag (initial linear memory size
# for its two 16 MiB buffers).
LDFLAGS_memory_access := -Wl,--initial-memory=39321600

# SIMD is enabled per-file. Enabling it globally would auto-vectorize other
# benchmarks, which would change what those benchmarks are designed to
# exercise.
CFLAGS_matrix_multiplication := -msimd128
CFLAGS_vector_math           := -msimd128

WASM_CFLAGS := --target=wasm32-wasip1 -O3 -mexec-model=reactor -Wl,--strip-all

WASM_OUTPUTS := $(addprefix $(WASM_OUT_DIR)/,$(addsuffix .wasm,$(WASM_TARGETS)))

# ----- help (default target) --------------------------------------------------

help: ## Show this help
	@printf "Usage: make <target>\n\nTargets:\n"
	@grep -E '^[a-zA-Z][a-zA-Z0-9_-]+:.*## ' $(MAKEFILE_LIST) \
	  | awk -F':.*## ' '{printf "  %-20s  %s\n", $$1, $$2}'
	@printf "\nCommon overrides:\n"
	@printf "  %-20s  %s\n" \
	  "BENCH_PATTERN=<pat>"  "go test -bench filter (default: $(BENCH_PATTERN))" \
	  "BENCH_TIME=<dur>"     "go test -benchtime (default: $(BENCH_TIME))" \
	  "BENCH_COUNT=<n>"      "iterations (default: $(BENCH_COUNT))" \
	  "WASI_SDK_DIR=<p>"     "wasi-sdk path (default: $(WASI_SDK_DIR))"

# ----- daily targets ----------------------------------------------------------

build: ## Compile all Go packages
	go build ./...

build-all: build ## Cross-compile the CLI for Linux, Darwin, and Windows
	GOOS=linux go build -o epsilon-linux ./cmd/epsilon
	GOOS=darwin go build -o epsilon-darwin ./cmd/epsilon
	GOOS=windows go build -o epsilon.exe ./cmd/epsilon

test: setup-wabt ## Run all Go tests (unit + spec)
	go test ./...

run-example: ## Run the basic example (smoke check)
	go run ./example/hello

fmt: ## Run gofmt across the tree
	go fmt ./...

vet: ## Run go vet across the tree
	go vet ./...

clean: ## Remove built artifacts (keeps the wasi-sdk toolchain)
	go clean ./...
	rm -f epsilon-linux epsilon-darwin epsilon.exe cpu.prof
	@if [ -f epsilon ]; then rm -f epsilon; fi
	rm -f internal/benchmarks/benchmarks.test
	rm -rf $(WASM_OUT_DIR)

distclean: clean ## Remove built artifacts AND the wasi-sdk toolchain
	rm -rf .toolchain

# ----- specialized test suites ------------------------------------------------

test-spec: internal/spec_tests/testsuite/.git setup-wabt ## Run wasm spec tests
	go test ./internal/spec_tests/...

test-wasi: wasip1/wasi-testsuite/.git ## Run the WASI testsuite (needs uv)
	@command -v uv >/dev/null 2>&1 || { \
	  echo "Error: 'uv' is not installed." && \
	  echo "See https://docs.astral.sh/uv/ for instructions." && \
	  exit 1; \
	}
	uv run --with-requirements requirements.txt wasip1/wasi_testsuite.py

test-all: test test-wasi ## Run all tests (Go tests + WASI spec tests)

# ----- benchmarks -------------------------------------------------------------

bench: build-wasm ## Run benchmarks (vars: BENCH_PATTERN, etc.)
	go test -bench=$(BENCH_PATTERN) -benchmem -benchtime=$(BENCH_TIME) \
	  -count=$(BENCH_COUNT) ./internal/benchmarks

bench-compare: ## Compare benchmarks across refs; TARGET=<ref> required
ifndef TARGET
	$(error TARGET is required. Example: make bench-compare TARGET=my-branch)
endif
	python3 internal/benchmarks/compare.py --target=$(TARGET) \
	  $(if $(BASE),--base=$(BASE),) \
	  $(if $(BENCH_COUNT),--count=$(BENCH_COUNT),) \
	  $(if $(filter-out .,$(BENCH_PATTERN)),--bench=$(BENCH_PATTERN),)

# ----- benchmark .wasm builds -------------------------------------------------

build-wasm: ## Rebuild benchmark .wasm files (auto-installs wasi-sdk)
build-wasm: $(WASM_OUTPUTS)

$(WASM_OUTPUTS): $(WASM_OUT_DIR)/%.wasm: \
    $(WASM_SRC_DIR)/%.c $(WASI_SDK_CLANG) | $(WASM_OUT_DIR)
	$(WASI_SDK_CLANG) $(WASM_CFLAGS) $(CFLAGS_$*) $(LDFLAGS_$*) -o $@ $<

$(WASM_OUT_DIR):
	@mkdir -p $@

# ----- toolchain --------------------------------------------------------------

setup-wasi-sdk: ## Install wasi-sdk locally (~600 MB, one-time)
setup-wasi-sdk: $(WASI_SDK_CLANG)

$(WASI_SDK_CLANG):
ifndef WASI_SDK_OS
	$(error Unsupported OS '$(UNAME_S)'. Only macOS and Linux are supported)
endif
	@echo "==> Downloading $(WASI_SDK_NAME)"
	@mkdir -p .toolchain
	@curl -fL --progress-bar -o .toolchain/$(WASI_SDK_NAME).tar.gz \
	  $(WASI_SDK_URL)
	@tar -xzf .toolchain/$(WASI_SDK_NAME).tar.gz -C .toolchain
	@rm -rf $(WASI_SDK_DIR)
	@mv .toolchain/$(WASI_SDK_NAME) $(WASI_SDK_DIR)
	@rm -f .toolchain/$(WASI_SDK_NAME).tar.gz
	@echo "==> wasi-sdk installed at $(WASI_SDK_DIR)"

setup-wabt: ## Install WABT locally (one-time)
setup-wabt: $(WABT_WAT2WASM)

$(WABT_WAT2WASM):
ifndef WABT_NAME
	$(error Prebuilt WABT is not available on $(UNAME_S)-$(UNAME_M))
endif
	@echo "==> Downloading WABT $(WABT_VERSION)"
	@mkdir -p .toolchain
	@curl -fL --progress-bar -o .toolchain/$(WABT_NAME).tar.gz \
	  $(WABT_URL)
	@tar -xzf .toolchain/$(WABT_NAME).tar.gz -C .toolchain
	@rm -rf $(WABT_DIR)
	@mv .toolchain/wabt-$(WABT_VERSION) $(WABT_DIR)
	@rm -f .toolchain/$(WABT_NAME).tar.gz
	@echo "==> WABT installed at $(WABT_DIR)"

# ----- submodule init ---------------------------------------------------------

internal/spec_tests/testsuite/.git wasip1/wasi-testsuite/.git:
	git submodule update --init --recursive

# ----- phony declarations -----------------------------------------------------

.PHONY: help build build-all test run-example fmt vet clean distclean \
        test-spec test-wasi test-all \
        bench bench-compare \
        build-wasm setup-wasi-sdk setup-wabt
