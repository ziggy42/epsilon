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

/* NOTE: Compiled with `-msimd128`. SIMD auto-vectorization is essential —
 * without it the inner loops run scalar.
 */

#include <math.h>
#include <stdint.h>

#define VECTOR_SIZE 128

__attribute__((export_name("compute_vector_math"))) float compute_vector_math(
    int iterations) {
  float input[VECTOR_SIZE];
  float output[VECTOR_SIZE];
  double double_input[VECTOR_SIZE / 2];
  double double_output[VECTOR_SIZE / 2];
  int32_t int_input[VECTOR_SIZE];
  float result = 0.0f;

  // Initialize input arrays with values that will stress different operations
  for (int i = 0; i < VECTOR_SIZE; i++) {
    input[i] = 1.0f + 0.5f * i;  // Positive values for sqrt
    int_input[i] = i - 64;       // Mix of positive and negative values
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
      float x = input[i] + 0.25f;  // Add offset to make rounding meaningful
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
      float x = fmaxf(input[i], 0.1f);    // f32x4.max
      float y = fminf(output[i], 10.0f);  // f32x4.min
      float div_result = x / y;           // f32x4.div

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

// The same set of element-wise operations applied to one lane width. `width`
// is the C type (int8_t, int16_t, ...) and `acc` is the running result. With
// -msimd128 each loop becomes the matching i8x16 / i16x8 / i32x4 / i64x2 SIMD
// instruction. `mul` is gated because 8-bit lanes have no SIMD multiply.
#define LANE_OPS(width, a, b, out, acc, mul)                          \
  do {                                                                \
    for (int i = 0; i < VECTOR_SIZE; i++)                             \
      out[i] = (mul) ? (width)(a[i] * b[i]) : (width)(a[i] + b[i]);   \
    for (int i = 0; i < VECTOR_SIZE; i++) out[i] += a[i] - b[i];      \
    for (int i = 0; i < VECTOR_SIZE; i++)                             \
      out[i] += a[i] < b[i] ? a[i] : b[i]; /* min */                  \
    for (int i = 0; i < VECTOR_SIZE; i++)                             \
      out[i] += a[i] > b[i] ? a[i] : b[i]; /* max */                  \
    for (int i = 0; i < VECTOR_SIZE; i++)                             \
      out[i] ^= a[i] & b[i]; /* and, then xor into the accumulator */ \
    for (int i = 0; i < VECTOR_SIZE; i++) out[i] |= b[i]; /* or */    \
    for (int i = 0; i < VECTOR_SIZE; i++)                             \
      out[i] += b[i] << 1; /* shift left (b is positive) */           \
    for (int i = 0; i < VECTOR_SIZE; i++)                             \
      out[i] += a[i] >> 1; /* shift right */                          \
    for (int i = 0; i < VECTOR_SIZE; i++) acc += out[i];              \
  } while (0)

__attribute__((export_name("compute_integer_vector_math"))) int64_t
compute_integer_vector_math(int iterations) {
  int8_t a8[VECTOR_SIZE], b8[VECTOR_SIZE], o8[VECTOR_SIZE];
  int16_t a16[VECTOR_SIZE], b16[VECTOR_SIZE], o16[VECTOR_SIZE];
  int32_t a32[VECTOR_SIZE], b32[VECTOR_SIZE], o32[VECTOR_SIZE];
  int64_t a64[VECTOR_SIZE], b64[VECTOR_SIZE], o64[VECTOR_SIZE];
  int64_t result = 0;

  // Small inputs: `a` is signed, `b` is positive. Kept tiny to stay in range.
  for (int i = 0; i < VECTOR_SIZE; i++) {
    int signed_value = (i % 9) - 4;    // -4..4
    int positive_value = (i % 7) + 1;  // 1..7
    a8[i] = signed_value;
    b8[i] = positive_value;
    a16[i] = signed_value;
    b16[i] = positive_value;
    a32[i] = signed_value;
    b32[i] = positive_value;
    a64[i] = signed_value;
    b64[i] = positive_value;
  }

  for (int iter = 0; iter < iterations; iter++) {
    LANE_OPS(int8_t, a8, b8, o8, result, 0);      // i8x16 (no SIMD multiply)
    LANE_OPS(int16_t, a16, b16, o16, result, 1);  // i16x8
    LANE_OPS(int32_t, a32, b32, o32, result, 1);  // i32x4
    LANE_OPS(int64_t, a64, b64, o64, result, 1);  // i64x2
  }

  return result;
}
