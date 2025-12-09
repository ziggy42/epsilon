package main

import (
	"fmt"
	"os"
	"path"

	"github.com/ziggy42/epsilon/epsilon"
)

func main() {
	// 1. Read the WASM file
	filename := path.Join("example", "hello", "add.wasm")
	wasmBytes, err := os.ReadFile(filename)
	if err != nil {
		fmt.Println("Error reading WASM file:", err)
		return
	}

	// 2. Instantiate the module
	instance, err := epsilon.NewRuntime().InstantiateModuleFromBytes(wasmBytes)
	if err != nil {
		fmt.Println("Error instantiating module:", err)
		return
	}

	// 3. Invoke an exported function
	result, err := instance.Invoke("add", int32(5), int32(37))
	if err != nil {
		fmt.Println("Error invoking function:", err)
		return
	}

	fmt.Println(result[0]) // Output: 42
}
