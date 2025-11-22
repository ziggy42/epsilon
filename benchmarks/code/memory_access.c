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
emcc memory_access.c -o memory_access.wasm -s \
  EXPORTED_FUNCTIONS="['_run_memcpy']" \
  -s STANDALONE_WASM \
  -O3 \
  --no-entry \
  -s INITIAL_MEMORY=39321600
```
 *
 * NOTE: The `-s INITIAL_MEMORY=39321600` flag allocates 32MB of memory
 * for the module, which is necessary for the two 16MB buffers.
 */

#include <stdint.h>
#include <stddef.h>

// 16 MiB (16 * 1024 * 1024 bytes)
#define BUFFER_SIZE 16777216

uint8_t source_buffer[BUFFER_SIZE];
uint8_t destination_buffer[BUFFER_SIZE];

uint32_t run_memcpy(int iterations) {
  // Initialize source buffer.
  for (size_t i = 0; i < BUFFER_SIZE; ++i) {
    source_buffer[i] = (uint8_t)(i * 31 % 251);
  }

  for (int iter = 0; iter < iterations; ++iter) {
    for (size_t i = 0; i < BUFFER_SIZE; ++i) {
      destination_buffer[i] = source_buffer[i];
    }
  }

  // A value is returned here to prevent aggressive optimizations to remove the
  // implementation above.
  return destination_buffer[BUFFER_SIZE / 2];
}