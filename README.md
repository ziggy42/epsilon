# ε Epsilon

A [WASM](https://webassembly.org/) virtual machine written in Go, currently implementing the [WebAssembly 2.0 specification](https://webassembly.github.io/spec/versions/core/WebAssembly-2.0.pdf).

## Usage

### Running the REPL

You can run the REPL directly using `go run`:
```bash
go run main.go
```

### Commands

The REPL supports the following commands:

| Command | Description |
|---|---|
| `LOAD [<module-name>] <path-to-file \| url>` | Load a WASM module from a file or URL. |
| `USE <module-name>` | Switch to a loaded module. |
| `INVOKE <function-name> [args...]` | Invoke an exported function. |
| `GET <global-name>` | Get the value of an exported global. |
| `MEM <offset> <length>` | Inspect a range of memory in the active module. |
| `/list` | List loaded modules and their exports. |
| `/help` | Show available commands. |
| `/clear` | Clear the screen and reset the VM state. |
| `/quit` | Exit the REPL. |

### Example

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

To run the tests, you will need to install the
[WebAssembly Binary Toolkit (WABT)](https://github.com/WebAssembly/wabt).
This provides the `wat2wasm` and `wast2json` tools used in the test suites.

### Unit Tests

Run all unit tests:
```bash
go test ./epsilon/...
```

### Spec Tests

This project includes the official WebAssembly specification tests as a
submodule. To run these tests:
```bash
go test ./spec_tests/...
```

### Benchmarks

Run all benchmarks:
```bash
go test -bench . ./benchmarks
```

## Specs deviations

* **NaN Payloads**: The specification has precise rules for managing `NaN`
payloads (the specific bit pattern within a `NaN`), including propagating them
from inputs or using a "canonical" form. This implementation does not inspect or
manipulate payloads, instead using Go's default `NaN` behavior.
* **NaN Sign**: The specification requires a non-deterministic sign for
resulting NaNs. This implementation produces a deterministic sign based on the
operation and underlying hardware.

## Contributing

See [`CONTRIBUTING.md`](CONTRIBUTING.md) for details.

## License

Apache 2.0; see [`LICENSE`](LICENSE) for details.

## Disclaimer

This is not an officially supported Google product. This project is not
eligible for the [Google Open Source Software Vulnerability Rewards
Program](https://bughunters.google.com/open-source-security).

