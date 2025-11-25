# ε Epsilon

A WebAssembly virtual machine written in Go.

Implements the [WebAssembly 2.0 specification](https://webassembly.github.io/spec/versions/core/WebAssembly-2.0.pdf)
with no runtime dependencies.

## Usage

### Running the REPL

```bash
go run main.go
```

### Commands

#### Module Management
| Command | Description |
|---------|-------------|
| `LOAD [<module-name>] <path-to-file \| url>` | Load a WASM module from a file / URL |
| `USE <module-name>` | Switch to a loaded module |

#### Execution
| Command | Description |
|---------|-------------|
| `INVOKE <function-name> [args...]` | Invoke an exported function |
| `GET <global-name>` | Get the value of an exported global |

#### Inspection
| Command | Description |
|---------|-------------|
| `MEM <offset> <length>` | Inspect a range of memory in the active module |
| `/list` | List loaded modules and their exports |

#### REPL Control
| Command | Description |
|---------|-------------|
| `/help` | Show available commands |
| `/clear` | Clear the screen and reset the VM state |
| `/quit` | Exit the REPL |

### Example Session

```
$ go run main.go
>> LOAD https://github.com/mdn/webassembly-examples/raw/refs/heads/main/understanding-text-format/add.wasm
'default' instantiated.
>> INVOKE add 1 2
3
>> LOAD table https://github.com/mdn/webassembly-examples/raw/refs/heads/main/understanding-text-format/wasm-table.wasm
'table' instantiated.
>> USE table
>> INVOKE callByIndex 0
42
>> INVOKE callByIndex 1
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
go test ./spec_tests/...
```

### Benchmarks

```bash
go test -bench . ./benchmarks
```

## Contributing

See [`CONTRIBUTING.md`](CONTRIBUTING.md) for details.

## License

Apache 2.0; see [`LICENSE`](LICENSE) for details.

## Disclaimer

This is not an officially supported Google product. This project is not
eligible for the [Google Open Source Software Vulnerability Rewards
Program](https://bughunters.google.com/open-source-security).

