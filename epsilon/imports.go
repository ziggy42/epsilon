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
		importedModule, ok := imports[imp.ModuleName]
		if !ok {
			return nil, fmt.Errorf("missing module %s", imp.ModuleName)
		}

		importedObj, ok := importedModule[imp.Name]
		if !ok {
			return nil, fmt.Errorf(
				"%s not in module %s", imp.Name, imp.ModuleName,
			)
		}

		switch t := imp.Type.(type) {
		case FunctionTypeIndex:
			switch f := importedObj.(type) {
			case func(...any) []any:
				hostFunction := &HostFunc{Type: module.Types[t], HostCode: f}
				functions = append(functions, hostFunction)
			case FunctionInstance:
				if !f.GetType().Equal(&module.Types[t]) {
					return nil, fmt.Errorf(
						"type mismatch for import %s.%s", imp.ModuleName, imp.Name,
					)
				}
				functions = append(functions, f)
			default:
				return nil, fmt.Errorf("%s.%s not a function", imp.ModuleName, imp.Name)
			}
		case GlobalType:
			switch v := importedObj.(type) {
			case int, int32, int64, float32, float64:
				if !valueMatchesType(v, t.ValueType) {
					return nil, fmt.Errorf(
						"incompatible import type for %s.%s: value type mismatch", imp.ModuleName, imp.Name,
					)
				}
				global := &Global{Value: v, Mutable: t.IsMutable, Type: t.ValueType}
				globals = append(globals, global)
			case *Global:
				if v.Mutable != t.IsMutable {
					return nil, fmt.Errorf(
						"incompatible import type for %s.%s: mutability mismatch", imp.ModuleName, imp.Name,
					)
				}
				if v.Type != nil && v.Type != t.ValueType {
					return nil, fmt.Errorf(
						"incompatible import type for %s.%s: value type mismatch", imp.ModuleName, imp.Name,
					)
				}
				globals = append(globals, v)
			default:
				return nil, fmt.Errorf(
					"%s.%s not a valid global", imp.ModuleName, imp.Name,
				)
			}
		case MemoryType:
			memory, ok := importedObj.(*Memory)
			if !ok {
				return nil, fmt.Errorf("%s.%s not a memory", imp.ModuleName, imp.Name)
			}
			providedLimits := Limits{
				Min: uint32(memory.Size()),
				Max: memory.Limits.Max,
			}
			if !limitsMatch(providedLimits, t.Limits) {
				return nil, fmt.Errorf(
					"incompatible import type for %s.%s: limits mismatch", imp.ModuleName, imp.Name,
				)
			}
			memories = append(memories, memory)
		case TableType:
			table, ok := importedObj.(*Table)
			if !ok {
				return nil, fmt.Errorf("%s.%s not a table", imp.ModuleName, imp.Name)
			}
			if table.Type.ReferenceType != t.ReferenceType {
				return nil, fmt.Errorf(
					"incompatible import type for %s.%s: reference type mismatch", imp.ModuleName, imp.Name,
				)
			}
			providedLimits := Limits{
				Min: uint32(table.Size()),
				Max: table.Type.Limits.Max,
			}
			if !limitsMatch(providedLimits, t.Limits) {
				return nil, fmt.Errorf(
					"incompatible import type for %s.%s: limits mismatch", imp.ModuleName, imp.Name,
				)
			}
			tables = append(tables, table)
		}
	}
	return &ResolvedImports{
		Functions: functions,
		Tables:    tables,
		Memories:  memories,
		Globals:   globals,
	}, nil
}

func valueMatchesType(val any, t ValueType) bool {
	switch t {
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
	case FuncRefType, ExternRefType:
		// For now, accept anything for reference types.
		return true
	default:
		return false
	}
}

func limitsMatch(provided, required Limits) bool {
	if provided.Min < required.Min {
		return false
	}
	if required.Max != nil {
		if provided.Max == nil {
			return false
		}
		if *provided.Max > *required.Max {
			return false
		}
	}
	return true
}
