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
emcc fibonacci.c -o fibonacci.wasm -s \
  EXPORTED_FUNCTIONS="['_fib_recursive', '_fib_iterative']" \
  -s STANDALONE_WASM \
  -O3 \
  --no-entry
```
 */


int fib_recursive(int n) {
  if (n == 0) {
    return 0;
  }

  if (n == 1) {
    return 1;
  }

  return fib_recursive(n - 1) + fib_recursive(n - 2);
}

int fib_iterative(int n) {
  if (n == 0) {
    return 0;
  }

  if (n == 1) {
    return 1;
  }

  int a = 0;
  int b = 1;
  for (int i = 2; i <= n; i++) {
    int next = a + b;
    a = b;
    b = next;
  }
  return b;
}