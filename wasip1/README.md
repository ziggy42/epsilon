# WASI Preview 1 Implementation

This package provides a [WASI Preview 1](https://github.com/WebAssembly/WASI/blob/8d7fb6eff7d5964b2375beb10518d2327a1dcfe8/legacy/README.md) 
(`wasi_snapshot_preview1`) implementation for the Epsilon WebAssembly runtime.

> **Note**: This implementation is experimental, use at your own risk.

## Platform Support

| Platform | Status |
|----------|--------|
| Linux | ✅ Supported |
| macOS | ✅ Supported |
| Windows | ❌ Not Supported |

On non-Unix platforms, `NewWasiModule()` returns an error.

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
	dir, _ := os.Open("/path/to/sandbox")

	// Create a WASI module with args, env, and pre-opened directory
	wasiModule, _ := wasip1.NewWasiModule(wasip1.WasiConfig{
		Args: []string{"guest.wasm", "--verbose", "--count=42"},
		Env: map[string]string{
			"GREETING": "Hello from WASI!",
			"USER":     "wasm_user",
		},
		Preopens: []wasip1.WasiPreopen{{
			File:             dir,
			GuestPath:        "/sandbox",
			Rights:           wasip1.DefaultDirRights,
			RightsInheriting: wasip1.DefaultDirInheritingRights,
		}},
	})
	defer wasiModule.Close()

	// Instantiate the module with WASI imports
	instance, _ := epsilon.NewRuntime().
		InstantiateModuleWithImports(bytes.NewReader(wasm), wasiModule.ToImports())

	// Invoke the _start function (WASI entry point)
	_, err := instance.Invoke("_start")

	// WASI modules call proc_exit(0) on success, which returns as an error.
	// Check if it's a successful exit (code 0) vs a real error.
	if exitErr, ok := err.(*wasip1.ProcExitError); ok && exitErr.Code == 0 {
		// Success
	} else if err != nil {
		panic(err)
	}
}
```


## Limitations

All 45 WASI Preview 1 functions are implemented, but the following have limitations:

### Clocks

- **`clock_time_get`**: The `process_cputime_id` and `thread_cputime_id` clocks 
  return `ERRNO_NOTSUP`. Only `realtime` and `monotonic` are supported.

### Signals

- **`proc_raise`**: Always returns `ERRNO_NOTSUP`. Signal handling is not implemented.

### Sockets

Socket operations work with pre-opened socket file descriptors but have the 
following limitations:

- **No socket creation**: There is no way to create new sockets from within WASM.
  The host must pre-open a listening socket and pass it to the WASM module.
- **`sock_recv`**: The `ri_flags` parameter (e.g., `MSG_PEEK`, `MSG_WAITALL`) is 
  ignored. Receive always uses standard blocking read semantics.
- **`sock_send`**: The `si_flags` parameter (e.g., `MSG_OOB`) is ignored.

### Polling

- **`poll_oneoff`**: Does not use true async I/O polling (`select`/`poll`/`epoll`).
  Regular files and directories are always reported as ready. Clock subscriptions 
  use `time.Sleep`. Hangup detection (`FD_READWRITE_HANGUP`) for sockets is not 
  implemented.
