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

type options struct {
	showVersion  bool
	modulePath   string
	functionName string
	functionArgs []string
	wasiArgs     []string
	wasiEnv      []string
	wasiDirs     []string
}

func main() {
	opts, err := parseOptions()
	if err != nil {
		printUsage()
		os.Exit(1)
	}

	if opts.showVersion {
		fmt.Println(epsilon.Version)
		return
	}

	if err := run(opts); err != nil {
		var procExitErr *wasip1.ProcExitError
		if errors.As(err, &procExitErr) {
			os.Exit(int(procExitErr.Code))
		}
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func parseOptions() (*options, error) {
	flag.Usage = printUsage
	version := flag.Bool("version", false, "print version and exit")
	var wasiArgs, wasiEnv, wasiDirs []string
	flag.Func("arg", "command-line argument", func(s string) error {
		wasiArgs = append(wasiArgs, s)
		return nil
	})
	flag.Func("env", "environment variable (KEY=VALUE)", func(s string) error {
		wasiEnv = append(wasiEnv, s)
		return nil
	})
	flag.Func(
		"dir",
		"directory to mount (use /from=/to to mount at a different path)",
		func(s string) error {
			wasiDirs = append(wasiDirs, s)
			return nil
		},
	)
	flag.Parse()

	if *version {
		return &options{showVersion: true}, nil
	}

	args := flag.Args()
	if len(args) == 0 {
		return nil, errors.New("no module specified")
	}

	opts := &options{
		modulePath:   args[0],
		functionName: wasip1.StartFunctionName,
		wasiArgs:     wasiArgs,
		wasiEnv:      wasiEnv,
		wasiDirs:     wasiDirs,
	}
	if len(args) > 1 {
		opts.functionName = args[1]
		opts.functionArgs = args[2:]
	}
	return opts, nil
}

func printUsage() {
	fmt.Fprint(os.Stderr, `Usage:
  epsilon [options] <module> [function] [args...]

Options:
`)
	flag.PrintDefaults()
	fmt.Fprint(os.Stderr, `
Examples:
  epsilon module.wasm                    Run module.wasm
  epsilon module.wasm add 5 10           Call add(5, 10)
  epsilon -dir /host=/guest module.wasm  Mount /host as /guest
`)
}

func run(opts *options) error {
	moduleReader, err := resolveModule(opts.modulePath)
	if err != nil {
		return err
	}
	defer moduleReader.Close()

	wasiModule, err := newWASIModule(*opts)
	if err != nil {
		return err
	}
	defer wasiModule.Close()

	instance, err := epsilon.NewRuntime().
		InstantiateModuleWithImports(moduleReader, wasiModule.ToImports())
	if err != nil {
		return err
	}

	results, err := invokeFunction(instance, opts.functionName, opts.functionArgs)
	if err != nil {
		return err
	}

	for _, r := range results {
		fmt.Println(r)
	}
	return nil
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
		return os.Open(source)
	}
}

func newWASIModule(opts options) (*wasip1.WasiModule, error) {
	builder := wasip1.NewWasiModuleBuilder().
		WithArgs(append([]string{opts.modulePath}, opts.wasiArgs...)...)

	for _, env := range opts.wasiEnv {
		key, value, ok := strings.Cut(env, "=")
		if !ok {
			return nil, fmt.Errorf("invalid env format %q, expected KEY=VALUE", env)
		}
		builder = builder.WithEnv(key, value)
	}

	for _, dir := range opts.wasiDirs {
		hostPath, guestPath, ok := strings.Cut(dir, "=")
		if !ok {
			guestPath = hostPath
		}
		file, err := os.Open(hostPath)
		if err != nil {
			builder.Close()
			return nil, fmt.Errorf("failed to open preopen %q: %w", hostPath, err)
		}
		builder = builder.WithDir(guestPath, file)
	}

	return builder.Build()
}

func invokeFunction(
	instance *epsilon.ModuleInstance,
	functionName string,
	rawArgs []string,
) ([]any, error) {
	function, err := instance.GetFunction(functionName)
	if err != nil {
		return nil, err
	}

	paramTypes := function.GetType().ParamTypes
	if len(rawArgs) != len(paramTypes) {
		return nil, fmt.Errorf(
			"args mismatch: expected %d, got %d", len(paramTypes), len(rawArgs),
		)
	}

	parsedArgs := make([]any, len(paramTypes))
	for i, paramType := range paramTypes {
		arg, err := parseArg(rawArgs[i], paramType)
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
