// SPDX-License-Identifier: Apache-2.0

package httpwire

import "net/http"

type Request struct {
	Method string      `json:"method"`
	Path   string      `json:"path"`
	Query  string      `json:"query,omitempty"`
	Header http.Header `json:"headers,omitempty"`
	Body   []byte      `json:"body,omitempty"`
}

type Response struct {
	Status int         `json:"status"`
	Header http.Header `json:"headers,omitempty"`
	Body   []byte      `json:"body,omitempty"`
}
