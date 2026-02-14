/*
 * Copyright 2026 Google LLC
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
emcc vector_math.c -o vector_math.wasm -s \
  EXPORTED_FUNCTIONS='["_compute_vector_math"]' \
  -s STANDALONE_WASM \
  -msimd128 \
  -O3 \
  --no-entry
```
 *
 * NOTE: The `-msimd128` flag is ESSENTIAL. It enables the WebAssembly
 * SIMD feature and allows the compiler to auto-vectorize the loops.
 */

#include <math.h>
#include <stdint.h>

#define VECTOR_SIZE 128

float compute_vector_math(int iterations) {
  float input[VECTOR_SIZE];
  float output[VECTOR_SIZE];
  double double_input[VECTOR_SIZE / 2];
  double double_output[VECTOR_SIZE / 2];
  int32_t int_input[VECTOR_SIZE];
  float result = 0.0f;

  // Initialize input arrays with values that will stress different operations
  for (int i = 0; i < VECTOR_SIZE; i++) {
    input[i] = 1.0f + 0.5f * i; // Positive values for sqrt
    int_input[i] = i - 64;      // Mix of positive and negative values
  }

  for (int i = 0; i < VECTOR_SIZE / 2; i++) {
    double_input[i] = 1.0 + 0.5 * i;
  }

  for (int iter = 0; iter < iterations; iter++) {
    // Test floating-point square root operations (f32x4.sqrt)
    for (int i = 0; i < VECTOR_SIZE; i++) {
      output[i] = sqrtf(input[i]);
    }

    // Test floating-point rounding operations
    for (int i = 0; i < VECTOR_SIZE; i++) {
      float x = input[i] + 0.25f; // Add offset to make rounding meaningful
      float ceil_val = ceilf(x);
      float floor_val = floorf(x);
      float trunc_val = truncf(x);
      float round_val = roundf(x);

      // Combine to prevent optimization and test multiple operations
      output[i] += ceil_val * 0.1f + floor_val * 0.2f + trunc_val * 0.3f +
                   round_val * 0.4f;
    }

    // Test double-precision operations (f64x2.sqrt, f64x2.add, etc.)
    for (int i = 0; i < VECTOR_SIZE / 2; i++) {
      double x = double_input[i];
      double sqrt_val = sqrt(x);
      double ceil_val = ceil(x);
      double floor_val = floor(x);

      // This should generate f64x2 operations
      double_output[i] = sqrt_val + ceil_val * floor_val;
    }

    // Test type conversion operations
    for (int i = 0; i < VECTOR_SIZE; i++) {
      // int32 -> float32 conversion (should use f32x4.convert_i32x4_s)
      float float_val = (float)int_input[i];

      // Some operations that benefit from SIMD
      float processed = fabsf(float_val) + 1.0f;

      // float32 -> int32 conversion with saturation
      int32_t int_val = (int32_t)processed;

      // Use results to prevent optimization
      output[i] += float_val * 0.01f + (float)int_val * 0.02f;
    }

    // Test division and min/max operations
    for (int i = 0; i < VECTOR_SIZE; i++) {
      float x = fmaxf(input[i], 0.1f);   // f32x4.max
      float y = fminf(output[i], 10.0f); // f32x4.min
      float div_result = x / y;          // f32x4.div

      output[i] = div_result;
    }

    // Accumulate results to prevent dead-code elimination
    for (int i = 0; i < VECTOR_SIZE; i++) {
      result += output[i];
    }
    for (int i = 0; i < VECTOR_SIZE / 2; i++) {
      result += (float)double_output[i] * 0.01f;
    }
  }

  return result;
}
