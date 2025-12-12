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
	"os"
	"strconv"
	"strings"

	"github.com/ziggy42/epsilon/epsilon"
)

const (
	prompt            = ">> "
	defaultModuleName = "default"
	colorRed          = "\033[31m"
	colorGreen        = "\033[32m"
	colorReset        = "\033[0m"
	clearScreen       = "\033[H\033[2J"

	usageLoad   = "LOAD [<module-name>] <path-to-file | url>"
	usageInvoke = "INVOKE [<module>.]<function-name> [args...]"
	usageGet    = "GET [<module>.]<global-name>"
	usageMem    = "MEM [<module>] <offset> <length>"

	helpText = "Commands:\n" +
		"  " + usageLoad + "\n" +
		"  " + usageInvoke + "\n" +
		"  " + usageGet + "\n" +
		"  " + usageMem + "\n" +
		"  LIST\n" +
		"  HELP\n" +
		"  CLEAR\n" +
		"  QUIT"
)

var (
	errNoModuleInstantiated = errors.New("no module loaded; use LOAD first")
	errLoadUsage            = errors.New("usage: " + usageLoad)
	errInvokeUsage          = errors.New("usage: " + usageInvoke)
	errGetUsage             = errors.New("usage: " + usageGet)
	errMemUsage             = errors.New("usage: " + usageMem)
)

type repl struct {
	runtime         *epsilon.Runtime
	moduleInstances map[string]*epsilon.ModuleInstance
	scanner         *bufio.Scanner
}

func startREPL() {
	repl := &repl{
		runtime:         epsilon.NewRuntime(),
		moduleInstances: make(map[string]*epsilon.ModuleInstance),
		scanner:         bufio.NewScanner(os.Stdin),
	}
	repl.run()
}

func (r *repl) run() {
	fmt.Print(prompt)

	for r.scanner.Scan() {
		parts := strings.Fields(r.scanner.Text())
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
			fmt.Println(helpText)
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
		instanceName, source = defaultModuleName, args[0]
	case 2:
		instanceName, source = args[0], args[1]
	default:
		return errLoadUsage
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
		return errInvokeUsage
	}

	funcNameArg, funcArgs := args[0], args[1:]
	module, funcName, err := r.parseItemName(funcNameArg)
	if err != nil {
		return err
	}

	result, err := parseAndInvokeFunction(module, funcName, funcArgs)
	if err != nil {
		return err
	}

	for _, r := range result {
		fmt.Println(green(fmt.Sprintf("%v", r)))
	}
	return nil
}

func (r *repl) handleGet(args []string) error {
	if len(args) != 1 {
		return errGetUsage
	}

	module, globalName, err := r.parseItemName(args[0])
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
		moduleName, offsetStr, lengthStr = defaultModuleName, args[0], args[1]
	case 3:
		moduleName, offsetStr, lengthStr = args[0], args[1], args[2]
	default:
		return errMemUsage
	}

	module, err := r.getModule(moduleName)
	if err != nil {
		return err
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

func (r *repl) handleClear() {
	fmt.Print(clearScreen)
	r.runtime = epsilon.NewRuntime()
	r.moduleInstances = make(map[string]*epsilon.ModuleInstance)
}

func (r *repl) parseItemName(
	input string,
) (*epsilon.ModuleInstance, string, error) {
	var moduleName, itemName string
	parts := strings.SplitN(input, ".", 2)
	if len(parts) == 2 {
		moduleName, itemName = parts[0], parts[1]
	} else {
		moduleName, itemName = defaultModuleName, parts[0]
	}
	module, err := r.getModule(moduleName)
	if err != nil {
		return nil, "", err
	}
	return module, itemName, nil
}

func red(s string) string {
	return fmt.Sprintf("%s%s%s", colorRed, s, colorReset)
}

func green(s string) string {
	return fmt.Sprintf("%s%s%s", colorGreen, s, colorReset)
}

func (r *repl) getModule(name string) (*epsilon.ModuleInstance, error) {
	module, ok := r.moduleInstances[name]
	if !ok {
		if name == defaultModuleName {
			return nil, errNoModuleInstantiated
		}
		return nil, fmt.Errorf("module '%s' not found", name)
	}
	return module, nil
}
