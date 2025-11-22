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

package repl

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"

	"epsilon/epsilon"
)

const prompt = ">> "
const defaultModuleName = "default"

var (
	errNoModuleInstantiated = errors.New("no module loaded; use LOAD first")
	errModuleNotFound       = errors.New("module not found")
)

type UsageError struct{}

func (e *UsageError) Error() string { return "wrong command usage" }

func NewUsageError() error { return &UsageError{} }

type Command struct {
	Usage   string
	Handler func(r *Repl, args []string) error
}

type Repl struct {
	vm              *epsilon.VM
	moduleInstances map[string]*epsilon.ModuleInstance
	activeModule    string
	scanner         *bufio.Scanner
	commands        map[string]Command
}

func NewRepl() *Repl {
	return &Repl{
		vm:              epsilon.NewVM(),
		moduleInstances: make(map[string]*epsilon.ModuleInstance),
		activeModule:    defaultModuleName,
		scanner:         bufio.NewScanner(os.Stdin),
		commands: map[string]Command{
			"LOAD": {
				Usage:   "LOAD [<module-name>] <path-to-file | url>",
				Handler: (*Repl).handleInstantiate,
			},
			"USE": {
				Usage:   "USE <module-name>",
				Handler: (*Repl).handleUse,
			},
			"INVOKE": {
				Usage:   "INVOKE <function-name> [args...]",
				Handler: (*Repl).handleInvoke,
			},
			"GET": {
				Usage:   "GET <global-name>",
				Handler: (*Repl).handleGet,
			},
			"MEM": {
				Usage:   "MEM <offset> <length>",
				Handler: (*Repl).handleMem,
			},
			"/list": {
				Usage:   "/list",
				Handler: (*Repl).handleList,
			},
			"/help": {
				Usage:   "/help",
				Handler: (*Repl).handleHelp,
			},
			"/clear": {
				Usage:   "/clear",
				Handler: (*Repl).handleClear,
			},
			"/quit": {
				Usage:   "/quit",
				Handler: (*Repl).handleQuit,
			},
		},
	}
}

func Start() {
	// Handle CTRL-C
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-c
		fmt.Println("\nBye!")
		os.Exit(0)
	}()

	NewRepl().run()
}

func (r *Repl) run() {
	fmt.Print(prompt)

	for r.scanner.Scan() {
		line := r.scanner.Text()
		parts := strings.Fields(line)
		if len(parts) == 0 {
			fmt.Print(prompt)
			continue
		}

		cmdName := parts[0]
		args := parts[1:]

		if cmd, ok := r.commands[cmdName]; ok {
			if err := cmd.Handler(r, args); err != nil {
				var usageErr *UsageError
				if errors.As(err, &usageErr) {
					fmt.Fprintln(os.Stderr, Red(fmt.Sprintf("Usage: %s", cmd.Usage)))
				} else {
					fmt.Fprintln(os.Stderr, Red(fmt.Sprintf("Error: %s", err)))
				}
			}
		} else {
			fmt.Fprintln(
				os.Stderr, Red(fmt.Sprintf("Error: unknown command: %s", cmdName)),
			)
		}
		fmt.Print(prompt)
	}
}

func (r *Repl) handleInstantiate(args []string) error {
	var instanceName, source string
	switch len(args) {
	case 1:
		instanceName = defaultModuleName
		source = args[0]
	case 2:
		instanceName = args[0]
		source = args[1]
	default:
		return NewUsageError()
	}

	if _, ok := r.moduleInstances[instanceName]; ok {
		return fmt.Errorf("module instance '%s' already exists", instanceName)
	}

	moduleReader, err := ResolveModule(source)
	if err != nil {
		return err
	}
	defer moduleReader.Close()

	module, err := epsilon.NewParser(moduleReader).Parse()
	if err != nil {
		return err
	}

	instance, err := r.vm.Instantiate(module, nil)
	if err != nil {
		return err
	}
	r.moduleInstances[instanceName] = instance
	fmt.Println(Green(fmt.Sprintf("'%s' instantiated.", instanceName)))
	return nil
}

func (r *Repl) handleUse(args []string) error {
	if len(args) != 1 {
		return NewUsageError()
	}
	selectedModule := args[0]
	_, ok := r.moduleInstances[selectedModule]
	if !ok {
		return errModuleNotFound
	}

	r.activeModule = selectedModule
	return nil
}

func (r *Repl) handleInvoke(args []string) error {
	module, err := r.getActiveModule()
	if err != nil {
		return err
	}

	if len(args) < 1 {
		return NewUsageError()
	}

	funcName := args[0]
	strArgs := args[1:]

	function, err := getFunctionInstance(module, funcName)
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
		arg, err := parseArg(strArgs[i], paramType)
		if err != nil {
			return err
		}
		parsedArgs = append(parsedArgs, arg)
	}

	result, err := r.vm.Invoke(module, funcName, parsedArgs...)
	if err != nil {
		return err
	}

	if len(result) > 0 {
		for _, r := range result {
			fmt.Println(Green(fmt.Sprintf("%v", r)))
		}
	}
	return nil
}

func (r *Repl) handleGet(args []string) error {
	module, err := r.getActiveModule()
	if err != nil {
		return err
	}

	if len(args) != 1 {
		return NewUsageError()
	}
	globalName := args[0]

	val, err := r.vm.Get(module, globalName)
	if err != nil {
		return err
	}
	fmt.Println(Green(fmt.Sprintf("%v", val)))
	return nil
}

func (r *Repl) handleClear(args []string) error {
	fmt.Print("\033[H\033[2J")
	r.vm = epsilon.NewVM()
	r.moduleInstances = make(map[string]*epsilon.ModuleInstance)
	r.activeModule = defaultModuleName
	return nil
}

func (r *Repl) handleQuit(args []string) error {
	os.Exit(0)
	return nil
}

func (r *Repl) handleMem(args []string) error {
	module, err := r.getActiveModule()
	if err != nil {
		return err
	}

	if len(args) != 2 {
		return NewUsageError()
	}

	offset, err := strconv.ParseUint(args[0], 10, 64)
	if err != nil {
		return fmt.Errorf("invalid offset: %s", args[0])
	}
	length, err := strconv.ParseUint(args[1], 10, 64)
	if err != nil {
		return fmt.Errorf("invalid length: %s", args[1])
	}

	for _, export := range module.Exports {
		if export.Name == "memory" {
			memory, ok := export.Value.(*epsilon.Memory)
			if !ok {
				return fmt.Errorf("cannot find memory")
			}
			memoryData, err := memory.Get(uint32(offset), 0, uint32(length))
			if err != nil {
				return nil
			}
			fmt.Println(memoryData)
		}
	}
	return nil
}

func (r *Repl) handleList(args []string) error {
	for name, module := range r.moduleInstances {
		fmt.Println(name)
		for _, export := range module.Exports {
			fmt.Printf("  %s\n", export.Name)
		}
	}
	return nil
}

func (r *Repl) handleHelp(args []string) error {
	for _, cmd := range r.commands {
		fmt.Println(cmd.Usage)
	}
	return nil
}

func (r *Repl) getActiveModule() (*epsilon.ModuleInstance, error) {
	if len(r.moduleInstances) == 0 {
		return nil, errNoModuleInstantiated
	}

	instance, ok := r.moduleInstances[r.activeModule]
	if !ok {
		return nil, fmt.Errorf("active module '%s' not found", r.activeModule)
	}
	return instance, nil
}

func getFunctionInstance(
	module *epsilon.ModuleInstance,
	name string,
) (epsilon.FunctionInstance, error) {
	for _, exp := range module.Exports {
		if exp.Name == name {
			if f, ok := exp.Value.(epsilon.FunctionInstance); ok {
				return f, nil
			}
		}
	}
	return nil, fmt.Errorf("'%s' not found", name)
}

func parseArg(argStr string, paramType epsilon.ValueType) (any, error) {
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
