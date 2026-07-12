// SPDX-License-Identifier: AGPL-3.0-only

// Command wsclient is a minimal WebSocket test client used by
// scripts/smoke-websocket.sh and scripts/smoke-tls-edge.sh to drive a real
// public WebSocket upgrade through a real gateway and agent binary pair,
// proving the tunneled round trip actually works end to end. It connects,
// exchanges a text and a binary message with the echo service on the other
// end, verifies the negotiated subprotocol, and exits non-zero with a
// message on any mismatch. An optional CA certificate file lets it complete
// a real TLS handshake for a wss:// URL signed by a private CA.
package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"
	"os"
	"time"

	"nhooyr.io/websocket"
)

func main() {
	if len(os.Args) != 3 && len(os.Args) != 4 {
		fmt.Fprintln(os.Stderr, "usage: wsclient <ws-url> <host> [ca-file]")
		os.Exit(2)
	}
	url := os.Args[1]
	host := os.Args[2]

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	dialOpts := &websocket.DialOptions{
		Host:         host,
		Subprotocols: []string{"echo.v1"},
	}
	if len(os.Args) == 4 {
		httpClient, err := httpClientTrusting(os.Args[3], host)
		if err != nil {
			fmt.Fprintf(os.Stderr, "load CA file failed: %v\n", err)
			os.Exit(1)
		}
		dialOpts.HTTPClient = httpClient
	}

	conn, _, err := websocket.Dial(ctx, url, dialOpts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "dial failed: %v\n", err)
		os.Exit(1)
	}
	defer conn.CloseNow()

	if got := conn.Subprotocol(); got != "echo.v1" {
		fmt.Fprintf(os.Stderr, "negotiated subprotocol = %q, want echo.v1\n", got)
		os.Exit(1)
	}

	if err := conn.Write(ctx, websocket.MessageText, []byte("porthook websocket smoke")); err != nil {
		fmt.Fprintf(os.Stderr, "write text failed: %v\n", err)
		os.Exit(1)
	}
	msgType, data, err := conn.Read(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "read text echo failed: %v\n", err)
		os.Exit(1)
	}
	if msgType != websocket.MessageText || string(data) != "porthook websocket smoke" {
		fmt.Fprintf(os.Stderr, "text echo = (%v, %q), want (Text, \"porthook websocket smoke\")\n", msgType, string(data))
		os.Exit(1)
	}

	binaryPayload := bytes.Repeat([]byte{0x01, 0x02, 0x03, 0xFF}, 256)
	if err := conn.Write(ctx, websocket.MessageBinary, binaryPayload); err != nil {
		fmt.Fprintf(os.Stderr, "write binary failed: %v\n", err)
		os.Exit(1)
	}
	msgType, data, err = conn.Read(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "read binary echo failed: %v\n", err)
		os.Exit(1)
	}
	if msgType != websocket.MessageBinary || !bytes.Equal(data, binaryPayload) {
		fmt.Fprintf(os.Stderr, "binary echo mismatch: got %d bytes, want %d bytes matching\n", len(data), len(binaryPayload))
		os.Exit(1)
	}

	if err := conn.Close(websocket.StatusNormalClosure, "smoke test done"); err != nil {
		fmt.Fprintf(os.Stderr, "close failed: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("websocket smoke check passed")
}

// httpClientTrusting returns a client that trusts the CA certificate(s) in
// caFile and always presents serverName as the TLS SNI, regardless of the
// dialed URL's host. This lets the dial URL stay an IP literal (avoiding any
// dependency on DNS) while the TLS handshake still targets the certificate
// issued for the real hostname.
func httpClientTrusting(caFile, serverName string) (*http.Client, error) {
	pemData, err := os.ReadFile(caFile)
	if err != nil {
		return nil, fmt.Errorf("read CA file: %w", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(pemData) {
		return nil, fmt.Errorf("no certificates found in %s", caFile)
	}
	return &http.Client{
		Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool, ServerName: serverName}},
	}, nil
}
