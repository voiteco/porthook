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

func TestBinaryBodyFrameRoundTripWSMessages(t *testing.T) {
	tests := []struct {
		name string
		typ  Type
	}{
		{"text", TypeWSMessageText},
		{"binary", TypeWSMessageBinary},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			frame, err := NewBinaryBodyFrame(tt.typ, "str_ws", "tun_ws", []byte("hello"))
			if err != nil {
				t.Fatalf("NewBinaryBodyFrame returned error: %v", err)
			}
			decoded, err := DecodeBinaryBodyFrame(frame)
			if err != nil {
				t.Fatalf("DecodeBinaryBodyFrame returned error: %v", err)
			}
			if decoded.Type != tt.typ {
				t.Fatalf("type = %s, want %s", decoded.Type, tt.typ)
			}
			if decoded.StreamID != "str_ws" || decoded.TunnelID != "tun_ws" {
				t.Fatalf("ids = %q/%q, want str_ws/tun_ws", decoded.StreamID, decoded.TunnelID)
			}
			if string(decoded.Data) != "hello" {
				t.Fatalf("data = %q, want hello", string(decoded.Data))
			}
		})
	}
}

func TestBinaryBodyFrameDistinguishesWSTextFromBinary(t *testing.T) {
	textFrame, err := NewBinaryBodyFrame(TypeWSMessageText, "str_ws", "tun_ws", []byte("same-bytes"))
	if err != nil {
		t.Fatalf("NewBinaryBodyFrame text returned error: %v", err)
	}
	binaryFrame, err := NewBinaryBodyFrame(TypeWSMessageBinary, "str_ws", "tun_ws", []byte("same-bytes"))
	if err != nil {
		t.Fatalf("NewBinaryBodyFrame binary returned error: %v", err)
	}
	if string(textFrame) == string(binaryFrame) {
		t.Fatal("text and binary WS frames encoded identically, want distinguishable frame type bytes")
	}

	decodedText, err := DecodeBinaryBodyFrame(textFrame)
	if err != nil {
		t.Fatalf("DecodeBinaryBodyFrame returned error: %v", err)
	}
	decodedBinary, err := DecodeBinaryBodyFrame(binaryFrame)
	if err != nil {
		t.Fatalf("DecodeBinaryBodyFrame returned error: %v", err)
	}
	if decodedText.Type == decodedBinary.Type {
		t.Fatal("decoded WS text and binary frames had the same type")
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
