# ε Epsilon

**Epsilon** is a pure-Go library for executing WebAssembly code with **zero dependencies**.
It works on **any architecture supported by Go**: no CGo, no native libraries, just Go.

Implements the [WebAssembly 2.0 specification](https://webassembly.github.io/spec/versions/core/WebAssembly-2.0.pdf).

## Library Usage

### Installation

```bash
go get github.com/ziggy42/epsilon/epsilon
```

### Simple Example

```go
url := "https://github.com/mdn/webassembly-examples/raw/refs/heads/main/understanding-text-format/add.wasm"
resp, _ := http.Get(url)
defer resp.Body.Close()
    
instance, _ := epsilon.NewRuntime().InstantiateModule(resp.Body)    
results, _ := instance.Invoke("add", int32(5), int32(37))
    
fmt.Println(results[0]) // Output: 42
```

### Example with Imports

```go
url := "https://github.com/mdn/webassembly-examples/raw/refs/heads/main/understanding-text-format/logger.wasm"
resp, _ := http.Get(url)
defer resp.Body.Close()

imports := epsilon.NewImportBuilder().
	AddHostFunc("console", "log", func(args ...any) []any {
		fmt.Printf("%d\n", args[0].(int32))
		return []any{}
	}).
	Build()

instance, _ := epsilon.NewRuntime().
    InstantiateModuleWithImports(resp.Body, imports)
_, _ = instance.Invoke("logIt") // Output: 13

```

## REPL Usage

### Running the REPL

```bash
go run ./cmd/epsilon
```

### Commands

#### Module Management
| Command | Description |
|---------|-------------|
| `LOAD [<module-name>] <path-to-file \| url>` | Load a WASM module from a file / URL |

#### Execution
| Command | Description |
|---------|-------------|
| `INVOKE [<module>.]<function-name> [args...]` | Invoke an exported function |
| `GET [<module>.]<global-name>` | Get the value of an exported global |

#### Inspection
| Command | Description |
|---------|-------------|
| `MEM [<module>] <offset> <length>` | Inspect a range of memory |
| `LIST` | List loaded modules and their exports |

#### REPL Control
| Command | Description |
|---------|-------------|
| `HELP` | Show available commands |
| `CLEAR` | Clear the screen and reset the VM state |
| `QUIT` | Exit the REPL |

### Example Session

```
$ go run ./cmd/epsilon
>> LOAD https://github.com/mdn/webassembly-examples/raw/refs/heads/main/understanding-text-format/add.wasm
'default' instantiated.
>> INVOKE add 1 2
3
>> LOAD table https://github.com/mdn/webassembly-examples/raw/refs/heads/main/understanding-text-format/wasm-table.wasm
'table' instantiated.
>> INVOKE table.callByIndex 0
42
>> INVOKE table.callByIndex 1
13
>>
```

## Testing

### Prerequisites

Tests require the 
[WebAssembly Binary Toolkit (WABT)](https://github.com/WebAssembly/wabt) for 
`wat2wasm` and `wast2json`.

### Unit Tests

```bash
go test ./epsilon/...
```

### Spec Tests

The official WASM specification tests are included as a submodule:

```bash
go test ./internal/spec_tests/...
```

### Benchmarks

```bash
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

