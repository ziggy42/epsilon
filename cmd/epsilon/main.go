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
	"bufio"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"

	"github.com/ziggy42/epsilon/epsilon"
)

const (
	prompt            = ">> "
	defaultModuleName = "default"
	colorRed          = "\033[31m"
	colorGreen        = "\033[32m"
	colorReset        = "\033[0m"
)

var (
	errNoModuleInstantiated = errors.New("no module loaded; use LOAD first")
)

func main() {
	// Handle CTRL-C
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-c
		os.Exit(0)
	}()

	repl := &repl{
		runtime:         epsilon.NewRuntime(),
		moduleInstances: make(map[string]*epsilon.ModuleInstance),
		scanner:         bufio.NewScanner(os.Stdin),
	}
	repl.run()
}

type repl struct {
	runtime         *epsilon.Runtime
	moduleInstances map[string]*epsilon.ModuleInstance
	scanner         *bufio.Scanner
}

func (r *repl) run() {
	fmt.Print(prompt)

	for r.scanner.Scan() {
		line := r.scanner.Text()
		parts := strings.Fields(line)
		if len(parts) == 0 {
			fmt.Print(prompt)
			continue
		}

		cmd := strings.ToUpper(parts[0])
		args := parts[1:]
		var err error

		switch cmd {
		case "LOAD":
			err = r.handleInstantiate(args)
		case "INVOKE":
			err = r.handleInvoke(args)
		case "GET":
			err = r.handleGet(args)
		case "MEM":
			err = r.handleMem(args)
		case "LIST":
			r.handleList()
		case "HELP":
			r.handleHelp()
		case "CLEAR":
			r.handleClear()
		case "QUIT":
			os.Exit(0)
		default:
			fmt.Fprintln(
				os.Stderr,
				red(fmt.Sprintf("Error: unknown command: %s", parts[0])),
			)
		}

		if err != nil {
			fmt.Fprintln(os.Stderr, red(fmt.Sprintf("Error: %s", err)))
		}
		fmt.Print(prompt)
	}
}

func (r *repl) handleInstantiate(args []string) error {
	var instanceName, source string
	switch len(args) {
	case 1:
		instanceName = defaultModuleName
		source = args[0]
	case 2:
		instanceName = args[0]
		source = args[1]
	default:
		return errors.New("usage: LOAD [<module-name>] <path-to-file | url>")
	}

	if _, ok := r.moduleInstances[instanceName]; ok {
		return fmt.Errorf("module instance '%s' already exists", instanceName)
	}

	moduleReader, err := resolveModule(source)
	if err != nil {
		return err
	}
	defer moduleReader.Close()

	instance, err := r.runtime.InstantiateModule(moduleReader)
	if err != nil {
		return err
	}
	r.moduleInstances[instanceName] = instance
	fmt.Println(green(fmt.Sprintf("'%s' instantiated.", instanceName)))
	return nil
}

func (r *repl) handleInvoke(args []string) error {
	if len(args) < 1 {
		return errors.New("usage: INVOKE [<module>.]<function-name> [args...]")
	}

	funcNameArg := args[0]
	strArgs := args[1:]

	module, funcName, err := r.parseItemName(funcNameArg)
	if err != nil {
		return err
	}

	function, err := module.GetFunction(funcName)
	if err != nil {
		return err
	}

	if len(strArgs) != len(function.GetType().ParamTypes) {
		return fmt.Errorf(
			"invalid number of arguments for %s; expected %d, got %d",
			funcName,
			len(function.GetType().ParamTypes),
			len(strArgs),
		)
	}

	var parsedArgs []any
	for i, paramType := range function.GetType().ParamTypes {
		arg, err := parseFunctionArgument(strArgs[i], paramType)
		if err != nil {
			return err
		}
		parsedArgs = append(parsedArgs, arg)
	}

	result, err := module.Invoke(funcName, parsedArgs...)
	if err != nil {
		return err
	}

	if len(result) > 0 {
		for _, r := range result {
			fmt.Println(green(fmt.Sprintf("%v", r)))
		}
	}
	return nil
}

func (r *repl) handleGet(args []string) error {
	if len(args) != 1 {
		return errors.New("usage: GET [<module>.]<global-name>")
	}
	globalNameArg := args[0]

	module, globalName, err := r.parseItemName(globalNameArg)
	if err != nil {
		return err
	}

	val, err := module.GetGlobal(globalName)
	if err != nil {
		return err
	}
	fmt.Println(green(fmt.Sprintf("%v", val)))
	return nil
}

func (r *repl) handleMem(args []string) error {
	var moduleName, offsetStr, lengthStr string

	switch len(args) {
	case 2:
		moduleName = defaultModuleName
		offsetStr = args[0]
		lengthStr = args[1]
	case 3:
		moduleName = args[0]
		offsetStr = args[1]
		lengthStr = args[2]
	default:
		return errors.New("usage: MEM [<module>] <offset> <length>")
	}

	module, ok := r.moduleInstances[moduleName]
	if !ok {
		if moduleName == defaultModuleName {
			return errNoModuleInstantiated
		}
		return fmt.Errorf("module '%s' not found", moduleName)
	}

	offset, err := strconv.ParseUint(offsetStr, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid offset: %s", offsetStr)
	}
	length, err := strconv.ParseUint(lengthStr, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid length: %s", lengthStr)
	}

	memory, err := module.GetMemory("memory")
	if err != nil {
		return err
	}
	memoryData, err := memory.Get(uint32(offset), 0, uint32(length))
	if err != nil {
		return err
	}
	fmt.Println(memoryData)
	return nil
}

func (r *repl) handleList() {
	for name, module := range r.moduleInstances {
		fmt.Println(name)
		for _, exportName := range module.ExportNames() {
			fmt.Printf("  %s\n", exportName)
		}
	}
}

func (r *repl) handleHelp() {
	helpText := `
Commands:
  LOAD [<module-name>] <path-to-file | url>
  INVOKE [<module>.]<function-name> [args...]
  GET [<module>.]<global-name>
  MEM [<module>] <offset> <length>
  LIST
  HELP
  CLEAR
  QUIT
`
	fmt.Println(strings.TrimSpace(helpText))
}

func (r *repl) handleClear() {
	fmt.Print("\033[H\033[2J")
	r.runtime = epsilon.NewRuntime()
	r.moduleInstances = make(map[string]*epsilon.ModuleInstance)
}

func (r *repl) parseItemName(
	input string,
) (*epsilon.ModuleInstance, string, error) {
	var moduleName, itemName string
	if strings.Contains(input, ".") {
		parts := strings.SplitN(input, ".", 2)
		moduleName = parts[0]
		itemName = parts[1]
	} else {
		moduleName = defaultModuleName
		itemName = input
	}

	module, ok := r.moduleInstances[moduleName]
	if !ok {
		return nil, "", fmt.Errorf("module '%s' not found", moduleName)
	}
	return module, itemName, nil
}

func parseFunctionArgument(
	argStr string,
	paramType epsilon.ValueType,
) (any, error) {
	switch paramType {
	case epsilon.I32:
		val, err := strconv.ParseInt(argStr, 10, 32)
		if err != nil {
			return nil, fmt.Errorf("failed to parse arg %s as i32: %v", argStr, err)
		}
		return int32(val), nil
	case epsilon.I64:
		val, err := strconv.ParseInt(argStr, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("failed to parse arg %s as i64: %v", argStr, err)
		}
		return val, nil
	case epsilon.F32:
		val, err := strconv.ParseFloat(argStr, 32)
		if err != nil {
			return nil, fmt.Errorf("failed to parse arg %s as f32: %v", argStr, err)
		}
		return float32(val), nil
	case epsilon.F64:
		val, err := strconv.ParseFloat(argStr, 64)
		if err != nil {
			return nil, fmt.Errorf("failed to parse arg %s as f64: %v", argStr, err)
		}
		return val, nil
	default:
		return nil, fmt.Errorf("unsupported arg type: %v", paramType)
	}
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
	case "file", "":
		return os.Open(u.Path)
	default:
		return nil, fmt.Errorf("unsupported url scheme: %s", u.Scheme)
	}
}

func red(s string) string {
	return fmt.Sprintf("%s%s%s", colorRed, s, colorReset)
}

func green(s string) string {
	return fmt.Sprintf("%s%s%s", colorGreen, s, colorReset)
}
