// SPDX-License-Identifier: Apache-2.0

package messages

import (
	"encoding/json"
	"testing"
)

func TestNewEnvelopeEncodesPayload(t *testing.T) {
	env, err := New(TypeAuthRequest, AuthRequest{
		Token:        "dev-token",
		AgentVersion: "test",
	})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}

	encoded, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("Marshal returned error: %v", err)
	}

	var decoded Envelope
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("Unmarshal returned error: %v", err)
	}

	payload, err := DecodePayload[AuthRequest](decoded)
	if err != nil {
		t.Fatalf("DecodePayload returned error: %v", err)
	}
	if payload.Token != "dev-token" {
		t.Fatalf("payload token = %q, want dev-token", payload.Token)
	}
	if payload.AgentVersion != "test" {
		t.Fatalf("payload agent version = %q, want test", payload.AgentVersion)
	}
}

func TestNewRejectsEmptyType(t *testing.T) {
	if _, err := New("", nil); err == nil {
		t.Fatal("New returned nil error for empty type")
	}
}

func TestNewUsesEmptyObjectPayload(t *testing.T) {
	env, err := New(TypePing, nil)
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}
	if string(env.Payload) != "{}" {
		t.Fatalf("payload = %s, want {}", env.Payload)
	}
}

func TestNewStreamCancel(t *testing.T) {
	env, err := NewStream(TypeHTTPStreamCancel, "str_test", "tun_test", StreamCancel{Reason: "client canceled"})
	if err != nil {
		t.Fatalf("NewStream returned error: %v", err)
	}
	if env.Type != TypeHTTPStreamCancel {
		t.Fatalf("type = %s, want %s", env.Type, TypeHTTPStreamCancel)
	}
	if env.StreamID != "str_test" {
		t.Fatalf("stream id = %q, want str_test", env.StreamID)
	}
	if env.TunnelID != "tun_test" {
		t.Fatalf("tunnel id = %q, want tun_test", env.TunnelID)
	}

	payload, err := DecodePayload[StreamCancel](env)
	if err != nil {
		t.Fatalf("DecodePayload returned error: %v", err)
	}
	if payload.Reason != "client canceled" {
		t.Fatalf("reason = %q, want client canceled", payload.Reason)
	}
}

func TestStreamingMessageTypeValues(t *testing.T) {
	tests := map[Type]string{
		TypeHTTPRequestStart:  "http.request.start",
		TypeHTTPRequestBody:   "http.request.body",
		TypeHTTPRequestEnd:    "http.request.end",
		TypeHTTPResponseStart: "http.response.start",
		TypeHTTPResponseBody:  "http.response.body",
		TypeHTTPResponseEnd:   "http.response.end",
	}

	for got, want := range tests {
		if string(got) != want {
			t.Fatalf("type = %q, want %q", got, want)
		}
	}
}
