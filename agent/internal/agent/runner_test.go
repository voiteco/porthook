// SPDX-License-Identifier: Apache-2.0

package agent

import "testing"

func TestBuildWebSocketURL(t *testing.T) {
	tests := []struct {
		name      string
		serverURL string
		want      string
	}{
		{
			name:      "http",
			serverURL: "http://localhost:8081",
			want:      "ws://localhost:8081/agent/connect",
		},
		{
			name:      "https",
			serverURL: "https://tunnel.example.com",
			want:      "wss://tunnel.example.com/agent/connect",
		},
		{
			name:      "base path",
			serverURL: "https://example.com/base",
			want:      "wss://example.com/base/agent/connect",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := BuildWebSocketURL(tt.serverURL)
			if err != nil {
				t.Fatalf("BuildWebSocketURL returned error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("url = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestBuildWebSocketURLRejectsInvalidURL(t *testing.T) {
	if _, err := BuildWebSocketURL("ftp://localhost"); err == nil {
		t.Fatal("BuildWebSocketURL returned nil error")
	}
}
