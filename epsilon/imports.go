// Copyright 2025 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package epsilon

import "fmt"

type ResolvedImports struct {
	Functions []FunctionInstance
	Tables    []*Table
	Memories  []*Memory
	Globals   []*Global
}

// ResolveImports resolves the imports declared in the given module against
// the provided map of available imports.
func ResolveImports(
	module *Module,
	imports map[string]map[string]any,
) (*ResolvedImports, error) {
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
			f, err := resolveFunctionImport(obj, module.Types[t], imp)
			if err != nil {
				return nil, err
			}
			functions = append(functions, f)
		case GlobalType:
			g, err := resolveGlobalImport(obj, t, imp)
			if err != nil {
				return nil, err
			}
			globals = append(globals, g)
		case MemoryType:
			m, err := resolveMemoryImport(obj, t, imp)
			if err != nil {
				return nil, err
			}
			memories = append(memories, m)
		case TableType:
			tbl, err := resolveTableImport(obj, t, imp)
			if err != nil {
				return nil, err
			}
			tables = append(tables, tbl)
		}
	}
	return &ResolvedImports{
		Functions: functions,
		Tables:    tables,
		Memories:  memories,
		Globals:   globals,
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
		return &HostFunc{Type: functionType, HostCode: f}, nil
	}

	return nil, fmt.Errorf("%s.%s not a function", imp.ModuleName, imp.Name)
}

func resolveGlobalImport(
	obj any,
	globalType GlobalType,
	imp Import,
) (*Global, error) {
	if g, ok := obj.(*Global); ok {
		if g.Mutable != globalType.IsMutable {
			return nil, fmt.Errorf(
				"mutability mismatch for %s.%s", imp.ModuleName, imp.Name,
			)
		}
		if g.Type != nil && g.Type != globalType.ValueType {
			return nil, fmt.Errorf(
				"value type mismatch for %s.%s", imp.ModuleName, imp.Name,
			)
		}
		return g, nil
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
	mem, ok := obj.(*Memory)
	if !ok {
		return nil, fmt.Errorf("%s.%s not a memory", imp.ModuleName, imp.Name)
	}

	provided := Limits{Min: uint32(mem.Size()), Max: mem.Limits.Max}
	if !limitsMatch(provided, memoryType.Limits) {
		return nil, fmt.Errorf("limit mismatch for %s.%s", imp.ModuleName, imp.Name)
	}
	return mem, nil
}

func resolveTableImport(
	obj any,
	tableType TableType,
	imp Import,
) (*Table, error) {
	tbl, ok := obj.(*Table)
	if !ok {
		return nil, fmt.Errorf("%s.%s not a table", imp.ModuleName, imp.Name)
	}

	if tbl.Type.ReferenceType != tableType.ReferenceType {
		return nil, fmt.Errorf(
			"reference type mismatch for %s.%s", imp.ModuleName, imp.Name,
		)
	}

	provided := Limits{Min: uint32(tbl.Size()), Max: tbl.Type.Limits.Max}
	if !limitsMatch(provided, tableType.Limits) {
		return nil, fmt.Errorf(
			"limit mismatch for %s.%s", imp.ModuleName, imp.Name,
		)
	}
	return tbl, nil
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
