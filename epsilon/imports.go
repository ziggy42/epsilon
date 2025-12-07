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

package epsilon

import "fmt"

type resolvedImports struct {
	functions []FunctionInstance
	tables    []*Table
	memories  []*Memory
	globals   []*Global
}

// resolveImports resolves the imports declared in the given module against
// the provided map of available imports.
func resolveImports(
	module *Module,
	imports map[string]map[string]any,
) (*resolvedImports, error) {
	functions := []FunctionInstance{}
	tables := []*Table{}
	memories := []*Memory{}
	globals := []*Global{}

	for _, imp := range module.Imports {
		obj, err := findImport(imports, imp.ModuleName, imp.Name)
		if err != nil {
			return nil, err
		}

		switch t := imp.Type.(type) {
		case FunctionTypeIndex:
			function, err := resolveFunctionImport(obj, module.Types[t], imp)
			if err != nil {
				return nil, err
			}
			functions = append(functions, function)
		case GlobalType:
			global, err := resolveGlobalImport(obj, t, imp)
			if err != nil {
				return nil, err
			}
			globals = append(globals, global)
		case MemoryType:
			memory, err := resolveMemoryImport(obj, t, imp)
			if err != nil {
				return nil, err
			}
			memories = append(memories, memory)
		case TableType:
			table, err := resolveTableImport(obj, t, imp)
			if err != nil {
				return nil, err
			}
			tables = append(tables, table)
		}
	}
	return &resolvedImports{
		functions: functions,
		tables:    tables,
		memories:  memories,
		globals:   globals,
	}, nil
}

func findImport(
	imports map[string]map[string]any,
	module, name string,
) (any, error) {
	if mod, ok := imports[module]; ok {
		if obj, ok := mod[name]; ok {
			return obj, nil
		}
	}
	return nil, fmt.Errorf("%s.%s not found in imports", module, name)
}

func resolveFunctionImport(
	obj any,
	functionType FunctionType,
	imp Import,
) (FunctionInstance, error) {
	if f, ok := obj.(FunctionInstance); ok {
		if !f.GetType().Equal(&functionType) {
			return nil, fmt.Errorf(
				"type mismatch for %s.%s", imp.ModuleName, imp.Name,
			)
		}
		return f, nil
	}

	if f, ok := obj.(func(...any) []any); ok {
		return &HostFunction{Type: functionType, HostCode: f}, nil
	}

	return nil, fmt.Errorf("%s.%s not a function", imp.ModuleName, imp.Name)
}

func resolveGlobalImport(
	obj any,
	globalType GlobalType,
	imp Import,
) (*Global, error) {
	if global, ok := obj.(*Global); ok {
		if global.Mutable != globalType.IsMutable {
			return nil, fmt.Errorf(
				"mutability mismatch for %s.%s", imp.ModuleName, imp.Name,
			)
		}
		if global.Type != nil && global.Type != globalType.ValueType {
			return nil, fmt.Errorf(
				"value type mismatch for %s.%s", imp.ModuleName, imp.Name,
			)
		}
		return global, nil
	}

	if !valueMatchesType(obj, globalType.ValueType) {
		return nil, fmt.Errorf(
			"value type mismatch for %s.%s", imp.ModuleName, imp.Name,
		)
	}
	return &Global{
		Value:   obj,
		Mutable: globalType.IsMutable,
		Type:    globalType.ValueType,
	}, nil
}

func resolveMemoryImport(
	obj any,
	memoryType MemoryType,
	imp Import,
) (*Memory, error) {
	memory, ok := obj.(*Memory)
	if !ok {
		return nil, fmt.Errorf("%s.%s not a memory", imp.ModuleName, imp.Name)
	}

	provided := Limits{Min: uint32(memory.Size()), Max: memory.Limits.Max}
	if !limitsMatch(provided, memoryType.Limits) {
		return nil, fmt.Errorf("limit mismatch for %s.%s", imp.ModuleName, imp.Name)
	}
	return memory, nil
}

func resolveTableImport(
	obj any,
	tableType TableType,
	imp Import,
) (*Table, error) {
	table, ok := obj.(*Table)
	if !ok {
		return nil, fmt.Errorf("%s.%s not a table", imp.ModuleName, imp.Name)
	}

	if table.Type.ReferenceType != tableType.ReferenceType {
		return nil, fmt.Errorf(
			"reference type mismatch for %s.%s", imp.ModuleName, imp.Name,
		)
	}

	provided := Limits{Min: uint32(table.Size()), Max: table.Type.Limits.Max}
	if !limitsMatch(provided, tableType.Limits) {
		return nil, fmt.Errorf(
			"limit mismatch for %s.%s", imp.ModuleName, imp.Name,
		)
	}
	return table, nil
}

func valueMatchesType(val any, valueType ValueType) bool {
	switch valueType {
	case I32:
		_, ok := val.(int32)
		return ok
	case I64:
		_, ok := val.(int64)
		return ok
	case F32:
		_, ok := val.(float32)
		return ok
	case F64:
		_, ok := val.(float64)
		return ok
	case V128:
		_, ok := val.(V128Value)
		return ok
	default:
		return false
	}
}

func limitsMatch(provided, required Limits) bool {
	if provided.Min < required.Min {
		return false
	}
	if required.Max == nil {
		return true
	}
	return provided.Max != nil && *provided.Max <= *required.Max
}
