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

#include <stdint.h>

__attribute__((import_module("env"), import_name("noop")))
extern int32_t host_noop(int32_t x);

__attribute__((export_name("run_host_calls")))
int32_t run_host_calls(int32_t iterations) {
  int32_t result = 0;
  for (int32_t i = 0; i < iterations; i++) {
    result = host_noop(result + i);
  }
  return result;
}
