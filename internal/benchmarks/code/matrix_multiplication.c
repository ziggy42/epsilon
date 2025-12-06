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
emcc matrix_multiplication.c -o matrix_multiplication.wasm -s \
  EXPORTED_FUNCTIONS="['_run_matrix_multiplication']" \
  -s STANDALONE_WASM \
  -msimd128 \
  -O3 \
  --no-entry
```
 *
 * NOTE: The `-msimd128` flag is ESSENTIAL. It enables the WebAssembly
 * SIMD feature and allows the compiler to auto-vectorize the loops.
 */

// Use a matrix size that is a multiple of 4 to be friendly to
// 128-bit SIMD vectors (which hold 4 x 32-bit floats).
#define DIM 64

float matrix_a[DIM][DIM];
float matrix_b[DIM][DIM];
float result_matrix[DIM][DIM];

void multiply() {
  for (int i = 0; i < DIM; i++) {
    for (int j = 0; j < DIM; j++) {
      float sum = 0.0f;
      for (int k = 0; k < DIM; k++) {
        sum += matrix_a[i][k] * matrix_b[k][j];
      }
      result_matrix[i][j] = sum;
    }
  }
}

float run_matrix_multiplication(int iterations) {
  // Initialize matrices with some values.
  for (int i = 0; i < DIM; i++) {
    for (int j = 0; j < DIM; j++) {
      matrix_a[i][j] = 2.64 * (i - j);
      matrix_b[i][j] = 0.12 * (i + j);
    }
  }

  for (int i = 0; i < iterations; i++) {
    multiply();
  }

  // Return a result to prevent dead-code elimination.
  return result_matrix[DIM / 2][DIM / 2];
}