// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestRunPrintsVersion(t *testing.T) {
	oldVersion := version
	version = "test-version"
	defer func() {
		version = oldVersion
	}()

	var stdout bytes.Buffer
	if err := run([]string{"--version"}, &stdout); err != nil {
		t.Fatalf("run returned error: %v", err)
	}

	if got := strings.TrimSpace(stdout.String()); got != "test-version" {
		t.Fatalf("version output = %q, want test-version", got)
	}
}

func TestRunRejectsUnknownArgs(t *testing.T) {
	var stdout bytes.Buffer
	err := run([]string{"--unknown"}, &stdout)
	if err == nil {
		t.Fatal("run returned nil error")
	}
	if !strings.Contains(err.Error(), "usage: porthook-gateway") {
		t.Fatalf("error = %q, want usage guidance", err.Error())
	}
}
