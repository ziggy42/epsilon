// Copyright 2025 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package repl

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
)

func ResolveModule(source string) (io.ReadCloser, error) {
	url, err := url.Parse(source)
	if err != nil {
		return nil, err
	}

	switch url.Scheme {
	case "http", "https":
		return resolveHttp(url)
	case "file", "":
		return os.Open(url.Path)
	default:
		return nil, fmt.Errorf("unsupported url scheme: %s", url.Scheme)
	}
}

func resolveHttp(url *url.URL) (io.ReadCloser, error) {
	response, err := http.Get(url.String())
	if err != nil {
		return nil, fmt.Errorf("http request failed: %w", err)
	}

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		response.Body.Close()
		return nil, fmt.Errorf("unexpected http status: %s", response.Status)
	}

	return response.Body, nil
}
