# ε Epsilon

[![Go Reference](https://pkg.go.dev/badge/github.com/ziggy42/epsilon.svg)](https://pkg.go.dev/github.com/ziggy42/epsilon)
[![Go Report Card](https://goreportcard.com/badge/github.com/ziggy42/epsilon)](https://goreportcard.com/report/github.com/ziggy42/epsilon)
[![License](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](https://opensource.org/licenses/Apache-2.0)

**Epsilon** is a pure Go WebAssembly runtime with zero dependencies. 

* Fully supports [WebAssembly 2.0 Specification](https://webassembly.github.io/spec/versions/core/WebAssembly-2.0.pdf)
* Runs on any architecture supported by Go (amd64, arm64, etc.) without requiring CGo
* Allows embedding WebAssembly modules in Go applications
* Includes a command-line interface and interactive REPL

## Installation

### As a Library

To use Epsilon in your Go project:

```bash
go get github.com/ziggy42/epsilon
```

### As a CLI Tool

To install the `epsilon` command-line interface:

```bash
go install github.com/ziggy42/epsilon/cmd/epsilon@latest
```

## Quick Start

### Basic Execution

Load and run a WebAssembly module directly from a byte slice:

```go
package main

import (
	"fmt"
	"os"

	"github.com/ziggy42/epsilon/epsilon"
)

func main() {
	// 1. Read the WASM file
	wasmBytes, _ := os.ReadFile("add.wasm")

	// 2. Instantiate the module
	instance, _ := epsilon.NewRuntime().InstantiateModuleFromBytes(wasmBytes)

	// 3. Invoke an exported function
	result, _ := instance.Invoke("add", int32(5), int32(37))

	fmt.Println(result[0]) // Output: 42
}
```

### Using Host Functions

Extend your WebAssembly modules with custom Go functions and more using 
`ImportBuilder`:

```go
// Create imports before instantiation
imports := epsilon.NewImportBuilder().
	AddHostFunc("env", "log", func(args ...any) []any {
		fmt.Printf("[WASM Log]: %v\n", args[0])
		return nil
	}).
	Build()

// Instantiate with imports
instance, _ := epsilon.NewRuntime().
	InstantiateModuleWithImports(wasmFile, imports)
```

## CLI

### Usage

```text
Usage:
  epsilon [options]
  epsilon <module>
  epsilon <module> <function> [args...]

Arguments:
  <module>      Path or URL to a WebAssembly module
  <function>    Name of the exported function to invoke
  [args...]     Arguments to pass to the function

Options:
  -version
        print version and exit

Examples:
  epsilon                          Start interactive REPL
  epsilon module.wasm              Instantiate a module
  epsilon module.wasm add 5 10     Invoke a function
```

### Example

```bash
$ epsilon https://github.com/mdn/webassembly-examples/raw/refs/heads/main/understanding-text-format/add.wasm add 10 32
42
```

## Development

### Building from Source

To build the CLI from source:

```bash
git clone https://github.com/ziggy42/epsilon.git
cd epsilon
go build -o bin/epsilon ./cmd/epsilon
```

### Testing & Benchmarks

#### Prerequisites

* Install [WABT](https://github.com/WebAssembly/wabt), which is required to compile WASM code defined in text format to binary.
* Fetch the spec tests submodule:
```bash
git submodule update --init --recursive
```

#### Running Tests

```bash
# Run unit tests
go test ./epsilon/...

# Run spec tests (requires git submodule)
go test ./internal/spec_tests/...

# Run benchmarks
go test -bench . ./internal/benchmarks
```

## Contributing

See [`CONTRIBUTING.md`](CONTRIBUTING.md) for details.

## License

Apache 2.0; see [`LICENSE`](LICENSE) for details.

## Disclaimer

This is not an officially supported Google product. This project is not
eligible for the [Google Open Source Software Vulnerability Rewards
Program](https://bughunters.google.com/open-source-security).

