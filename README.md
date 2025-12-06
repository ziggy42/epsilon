# ε Epsilon

A WebAssembly virtual machine written in Go.

Implements the [WebAssembly 2.0 specification](https://webassembly.github.io/spec/versions/core/WebAssembly-2.0.pdf)
with no runtime dependencies.

## Usage

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

