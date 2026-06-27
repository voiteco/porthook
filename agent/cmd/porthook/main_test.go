// SPDX-License-Identifier: Apache-2.0

package main

import (
	"strings"
	"testing"
)

func TestParseHTTPConfigRejectsInvalidSubdomain(t *testing.T) {
	_, err := parseHTTPConfig([]string{"3000", "--subdomain", "Demo"})
	if err == nil {
		t.Fatal("parseHTTPConfig returned nil error")
	}
	if !strings.Contains(err.Error(), `invalid subdomain "Demo"`) {
		t.Fatalf("error = %q, want invalid subdomain guidance", err.Error())
	}
}

func TestParseHTTPConfigAcceptsValidSubdomain(t *testing.T) {
	cfg, err := parseHTTPConfig([]string{"3000", "--subdomain", "demo-1"})
	if err != nil {
		t.Fatalf("parseHTTPConfig returned error: %v", err)
	}
	if cfg.RequestedSubdomain != "demo-1" {
		t.Fatalf("subdomain = %q, want demo-1", cfg.RequestedSubdomain)
	}
}
