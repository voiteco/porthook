// SPDX-License-Identifier: Apache-2.0

package agent

import (
	"path/filepath"
	"testing"
)

func TestSaveLoadAndRemoveConfigFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	t.Setenv("PORTHOOK_CONFIG_PATH", path)

	if err := SaveConfigFile(ConfigFile{
		ServerURL: "https://tunnel.example.com",
		Token:     "ph_test",
	}); err != nil {
		t.Fatalf("SaveConfigFile returned error: %v", err)
	}

	loaded, ok, err := LoadConfigFile()
	if err != nil {
		t.Fatalf("LoadConfigFile returned error: %v", err)
	}
	if !ok {
		t.Fatal("LoadConfigFile did not find saved config")
	}
	if loaded.ServerURL != "https://tunnel.example.com" || loaded.Token != "ph_test" {
		t.Fatalf("loaded config = %+v, want saved server/token", loaded)
	}

	if err := RemoveConfigFile(); err != nil {
		t.Fatalf("RemoveConfigFile returned error: %v", err)
	}
	_, ok, err = LoadConfigFile()
	if err != nil {
		t.Fatalf("LoadConfigFile after remove returned error: %v", err)
	}
	if ok {
		t.Fatal("LoadConfigFile found config after remove")
	}
}

func TestConfigFromEnvLoadsConfigFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	t.Setenv("PORTHOOK_CONFIG_PATH", path)
	t.Setenv("PORTHOOK_SERVER_URL", "")
	t.Setenv("PORTHOOK_TOKEN", "")

	if err := SaveConfigFile(ConfigFile{
		ServerURL: "https://saved.example.com",
		Token:     "ph_saved",
	}); err != nil {
		t.Fatalf("SaveConfigFile returned error: %v", err)
	}

	cfg := ConfigFromEnv()
	if cfg.ServerURL != "https://saved.example.com" {
		t.Fatalf("server URL = %q, want saved config", cfg.ServerURL)
	}
	if cfg.Token != "ph_saved" {
		t.Fatalf("token = %q, want saved config", cfg.Token)
	}
}

func TestConfigFromEnvOverridesConfigFileWithEnvironment(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	t.Setenv("PORTHOOK_CONFIG_PATH", path)
	t.Setenv("PORTHOOK_SERVER_URL", "https://env.example.com")
	t.Setenv("PORTHOOK_TOKEN", "ph_env")

	if err := SaveConfigFile(ConfigFile{
		ServerURL: "https://saved.example.com",
		Token:     "ph_saved",
	}); err != nil {
		t.Fatalf("SaveConfigFile returned error: %v", err)
	}

	cfg := ConfigFromEnv()
	if cfg.ServerURL != "https://env.example.com" {
		t.Fatalf("server URL = %q, want env override", cfg.ServerURL)
	}
	if cfg.Token != "ph_env" {
		t.Fatalf("token = %q, want env override", cfg.Token)
	}
}
