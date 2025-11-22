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

import "fmt"

const (
	ColorRed   = "\033[31m"
	ColorGreen = "\033[32m"
	ColorReset = "\033[0m"
)

func Red(s string) string {
	return fmt.Sprintf("%s%s%s", ColorRed, s, ColorReset)
}

func Green(s string) string {
	return fmt.Sprintf("%s%s%s", ColorGreen, s, ColorReset)
}
