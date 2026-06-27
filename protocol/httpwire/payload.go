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

type RequestStart struct {
	Method        string      `json:"method"`
	Path          string      `json:"path"`
	Query         string      `json:"query,omitempty"`
	Header        http.Header `json:"headers,omitempty"`
	ContentLength int64       `json:"content_length,omitempty"`
}

type Response struct {
	Status int         `json:"status"`
	Header http.Header `json:"headers,omitempty"`
	Body   []byte      `json:"body,omitempty"`
}

type ResponseStart struct {
	Status int         `json:"status"`
	Header http.Header `json:"headers,omitempty"`
}

type BodyChunk struct {
	Data []byte `json:"data"`
}
