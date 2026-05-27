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

#include <stdint.h>

__attribute__((noinline))
static int fib_recursive_impl(int n) {
  if (n == 0) {
    return 0;
  }
  if (n == 1) {
    return 1;
  }
  // `volatile` prevents LLVM from rewriting one recursive call into a loop,
  // so both branches of the tree recursion survive -O3.
  volatile int a = fib_recursive_impl(n - 1);
  volatile int b = fib_recursive_impl(n - 2);
  return a + b;
}

// `n` is passed in (rather than hardcoded) so LLVM can't fold the inner
// loop into a closed-form expression.
__attribute__((export_name("fib_recursive")))
int fib_recursive(int32_t iterations, int32_t n) {
  int total = 0;
  for (int32_t i = 0; i < iterations; i++) {
    total += fib_recursive_impl(n);
  }
  return total;
}

__attribute__((export_name("fib_iterative")))
int fib_iterative(int32_t iterations, int32_t n) {
  int total = 0;
  for (int32_t iter = 0; iter < iterations; iter++) {
    int a = 0;
    int b = 1;
    for (int32_t i = 2; i <= n; i++) {
      int next = a + b;
      a = b;
      b = next;
    }
    total += b;
  }
  return total;
}
