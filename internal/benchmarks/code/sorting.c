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
emcc sorting.c -o sorting.wasm -s \
  EXPORTED_FUNCTIONS="['_bubble_sort', '_merge_sort', '_quick_sort']" \
  -s STANDALONE_WASM \
  -O3 \
  --no-entry
```
 */
#include <stdlib.h>

#define BUFFER_LENGTH 100
int data_buffer[BUFFER_LENGTH] = {
    -225, 285,  -180, -51,  -278, -113, 227,  0,    63,   -190, 260,  274,
    275,  194,  -115, -102, 262,  -108, 288,  -25,  -125, -226, -213, -110,
    43,   -126, 71,   254,  111,  -227, -204, 287,  18,   225,  -143, -54,
    30,   264,  195,  127,  -255, 64,   -221, 184,  211,  -292, -214, 189,
    130,  -83,  -55,  237,  188,  -209, 181,  133,  -206, 197,  -123, 247,
    221,  -200, 259,  -192, 198,  123,  -170, -120, -264, 136,  281,  -259,
    227,  14,   174,  -83,  31,   -231, -156, -32,  289,  -39,  107,  148,
    296,  -265, 298,  -188, -265, -223, -75,  192,  -223, 74,   47,   147,
    282,  246,  -239, 83};

void bubble_sort() {
  for (int i = 0; i < BUFFER_LENGTH; i++) {
    for (int j = 0; j < BUFFER_LENGTH - i - 1; j++) {
      if (data_buffer[j] > data_buffer[j + 1]) {
        int tmp = data_buffer[j + 1];
        data_buffer[j + 1] = data_buffer[j];
        data_buffer[j] = tmp;
      }
    }
  }
}

void merge_sort_internal(int *array, int left, int right) {
  if (left >= right) {
    return;
  }

  int mid = left + (right - left) / 2;
  merge_sort_internal(array, left, mid);
  merge_sort_internal(array, mid + 1, right);

  int n1 = mid - left + 1;
  int n2 = right - mid;
  int *left_array = (int *)malloc(n1 * sizeof(int));
  int *right_array = (int *)malloc(n2 * sizeof(int));
  for (int i = 0; i < n1; i++) {
    left_array[i] = array[left + i];
  }
  for (int j = 0; j < n2; j++) {
    right_array[j] = array[mid + 1 + j];
  }

  int i = 0;
  int j = 0;
  int k = left;
  while (i < n1 && j < n2) {
    if (left_array[i] <= right_array[j]) {
      array[k++] = left_array[i++];
    } else {
      array[k++] = right_array[j++];
    }
  }

  while (i < n1) {
    array[k++] = left_array[i++];
  }

  while (j < n2) {
    array[k++] = right_array[j++];
  }

  free(left_array);
  free(right_array);
}

void merge_sort() { merge_sort_internal(data_buffer, 0, BUFFER_LENGTH - 1); }

void swap(int *a, int *b) {
  int temp = *a;
  *a = *b;
  *b = temp;
}

int partition(int *array, int left, int right) {
  int pivot = array[right];
  int i = left - 1;

  for (int j = left; j < right; j++) {
    if (array[j] <= pivot) {
      i++;
      swap(&array[i], &array[j]);
    }
  }
  swap(&array[i + 1], &array[right]);
  return i + 1;
}

void quick_sort_internal(int *array, int left, int right) {
  if (left >= right) {
    return;
  }
  int pivot_index = partition(array, left, right);
  quick_sort_internal(array, left, pivot_index - 1);
  quick_sort_internal(array, pivot_index + 1, right);
}

void quick_sort() { quick_sort_internal(data_buffer, 0, BUFFER_LENGTH - 1); }

#include <stdio.h>
int main(void) {
  merge_sort();
  for (int i = 0; i < BUFFER_LENGTH; i++) {
    printf("%d, ", data_buffer[i]);
  }
}