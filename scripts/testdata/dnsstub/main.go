// SPDX-License-Identifier: AGPL-3.0-only

// Command dnsstub is a minimal authoritative DNS server used only by
// scripts/smoke-tls-edge.sh to answer the control plane's custom-domain TXT
// verification lookup without depending on real, internet-resolvable DNS.
// It answers exactly one configured name with one configured TXT value and
// returns NXDOMAIN for everything else.
package main

import (
	"log"
	"net"
	"os"
	"strings"

	"golang.org/x/net/dns/dnsmessage"
)

func main() {
	addr := os.Getenv("DNSSTUB_ADDR")
	if addr == "" {
		addr = "127.0.0.1:15353"
	}
	name := dnsFQDN(os.Getenv("DNSSTUB_NAME"))
	value := os.Getenv("DNSSTUB_VALUE")
	if name == "" || value == "" {
		log.Fatal("dnsstub: DNSSTUB_NAME and DNSSTUB_VALUE are required")
	}

	conn, err := net.ListenPacket("udp", addr)
	if err != nil {
		log.Fatalf("dnsstub: listen: %v", err)
	}
	defer conn.Close()

	log.Printf("dnsstub: listening on %s, answering TXT %s = %q", addr, name, value)

	buf := make([]byte, 512)
	for {
		n, peer, err := conn.ReadFrom(buf)
		if err != nil {
			log.Printf("dnsstub: read: %v", err)
			continue
		}
		reply, err := buildReply(buf[:n], name, value)
		if err != nil {
			log.Printf("dnsstub: build reply: %v", err)
			continue
		}
		if _, err := conn.WriteTo(reply, peer); err != nil {
			log.Printf("dnsstub: write: %v", err)
		}
	}
}

func buildReply(query []byte, wantName, value string) ([]byte, error) {
	var msg dnsmessage.Message
	if err := msg.Unpack(query); err != nil {
		return nil, err
	}

	response := dnsmessage.Message{
		Header: dnsmessage.Header{
			ID:            msg.Header.ID,
			Response:      true,
			Authoritative: true,
		},
		Questions: msg.Questions,
	}

	if len(msg.Questions) == 1 {
		question := msg.Questions[0]
		if question.Type == dnsmessage.TypeTXT && strings.EqualFold(question.Name.String(), wantName) {
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
	}

	return response.Pack()
}

func dnsFQDN(name string) string {
	name = strings.TrimSpace(name)
	if name == "" || strings.HasSuffix(name, ".") {
		return name
	}
	return name + "."
}
