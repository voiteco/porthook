// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestRunPrintsVersion(t *testing.T) {
	version = "test-version"
	var stdout bytes.Buffer

	if err := run([]string{"version"}, &stdout); err != nil {
		t.Fatalf("run returned error: %v", err)
	}
	if got := strings.TrimSpace(stdout.String()); got != "test-version" {
		t.Fatalf("stdout = %q, want test-version", got)
	}
}

func TestRunRejectsUnknownArgs(t *testing.T) {
	var stdout bytes.Buffer

	err := run([]string{"serve"}, &stdout)
	if err == nil {
		t.Fatal("run returned nil error")
	}
	if !strings.Contains(err.Error(), "usage: porthook-control-plane") {
		t.Fatalf("error = %q, want usage", err.Error())
	}
}
