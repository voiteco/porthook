// SPDX-License-Identifier: Apache-2.0

package messages

import (
	"encoding/json"
	"reflect"
	"slices"
	"testing"
)

func TestNewEnvelopeEncodesPayload(t *testing.T) {
	env, err := New(TypeAuthRequest, AuthRequest{
		Token:           "dev-token",
		AgentVersion:    "test",
		ProtocolVersion: ProtocolVersion,
		Capabilities:    []string{"stream_start_end"},
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
	if payload.ProtocolVersion != ProtocolVersion {
		t.Fatalf("payload protocol version = %q, want %q", payload.ProtocolVersion, ProtocolVersion)
	}
	if !reflect.DeepEqual(payload.Capabilities, []string{"stream_start_end"}) {
		t.Fatalf("payload capabilities = %v, want [stream_start_end]", payload.Capabilities)
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
		TypeWSOpen:            "ws.open",
		TypeWSAccept:          "ws.accept",
		TypeWSError:           "ws.error",
		TypeWSClose:           "ws.close",
		TypeWSCancel:          "ws.cancel",
		TypeWSMessageText:     "ws.message.text",
		TypeWSMessageBinary:   "ws.message.binary",
	}

	for got, want := range tests {
		if string(got) != want {
			t.Fatalf("type = %q, want %q", got, want)
		}
	}
}

func TestNewWSCancelReusesStreamCancelPayload(t *testing.T) {
	env, err := NewStream(TypeWSCancel, "str_ws", "tun_ws", StreamCancel{Reason: "peer closed"})
	if err != nil {
		t.Fatalf("NewStream returned error: %v", err)
	}
	payload, err := DecodePayload[StreamCancel](env)
	if err != nil {
		t.Fatalf("DecodePayload returned error: %v", err)
	}
	if payload.Reason != "peer closed" {
		t.Fatalf("reason = %q, want peer closed", payload.Reason)
	}
}

func TestNewWSErrorReusesErrorPayload(t *testing.T) {
	env, err := NewStream(TypeWSError, "str_ws", "tun_ws", ErrorPayload{
		Code:    "local_dial_failed",
		Message: "could not reach the local WebSocket endpoint",
	})
	if err != nil {
		t.Fatalf("NewStream returned error: %v", err)
	}
	payload, err := DecodePayload[ErrorPayload](env)
	if err != nil {
		t.Fatalf("DecodePayload returned error: %v", err)
	}
	if payload.Code != "local_dial_failed" {
		t.Fatalf("code = %q, want local_dial_failed", payload.Code)
	}
}

func TestDefaultProtocolCapabilities(t *testing.T) {
	capabilities := DefaultProtocolCapabilities()
	want := []string{
		CapabilityStreamStartEnd,
		CapabilityBinaryBodyFrame,
		CapabilityStreamCancel,
		CapabilityWebSocketTunnel,
	}
	if !reflect.DeepEqual(capabilities, want) {
		t.Fatalf("capabilities = %v, want %v", capabilities, want)
	}
}

func TestRequiredProtocolCapabilities(t *testing.T) {
	required := RequiredProtocolCapabilities()
	want := []string{
		CapabilityStreamStartEnd,
		CapabilityBinaryBodyFrame,
		CapabilityStreamCancel,
	}
	if !reflect.DeepEqual(required, want) {
		t.Fatalf("required capabilities = %v, want %v", required, want)
	}
	if slices.Contains(required, CapabilityWebSocketTunnel) {
		t.Fatal("required capabilities unexpectedly include websocket_tunnel, which must stay optional for v0.2 interoperability")
	}
}

func TestIsProtocolVersionSupported(t *testing.T) {
	tests := []struct {
		version string
		want    bool
	}{
		{"0.2", true},
		{"0.3", true},
		{"1.0", true},
		{"0.1", false},
		{"", false},
		{"not-a-version", false},
		{"0", false},
	}
	for _, tt := range tests {
		if got := IsProtocolVersionSupported(tt.version); got != tt.want {
			t.Errorf("IsProtocolVersionSupported(%q) = %v, want %v", tt.version, got, tt.want)
		}
	}
}

func TestMissingRequiredCapabilities(t *testing.T) {
	missing := MissingRequiredCapabilities([]string{"stream_start_end"}, []string{
		CapabilityStreamStartEnd,
		CapabilityBinaryBodyFrame,
		CapabilityStreamCancel,
	})
	if len(missing) != 2 {
		t.Fatalf("missing = %v, want 2", missing)
	}
	if missing[0] != CapabilityBinaryBodyFrame || missing[1] != CapabilityStreamCancel {
		t.Fatalf("missing = %v, want sorted list without offered capabilities", missing)
	}
}
