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
emcc factorial.c -o factorial.wasm -s \
  EXPORTED_FUNCTIONS="['_fac_recursive', '_fac_iterative']" \
  -s STANDALONE_WASM \
  -O3 \
  --no-entry
```
 */
#include <stdint.h>

int64_t fac_recursive(int64_t n) {
  if (n == 0) {
    return 1;
  }
  return n * fac_recursive(n - 1);
}

int64_t fac_iterative(int64_t n) {
  int64_t fac = 1;
  for (int i = 2; i <=n; i++) {
    fac *= i;
  } 
  return fac;
}