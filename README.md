# ε Epsilon

[![Go Reference](https://pkg.go.dev/badge/github.com/ziggy42/epsilon.svg)](https://pkg.go.dev/github.com/ziggy42/epsilon)
[![Go Report Card](https://goreportcard.com/badge/github.com/ziggy42/epsilon)](https://goreportcard.com/report/github.com/ziggy42/epsilon)
[![License](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](https://opensource.org/licenses/Apache-2.0)

**Epsilon** is a pure Go WebAssembly runtime with zero dependencies. 

* Fully supports [WebAssembly 2.0 Specification](https://webassembly.github.io/spec/versions/core/WebAssembly-2.0.pdf)
* Runs on any architecture supported by Go (amd64, arm64, etc.) without requiring CGo
* Allows embedding WebAssembly modules in Go applications
* Includes an interactive REPL for testing and debugging

## Installation

```bash
go get github.com/ziggy42/epsilon
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

## Interactive REPL

Epsilon includes a REPL for interactively testing and debugging modules.

```bash
# Run the REPL
go run ./cmd/epsilon
```

### Essential Commands

| Category | Command | Description |
|----------|---------|-------------|
| **Loading** | `LOAD <path\|url>` | Load a module from a file or URL |
| **Running** | `INVOKE <func> [args...]` | Call an exported function |
| **State** | `GET <global>` | Read a global variable |
| **Debug** | `MEM <offset> <len>` | Inspect linear memory |
| **System** | `LIST` | List loaded modules and their exports |

**Example Session:**
```text
$ go run ./cmd/epsilon
>> LOAD https://github.com/mdn/webassembly-examples/raw/refs/heads/main/understanding-text-format/add.wasm
'default' instantiated.
>> INVOKE add 10 32
42
```

## Testing & Benchmarks

### Running Tests

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

