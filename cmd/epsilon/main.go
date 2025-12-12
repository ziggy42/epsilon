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

	"github.com/ziggy42/epsilon/epsilon"
)

func printUsage() {
	fmt.Fprintf(os.Stderr, "Usage:\n")
	fmt.Fprintf(os.Stderr, "  epsilon [options]\n")
	fmt.Fprintf(os.Stderr, "  epsilon <module>\n")
	fmt.Fprintf(os.Stderr, "  epsilon <module> <function> [args...]\n\n")
	fmt.Fprintf(os.Stderr, "Arguments:\n")
	fmt.Fprintf(os.Stderr, "  <module>      Path or URL to a WebAssembly module\n")
	fmt.Fprintf(os.Stderr, "  <function>    Name of the exported function to invoke\n")
	fmt.Fprintf(os.Stderr, "  [args...]     Arguments to pass to the function\n\n")
	fmt.Fprintf(os.Stderr, "Options:\n")
	flag.PrintDefaults()
	fmt.Fprintf(os.Stderr, "\nExamples:\n")
	fmt.Fprintf(os.Stderr, "  epsilon                          Start interactive REPL\n")
	fmt.Fprintf(os.Stderr, "  epsilon module.wasm              Instantiate a module\n")
	fmt.Fprintf(os.Stderr, "  epsilon module.wasm add 5 10     Invoke a function\n")
}

func main() {
	flag.Usage = printUsage
	version := flag.Bool("version", false, "print version and exit")
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

	if err := runCLI(args); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func runCLI(args []string) error {
	modulePath := args[0]
	moduleReader, err := resolveModule(modulePath)
	if err != nil {
		return err
	}
	defer moduleReader.Close()

	instance, err := epsilon.NewRuntime().InstantiateModule(moduleReader)
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
