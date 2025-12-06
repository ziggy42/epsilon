/*
 * Copyright 2025 Google LLC
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

/** Compile with:
```
emcc trigonometry.c -o trigonometry.wasm -s \
  EXPORTED_FUNCTIONS="['_compute_sin']" \
  -s STANDALONE_WASM \
  -O3 \
  --no-entry
```
 */
#include <math.h>

float compute_sin(float n) {
  return sin(n);
}