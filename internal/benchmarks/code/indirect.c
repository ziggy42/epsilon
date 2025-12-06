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
emcc indirect.c -o indirect.wasm -s \
  EXPORTED_FUNCTIONS="['_run_indirect_calls']" \
  -s STANDALONE_WASM \
  -O3 \
  --no-entry
```
 */

int add(int a, int b) { return a + b; }
int sub(int a, int b) { return a - b; }
int mul(int a, int b) { return a * b; }
int xor_op(int a, int b) { return a ^ b; }

typedef int (*binary_op)(int, int);

binary_op operations[] = {&add, &sub, &mul, &xor_op};

#define NUM_OPS 4

int run_indirect_calls(int iterations) {
  int result = 0;
  for (int i = 0; i < iterations; i++) {
    // Cycle through the functions in the operations table.
    // This forces the VM to use `call_indirect`.
    binary_op op = operations[i % NUM_OPS];
    result = op(result, i);
  }
  return result;
}