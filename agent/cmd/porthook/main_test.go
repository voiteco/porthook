// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/voiteco/porthook/agent/internal/agent"
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

func TestRunHelpPrintsUsage(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if err := runWithIO([]string{"help"}, strings.NewReader(""), &stdout, &stderr); err != nil {
		t.Fatalf("run help returned error: %v", err)
	}
	output := stdout.String()
	for _, want := range []string{"porthook login", "porthook tokens", "porthook reserved", "porthook help"} {
		if !strings.Contains(output, want) {
			t.Fatalf("stdout = %q, want %q", output, want)
		}
	}
}

func TestRunLoginWritesConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	t.Setenv("PORTHOOK_CONFIG_PATH", path)

	if err := run([]string{"login", "--server", "https://tunnel.example.com", "--token", "ph_login"}); err != nil {
		t.Fatalf("run login returned error: %v", err)
	}

	loaded, ok, err := agent.LoadConfigFile()
	if err != nil {
		t.Fatalf("LoadConfigFile returned error: %v", err)
	}
	if !ok {
		t.Fatal("login did not write config file")
	}
	if loaded.ServerURL != "https://tunnel.example.com" || loaded.Token != "ph_login" {
		t.Fatalf("loaded config = %+v, want login values", loaded)
	}
}

func TestRunLoginReadsTokenFromStdin(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	t.Setenv("PORTHOOK_CONFIG_PATH", path)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if err := runWithIO(
		[]string{"login", "--server", "https://tunnel.example.com", "--token-stdin"},
		strings.NewReader("ph_stdin\n"),
		&stdout,
		&stderr,
	); err != nil {
		t.Fatalf("run login returned error: %v", err)
	}

	loaded, ok, err := agent.LoadConfigFile()
	if err != nil {
		t.Fatalf("LoadConfigFile returned error: %v", err)
	}
	if !ok {
		t.Fatal("login did not write config file")
	}
	if loaded.ServerURL != "https://tunnel.example.com" || loaded.Token != "ph_stdin" {
		t.Fatalf("loaded config = %+v, want stdin login values", loaded)
	}
	if !strings.Contains(stdout.String(), "Login saved") {
		t.Fatalf("stdout = %q, want login confirmation", stdout.String())
	}
}

func TestRunLoginRejectsTokenFlagAndTokenStdin(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := runWithIO(
		[]string{"login", "--server", "https://tunnel.example.com", "--token", "ph_flag", "--token-stdin"},
		strings.NewReader("ph_stdin"),
		&stdout,
		&stderr,
	)
	if err == nil {
		t.Fatal("run login returned nil error")
	}
	if !strings.Contains(err.Error(), "--token and --token-stdin are mutually exclusive") {
		t.Fatalf("error = %q, want mutually exclusive guidance", err.Error())
	}
}

func TestRunLoginRequiresTokenWhenNonInteractive(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := runWithIO(
		[]string{"login", "--server", "https://tunnel.example.com"},
		strings.NewReader(""),
		&stdout,
		&stderr,
	)
	if err == nil {
		t.Fatal("run login returned nil error")
	}
	if !strings.Contains(err.Error(), "--token-stdin") {
		t.Fatalf("error = %q, want token-stdin guidance", err.Error())
	}
}

func TestRunLogoutRemovesConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	t.Setenv("PORTHOOK_CONFIG_PATH", path)
	if err := agent.SaveConfigFile(agent.ConfigFile{
		ServerURL: "https://tunnel.example.com",
		Token:     "ph_login",
	}); err != nil {
		t.Fatalf("SaveConfigFile returned error: %v", err)
	}

	if err := run([]string{"logout"}); err != nil {
		t.Fatalf("run logout returned error: %v", err)
	}

	_, ok, err := agent.LoadConfigFile()
	if err != nil {
		t.Fatalf("LoadConfigFile returned error: %v", err)
	}
	if ok {
		t.Fatal("logout left config file behind")
	}
}

func TestParseHTTPConfigUsesSavedLogin(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	t.Setenv("PORTHOOK_CONFIG_PATH", path)
	t.Setenv("PORTHOOK_SERVER_URL", "")
	t.Setenv("PORTHOOK_TOKEN", "")
	if err := agent.SaveConfigFile(agent.ConfigFile{
		ServerURL: "https://saved.example.com",
		Token:     "ph_saved",
	}); err != nil {
		t.Fatalf("SaveConfigFile returned error: %v", err)
	}

	cfg, err := parseHTTPConfig([]string{"3000"})
	if err != nil {
		t.Fatalf("parseHTTPConfig returned error: %v", err)
	}
	if cfg.ServerURL != "https://saved.example.com" {
		t.Fatalf("server URL = %q, want saved login", cfg.ServerURL)
	}
	if cfg.Token != "ph_saved" {
		t.Fatalf("token = %q, want saved login", cfg.Token)
	}
}
