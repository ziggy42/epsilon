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
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"

	"github.com/ziggy42/epsilon/epsilon"
)

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
		// Fallback to os.Open if we don't have a scheme.
		return os.Open(source)
	}
}

// parseAndInvokeFunction invokes the given function parsing the provided args
// to their correct types.
func parseAndInvokeFunction(
	instance *epsilon.ModuleInstance,
	functionName string,
	args []string,
) ([]any, error) {
	function, err := instance.GetFunction(functionName)
	if err != nil {
		return nil, err
	}

	paramTypes := function.GetType().ParamTypes
	if len(args) != len(paramTypes) {
		return nil, fmt.Errorf(
			"args mismatch: expected %d, got %d", len(paramTypes), len(args),
		)
	}

	parsedArgs := make([]any, len(paramTypes))
	for i, paramType := range paramTypes {
		arg, err := parseArg(args[i], paramType)
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
