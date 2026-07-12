// SPDX-License-Identifier: AGPL-3.0-only

package customdomains

import (
	"context"
	"net"
	"testing"
	"time"

	"golang.org/x/net/dns/dnsmessage"
)

func TestNewTXTResolverReturnsSystemResolverForEmptyAddr(t *testing.T) {
	resolver := NewTXTResolver("")
	if _, ok := resolver.(netTXTResolver); !ok {
		t.Fatalf("resolver type = %T, want netTXTResolver", resolver)
	}
}

func TestCustomTXTResolverQueriesTheConfiguredServer(t *testing.T) {
	conn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("ListenPacket returned error: %v", err)
	}
	defer conn.Close()

	const wantName = "_porthook.preview.example.test."
	const wantValue = "porthook-domain-verification=phdv_test"

	go serveOneTXTAnswer(t, conn, wantName, wantValue)

	resolver := NewTXTResolver(conn.LocalAddr().String())
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	values, err := resolver.LookupTXT(ctx, wantName)
	if err != nil {
		t.Fatalf("LookupTXT returned error: %v", err)
	}
	if len(values) != 1 || values[0] != wantValue {
		t.Fatalf("values = %v, want [%q]", values, wantValue)
	}
}

func TestCustomTXTResolverFailsWhenServerUnreachable(t *testing.T) {
	// Reserve a port, then close it immediately so nothing answers there.
	conn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("ListenPacket returned error: %v", err)
	}
	addr := conn.LocalAddr().String()
	conn.Close()

	resolver := NewTXTResolver(addr)
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	if _, err := resolver.LookupTXT(ctx, "example.test."); err == nil {
		t.Fatal("LookupTXT returned nil error for an unreachable resolver")
	}
}

// serveOneTXTAnswer answers exactly one DNS query on conn with a TXT record
// for wantName, if the query asks for that name; otherwise it responds with
// NXDOMAIN. It exits after the first request.
func serveOneTXTAnswer(t *testing.T, conn net.PacketConn, wantName, value string) {
	t.Helper()

	buf := make([]byte, 512)
	n, addr, err := conn.ReadFrom(buf)
	if err != nil {
		return
	}

	var msg dnsmessage.Message
	if err := msg.Unpack(buf[:n]); err != nil {
		t.Errorf("Unpack query returned error: %v", err)
		return
	}
	if len(msg.Questions) != 1 {
		t.Errorf("questions = %d, want 1", len(msg.Questions))
		return
	}
	question := msg.Questions[0]

	response := dnsmessage.Message{
		Header: dnsmessage.Header{
			ID:            msg.Header.ID,
			Response:      true,
			Authoritative: true,
		},
		Questions: msg.Questions,
	}
	if question.Name.String() == wantName && question.Type == dnsmessage.TypeTXT {
		response.Answers = []dnsmessage.Resource{
			{
				Header: dnsmessage.ResourceHeader{
					Name:  question.Name,
					Type:  dnsmessage.TypeTXT,
					Class: dnsmessage.ClassINET,
					TTL:   60,
				},
				Body: &dnsmessage.TXTResource{TXT: []string{value}},
			},
		}
	} else {
		response.Header.RCode = dnsmessage.RCodeNameError
	}

	packed, err := response.Pack()
	if err != nil {
		t.Errorf("Pack response returned error: %v", err)
		return
	}
	if _, err := conn.WriteTo(packed, addr); err != nil {
		t.Errorf("WriteTo returned error: %v", err)
	}
}
