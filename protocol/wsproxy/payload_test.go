// SPDX-License-Identifier: Apache-2.0

package wsproxy

import (
	"net/http"
	"testing"

	"github.com/voiteco/porthook/protocol/messages"
)

func TestWSProxyPayloadsRoundTrip(t *testing.T) {
	open, err := messages.NewStream(messages.TypeWSOpen, "str_ws", "tun_ws", Open{
		Path:         "/socket",
		Query:        "room=1",
		Header:       http.Header{"Cookie": []string{"session=abc"}},
		Subprotocols: []string{"chat.v1"},
	})
	if err != nil {
		t.Fatalf("NewStream open returned error: %v", err)
	}
	decodedOpen, err := messages.DecodePayload[Open](open)
	if err != nil {
		t.Fatalf("DecodePayload open returned error: %v", err)
	}
	if decodedOpen.Path != "/socket" || decodedOpen.Query != "room=1" {
		t.Fatalf("decoded open = %+v, want /socket?room=1", decodedOpen)
	}
	if decodedOpen.Header.Get("Cookie") != "session=abc" {
		t.Fatalf("Cookie = %q, want session=abc", decodedOpen.Header.Get("Cookie"))
	}
	if len(decodedOpen.Subprotocols) != 1 || decodedOpen.Subprotocols[0] != "chat.v1" {
		t.Fatalf("subprotocols = %v, want [chat.v1]", decodedOpen.Subprotocols)
	}

	accept, err := messages.NewStream(messages.TypeWSAccept, "str_ws", "tun_ws", Accept{
		Subprotocol: "chat.v1",
	})
	if err != nil {
		t.Fatalf("NewStream accept returned error: %v", err)
	}
	decodedAccept, err := messages.DecodePayload[Accept](accept)
	if err != nil {
		t.Fatalf("DecodePayload accept returned error: %v", err)
	}
	if decodedAccept.Subprotocol != "chat.v1" {
		t.Fatalf("subprotocol = %q, want chat.v1", decodedAccept.Subprotocol)
	}

	closeMsg, err := messages.NewStream(messages.TypeWSClose, "str_ws", "tun_ws", Close{
		Code:   1000,
		Reason: "done",
	})
	if err != nil {
		t.Fatalf("NewStream close returned error: %v", err)
	}
	decodedClose, err := messages.DecodePayload[Close](closeMsg)
	if err != nil {
		t.Fatalf("DecodePayload close returned error: %v", err)
	}
	if decodedClose.Code != 1000 || decodedClose.Reason != "done" {
		t.Fatalf("decoded close = %+v, want code 1000 reason done", decodedClose)
	}
}

func TestWSProxyOpenOmitsEmptyFields(t *testing.T) {
	open, err := messages.NewStream(messages.TypeWSOpen, "str_ws", "tun_ws", Open{
		Path: "/socket",
	})
	if err != nil {
		t.Fatalf("NewStream open returned error: %v", err)
	}
	decoded, err := messages.DecodePayload[Open](open)
	if err != nil {
		t.Fatalf("DecodePayload open returned error: %v", err)
	}
	if decoded.Query != "" || len(decoded.Header) != 0 || len(decoded.Subprotocols) != 0 {
		t.Fatalf("decoded open = %+v, want only Path set", decoded)
	}
}
