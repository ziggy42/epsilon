// Copyright 2026 Google LLC
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

// Global is a global variable.
type Global struct {
	value   value
	Mutable bool
	Type    ValueType

	// owner identifies which vm and therefore which runtime this instance
	// belongs to.
	owner *vm
}

func newGlobal(
	owner *vm,
	v any,
	mutable bool,
	valueType ValueType,
) *Global {
	var val value
	if gv, ok := v.(value); ok {
		val = gv
	} else {
		val = newValue(v)
	}
	return &Global{
		owner:   owner,
		value:   val,
		Mutable: mutable,
		Type:    valueType,
	}
}

func (g *Global) Get() any {
	return g.value.any(g.Type)
}
