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

package wabt

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type WastJSON struct {
	SourceFilename string    `json:"source_filename"`
	Commands       []Command `json:"commands"`
}

type Command struct {
	Type     string  `json:"type"`
	Line     int     `json:"line"`
	Filename string  `json:"filename,omitempty"`
	Name     string  `json:"name,omitempty"`
	Action   *Action `json:"action,omitempty"`
	Expected []Value `json:"expected,omitempty"`
	As       string  `json:"as,omitempty"`
}

type Action struct {
	Type   string  `json:"type"`
	Field  string  `json:"field"`
	Args   []Value `json:"args"`
	Module string  `json:"module,omitempty"`
}

type Value struct {
	Type     string `json:"type"`
	LaneType string `json:"lane_type,omitempty"`
	Value    any    `json:"value"`
}

func Wast2json(wastFile string) (*WastJSON, map[string][]byte, error) {
	tempDir, err := os.MkdirTemp("", "spec-test")
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	outputName := strings.TrimSuffix(filepath.Base(wastFile), ".wast")
	outputJson := filepath.Join(tempDir, outputName+".json")

	cmd := exec.Command("wast2json", wastFile, "--output="+outputJson)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, nil, fmt.Errorf("wast2json failed: %s", output)
	}

	jsonFile, err := os.Open(outputJson)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to open json file: %v", err)
	}
	defer jsonFile.Close()

	var wastJSON WastJSON
	if err := json.NewDecoder(jsonFile).Decode(&wastJSON); err != nil {
		return nil, nil, fmt.Errorf("failed to decode json: %v", err)
	}

	wasmDict := make(map[string][]byte)
	walkErr := filepath.Walk(
		tempDir,
		func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if !info.IsDir() && (strings.HasSuffix(info.Name(), ".wasm")) {
				data, err := os.ReadFile(path)
				if err != nil {
					return err
				}
				wasmDict[info.Name()] = data
			}
			return nil
		})

	if walkErr != nil {
		return nil, nil, fmt.Errorf("failed to read wasm files: %v", walkErr)
	}

	return &wastJSON, wasmDict, nil
}
