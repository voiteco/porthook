// SPDX-License-Identifier: Apache-2.0

package httpwire

import (
	"net/http"
	"testing"

	"github.com/voiteco/porthook/protocol/messages"
)

func TestStreamingPayloadsRoundTrip(t *testing.T) {
	start, err := messages.NewStream(messages.TypeHTTPRequestStart, "str_test", "tun_test", RequestStart{
		Method:        http.MethodPost,
		Path:          "/upload",
		Query:         "x=1",
		Header:        http.Header{"Content-Type": []string{"text/plain"}},
		ContentLength: 7,
	})
	if err != nil {
		t.Fatalf("NewStream request start returned error: %v", err)
	}
	decodedStart, err := messages.DecodePayload[RequestStart](start)
	if err != nil {
		t.Fatalf("DecodePayload request start returned error: %v", err)
	}
	if decodedStart.Method != http.MethodPost || decodedStart.Path != "/upload" || decodedStart.Query != "x=1" {
		t.Fatalf("decoded start = %+v, want posted upload request", decodedStart)
	}
	if decodedStart.Header.Get("Content-Type") != "text/plain" {
		t.Fatalf("Content-Type = %q, want text/plain", decodedStart.Header.Get("Content-Type"))
	}
	if decodedStart.ContentLength != 7 {
		t.Fatalf("content length = %d, want 7", decodedStart.ContentLength)
	}

	chunk, err := messages.NewStream(messages.TypeHTTPRequestBody, "str_test", "tun_test", BodyChunk{
		Data: []byte("payload"),
	})
	if err != nil {
		t.Fatalf("NewStream request body returned error: %v", err)
	}
	decodedChunk, err := messages.DecodePayload[BodyChunk](chunk)
	if err != nil {
		t.Fatalf("DecodePayload body chunk returned error: %v", err)
	}
	if string(decodedChunk.Data) != "payload" {
		t.Fatalf("chunk data = %q, want payload", string(decodedChunk.Data))
	}

	responseStart, err := messages.NewStream(messages.TypeHTTPResponseStart, "str_test", "tun_test", ResponseStart{
		Status: http.StatusCreated,
		Header: http.Header{
			"Content-Type": []string{"text/plain"},
		},
	})
	if err != nil {
		t.Fatalf("NewStream response start returned error: %v", err)
	}
	decodedResponseStart, err := messages.DecodePayload[ResponseStart](responseStart)
	if err != nil {
		t.Fatalf("DecodePayload response start returned error: %v", err)
	}
	if decodedResponseStart.Status != http.StatusCreated {
		t.Fatalf("status = %d, want 201", decodedResponseStart.Status)
	}
	if decodedResponseStart.Header.Get("Content-Type") != "text/plain" {
		t.Fatalf("response Content-Type = %q, want text/plain", decodedResponseStart.Header.Get("Content-Type"))
	}
}
