// Copyright 2025 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/ziggy42/epsilon/epsilon"
	"github.com/ziggy42/epsilon/wasi_preview1"
)

func printUsage() {
	fmt.Fprintf(os.Stderr, "Usage:\n")
	fmt.Fprintf(os.Stderr, "  epsilon [options]\n")
	fmt.Fprintf(os.Stderr, "  epsilon [options] <module>\n")
	fmt.Fprintf(os.Stderr, "  epsilon [options] <module> <function> [function-args...]\n\n")
	fmt.Fprintf(os.Stderr, "Arguments:\n")
	fmt.Fprintf(os.Stderr, "  <module>           Path or URL to a WebAssembly module\n")
	fmt.Fprintf(os.Stderr, "  <function>         Name of the exported function to invoke\n")
	fmt.Fprintf(os.Stderr, "  [function-args...] Arguments to pass to the exported function\n\n")
	fmt.Fprintf(os.Stderr, "Options:\n")
	flag.PrintDefaults()
	fmt.Fprintf(os.Stderr, "\nExamples:\n")
	fmt.Fprintf(os.Stderr, "  epsilon                                     Start interactive REPL\n")
	fmt.Fprintf(os.Stderr, "  epsilon module.wasm                         Instantiate a module\n")
	fmt.Fprintf(os.Stderr, "  epsilon module.wasm add 5 10                Invoke a function\n")
	fmt.Fprintf(os.Stderr, "  epsilon -arg foo -arg bar module.wasm       Pass args to WASI module\n")
	fmt.Fprintf(os.Stderr, "  epsilon -env KEY=value module.wasm          Pass env vars to WASI module\n")
	fmt.Fprintf(os.Stderr, "  epsilon -arg foo -env KEY=val module.wasm   Pass both args and env\n")
	fmt.Fprintf(os.Stderr, "  epsilon -dir /tmp module.wasm               Pre-open a directory\n")
	fmt.Fprintf(os.Stderr, "  epsilon -dir /host:/guest module.wasm       Pre-open with path mapping\n")
}

type arrayFlags []string

// String is required to satisfy flag.Value interface.
func (a *arrayFlags) String() string {
	return strings.Join(*a, ",")
}

func (a *arrayFlags) Set(value string) error {
	*a = append(*a, value)
	return nil
}

type envFlags map[string]string

// String is required to satisfy flag.Value interface.
func (e *envFlags) String() string {
	pairs := make([]string, 0, len(*e))
	for k, v := range *e {
		pairs = append(pairs, k+"="+v)
	}
	return strings.Join(pairs, ",")
}

func (e *envFlags) Set(value string) error {
	parts := strings.SplitN(value, "=", 2)
	if len(parts) != 2 {
		return fmt.Errorf("invalid env format, expected KEY=VALUE")
	}
	(*e)[parts[0]] = parts[1]
	return nil
}

func main() {
	flag.Usage = printUsage
	version := flag.Bool("version", false, "print version and exit")
	var wasiArgs arrayFlags
	var wasiDirs arrayFlags
	wasiEnv := make(envFlags)
	flag.Var(&wasiArgs, "arg", "argument to pass to WASI module")
	flag.Var(&wasiEnv, "env", "env variable to pass to WASI module (KEY=VALUE)")
	flag.Var(&wasiDirs, "dir", "dir or file to pre-open (format: host_path or host_path:guest_path)")
	flag.Parse()

	if *version {
		fmt.Println(epsilon.Version)
		return
	}

	args := flag.Args()
	if len(args) == 0 {
		startREPL()
		return
	}

	if err := runCLI(args, wasiArgs, wasiEnv, wasiDirs); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func runCLI(
	args []string,
	wasiArgs []string,
	wasiEnv map[string]string,
	wasiDirs []string,
) error {
	modulePath := args[0]
	moduleReader, err := resolveModule(modulePath)
	if err != nil {
		return err
	}
	defer moduleReader.Close()

	// WASI expects argv[0] to be the program name (module path)
	fullArgs := append([]string{modulePath}, wasiArgs...)

	// Parse pre-opened directories with host:guest path mapping
	preopens := make([]wasi_preview1.WasiPreopenDir, 0, len(wasiDirs))
	for _, dir := range wasiDirs {
		parts := strings.SplitN(dir, ":", 2)
		hostPath := parts[0]
		guestPath := hostPath
		if len(parts) == 2 {
			guestPath = parts[1]
		}

		preopens = append(preopens, wasi_preview1.WasiPreopenDir{
			HostPath:         hostPath,
			GuestPath:        guestPath,
			Rights:           wasi_preview1.DefaultDirRights,
			RightsInheriting: wasi_preview1.DefaultDirInheritingRights,
		})
	}

	// Create WASI module
	wasiModule, err := wasi_preview1.NewWasiModule(fullArgs, wasiEnv, preopens)
	if err != nil {
		return err
	}

	// Instantiate with WASI imports
	instance, err := epsilon.NewRuntime().
		InstantiateModuleWithImports(moduleReader, wasiModule.ToImports())
	if err != nil {
		return err
	}

	if len(args) == 1 {
		return nil
	}

	funcName, funcArgs := args[1], args[2:]
	results, err := parseAndInvokeFunction(instance, funcName, funcArgs)
	if err != nil {
		return err
	}

	for _, r := range results {
		fmt.Println(r)
	}
	return nil
}
