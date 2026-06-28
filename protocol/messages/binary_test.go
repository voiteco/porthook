// SPDX-License-Identifier: Apache-2.0

package messages

import (
	"strings"
	"testing"
)

func TestBinaryBodyFrameRoundTrip(t *testing.T) {
	frame, err := NewBinaryBodyFrame(TypeHTTPResponseBody, "str_test", "tun_test", []byte("payload"))
	if err != nil {
		t.Fatalf("NewBinaryBodyFrame returned error: %v", err)
	}

	decoded, err := DecodeBinaryBodyFrame(frame)
	if err != nil {
		t.Fatalf("DecodeBinaryBodyFrame returned error: %v", err)
	}
	if decoded.Type != TypeHTTPResponseBody {
		t.Fatalf("type = %s, want %s", decoded.Type, TypeHTTPResponseBody)
	}
	if decoded.StreamID != "str_test" {
		t.Fatalf("stream id = %q, want str_test", decoded.StreamID)
	}
	if decoded.TunnelID != "tun_test" {
		t.Fatalf("tunnel id = %q, want tun_test", decoded.TunnelID)
	}
	if string(decoded.Data) != "payload" {
		t.Fatalf("data = %q, want payload", string(decoded.Data))
	}
}

func TestBinaryBodyFrameRejectsUnsupportedMessageType(t *testing.T) {
	_, err := NewBinaryBodyFrame(TypeHTTPResponseStart, "str_test", "tun_test", []byte("payload"))
	if err == nil {
		t.Fatal("NewBinaryBodyFrame returned nil error")
	}
	if !strings.Contains(err.Error(), "unsupported binary body message type") {
		t.Fatalf("error = %q, want unsupported type", err.Error())
	}
}

func TestDecodeBinaryBodyFrameRejectsInvalidFrame(t *testing.T) {
	_, err := DecodeBinaryBodyFrame([]byte("not-a-porthook-frame"))
	if err == nil {
		t.Fatal("DecodeBinaryBodyFrame returned nil error")
	}
	if !strings.Contains(err.Error(), "invalid binary body frame magic") {
		t.Fatalf("error = %q, want invalid magic", err.Error())
	}
}
