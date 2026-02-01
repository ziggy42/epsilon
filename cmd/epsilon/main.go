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
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/ziggy42/epsilon/epsilon"
	"github.com/ziggy42/epsilon/wasip1"
)

func printUsage() {
	fmt.Fprintf(os.Stderr, "Usage:\n")
	fmt.Fprintf(os.Stderr, "  epsilon [options] <module>\n")
	fmt.Fprintf(os.Stderr, "  epsilon [options] <module> <function> [function-args...]\n\n")
	fmt.Fprintf(os.Stderr, "Arguments:\n")
	fmt.Fprintf(os.Stderr, "  <module>           Path or URL to a WebAssembly module\n")
	fmt.Fprintf(os.Stderr, "  <function>         Name of the exported function to invoke\n")
	fmt.Fprintf(os.Stderr, "  [function-args...] Arguments to pass to the exported function\n\n")
	fmt.Fprintf(os.Stderr, "Options:\n")
	flag.PrintDefaults()
	fmt.Fprintf(os.Stderr, "\nExamples:\n")
	fmt.Fprintf(os.Stderr, "  epsilon module.wasm                         Run a WASI module\n")
	fmt.Fprintf(os.Stderr, "  epsilon module.wasm add 5 10                Invoke a function\n")
	fmt.Fprintf(os.Stderr, "  epsilon -arg foo -arg bar module.wasm       Pass args to WASI module\n")
	fmt.Fprintf(os.Stderr, "  epsilon -env KEY=value module.wasm          Pass env vars to WASI module\n")
	fmt.Fprintf(os.Stderr, "  epsilon -arg foo -env KEY=val module.wasm   Pass both args and env\n")
	fmt.Fprintf(os.Stderr, "  epsilon -dir /tmp module.wasm               Pre-open a directory\n")
	fmt.Fprintf(os.Stderr, "  epsilon -dir /host=/guest module.wasm       Pre-open with path mapping\n")
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
	flag.Var(&wasiDirs, "dir", "dir or file to pre-open (format: host_path or host_path=guest_path)")
	flag.Parse()

	if *version {
		fmt.Println(epsilon.Version)
		return
	}

	args := flag.Args()
	if len(args) == 0 {
		printUsage()
		os.Exit(1)
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

	// Create WASI module
	builder := wasip1.NewWasiModuleBuilder().
		// WASI expects argv[0] to be the program name
		WithArgs(append([]string{modulePath}, wasiArgs...)...).
		WithEnvMap(wasiEnv)

	// Parse pre-opened directories with host:guest path mapping
	for _, dir := range wasiDirs {
		hostPath, guestPath := extractHostGuestPaths(dir)
		file, err := os.Open(hostPath)
		if err != nil {
			builder.Close()
			return fmt.Errorf("failed to open preopen %q: %w", hostPath, err)
		}
		builder = builder.WithDir(guestPath, file)
	}

	wasiModule, err := builder.Build()
	if err != nil {
		return err
	}
	defer wasiModule.Close()

	instance, err := epsilon.NewRuntime().
		InstantiateModuleWithImports(moduleReader, wasiModule.ToImports())
	if err != nil {
		return err
	}

	funcName := wasip1.StartFunctionName
	var funcArgs []string
	if len(args) > 1 {
		funcName = args[1]
		funcArgs = args[2:]
	}
	results, err := parseAndInvokeFunction(instance, funcName, funcArgs)
	if err != nil {
		var procExitErr *wasip1.ProcExitError
		if errors.As(err, &procExitErr) {
			os.Exit(int(procExitErr.Code))
		}
		return err
	}

	for _, r := range results {
		fmt.Println(r)
	}
	return nil
}

func extractHostGuestPaths(arg string) (string, string) {
	parts := strings.SplitN(arg, "=", 2)
	if len(parts) == 2 {
		return parts[0], parts[1]
	}
	return parts[0], parts[0]
}

func resolveModule(source string) (io.ReadCloser, error) {
	u, err := url.Parse(source)
	if err != nil {
		return nil, err
	}

	switch u.Scheme {
	case "http", "https":
		resp, err := http.Get(u.String())
		if err != nil {
			return nil, fmt.Errorf("http request failed: %w", err)
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			resp.Body.Close()
			return nil, fmt.Errorf("unexpected http status: %s", resp.Status)
		}
		return resp.Body, nil
	case "file":
		return os.Open(u.Path)
	default:
		// Fallback to os.Open if we don't have a scheme.
		return os.Open(source)
	}
}

// parseAndInvokeFunction invokes the given function parsing the provided args
// to their correct types.
func parseAndInvokeFunction(
	instance *epsilon.ModuleInstance,
	functionName string,
	args []string,
) ([]any, error) {
	function, err := instance.GetFunction(functionName)
	if err != nil {
		return nil, err
	}

	paramTypes := function.GetType().ParamTypes
	if len(args) != len(paramTypes) {
		return nil, fmt.Errorf(
			"args mismatch: expected %d, got %d", len(paramTypes), len(args),
		)
	}

	parsedArgs := make([]any, len(paramTypes))
	for i, paramType := range paramTypes {
		arg, err := parseArg(args[i], paramType)
		if err != nil {
			return nil, err
		}
		parsedArgs[i] = arg
	}

	return instance.Invoke(functionName, parsedArgs...)
}

func parseArg(raw string, t epsilon.ValueType) (any, error) {
	switch t {
	case epsilon.I32:
		v, err := strconv.ParseInt(raw, 10, 32)
		return int32(v), err
	case epsilon.I64:
		return strconv.ParseInt(raw, 10, 64)
	case epsilon.F32:
		v, err := strconv.ParseFloat(raw, 32)
		return float32(v), err
	case epsilon.F64:
		return strconv.ParseFloat(raw, 64)
	default:
		return nil, fmt.Errorf("unsupported type: %v", t)
	}
}
