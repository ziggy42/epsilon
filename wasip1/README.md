# WASI Preview 1 Implementation

This package provides a
[WASI Preview 1](https://github.com/WebAssembly/WASI/blob/8d7fb6eff7d5964b2375beb10518d2327a1dcfe8/legacy/README.md)
(`wasi_snapshot_preview1`) implementation for the Epsilon WebAssembly runtime.

> **Note**: This implementation is experimental, use at your own risk.

## Platform Support

| Platform | Status           |
| -------- | ---------------- |
| Linux    | ✅ Supported     |
| macOS    | ✅ Supported     |
| Windows  | ❌ Not Supported |

The core WASI logic is platform-agnostic and compiles everywhere; it talks to
the host only through the `FileSystem`/`File` interfaces (see
[Filesystem](#filesystem)). The only bundled backend is the syscall-based host
backend, which is Unix-only, so on non-Unix platforms
`NewWasiModuleBuilder().Build()` (and `OpenHostFileSystem`/`NewHostFile`) return
an error unless every preopen and stream is supplied via a custom backend.

## Usage

```go
package main

import (
	"bytes"
	"os"

	"github.com/ziggy42/epsilon/epsilon"
	"github.com/ziggy42/epsilon/wasip1"
)

func main() {
	wasm, _ := os.ReadFile("guest.wasm")

	// Open a directory to pre-open for the WASM module
	fsys, _ := wasip1.OpenHostFileSystem("/path/to/sandbox")

	// Create a WASI module with args, env, and a pre-opened directory
	wasiModule, _ := wasip1.NewWasiModuleBuilder().
		WithArgs("guest.wasm", "--verbose", "--count=42").
		WithEnv("GREETING", "Hello from WASI!").
		WithEnv("USER", "wasm_user").
		WithFS("/sandbox", fsys).
		Build()
	defer wasiModule.Close()

	// Instantiate the module with WASI imports
	instance, _ := epsilon.NewRuntime().
		InstantiateModuleWithImports(bytes.NewReader(wasm), wasiModule.ToImports())

	// Invoke the _start function (WASI entry point)
	_, err := instance.Invoke(wasip1.StartFunctionName)

	// WASI modules call proc_exit(0) on success, which returns as an error.
	// Check if it's a successful exit (code 0) vs a real error.
	if exitErr, ok := err.(*wasip1.ProcExitError); ok && exitErr.Code == 0 {
		// Success
	} else if err != nil {
		panic(err)
	}
}
```

## Filesystem

The runtime accesses the host through two interfaces (see `wasi_types.go`), so
the WASI state machine is decoupled from the host OS.

- `FileSystem` is a sandboxed, directory-rooted capability. Every WASI directory
  descriptor (a preopen, or a directory from `path_open`) is one `FileSystem`.
  All names resolve relative to its root and cannot escape it.
- `File` is an open file, directory, or socket handle. It extends `io/fs.File`
  with the write, seek, and metadata operations WASI needs.

The default host backend resolves every path relative to the sandbox root with
`*at` syscalls. It blocks `..` traversal and symlink escapes, and never follows
a path outside the root.

Mount any `FileSystem` with `WithFS`. Open a host directory with
`OpenHostFileSystem`.

```go
// Open a host directory, or supply your own FileSystem.
fsys, _ := wasip1.OpenHostFileSystem("/path/to/sandbox")
builder.WithFS("/sandbox", fsys)
```

The stream methods (`WithStdin`/`WithStdout`/`WithStderr`) also take a `File`.
Wrap a host `*os.File` with `NewHostFile`. The builder takes ownership of every
`FileSystem` and `File` it is given. It never closes the process stdio.

## Testing

This implementation is tested against the official
[WASI testsuite](https://github.com/WebAssembly/wasi-testsuite).

### Prerequisites

- **[uv](https://docs.astral.sh/uv/)**: Python package manager for running the
  test runner

### Running WASI Spec Tests

From the repository root:

```bash
make test-wasi
```

## Limitations

All 45 WASI Preview 1 functions are implemented, but the following have
limitations:

### Clocks

- **`clock_time_get`**: The `process_cputime_id` and `thread_cputime_id` clocks
  return `ERRNO_NOTSUP`. Only `realtime` and `monotonic` are supported.

### Signals

- **`proc_raise`**: Always returns `ERRNO_NOTSUP`. Signal handling is not
  implemented.

### Sockets

Socket operations work with pre-opened socket file descriptors but have the
following limitations:

- **No socket creation**: There is no way to create new sockets from WASM. The
  host must pre-open a listening socket and pass it to the WASM module.
- **`sock_recv`**: The `ri_flags` parameter (e.g., `MSG_PEEK`, `MSG_WAITALL`) is
  ignored. Receive always uses standard blocking read semantics.
- **`sock_send`**: The `si_flags` parameter (e.g., `MSG_OOB`) is ignored.

### Polling

- **`poll_oneoff`**: Does not use true async I/O polling
  (`select`/`poll`/`epoll`). All file descriptors — including sockets — are
  always reported as immediately ready regardless of actual I/O readiness.
  Modules that poll a socket waiting for data will busy-loop with no sleep,
  causing 100% CPU usage. Clock subscriptions use `time.Sleep`. Hangup detection
  (`FD_READWRITE_HANGUP`) for sockets is not implemented.
