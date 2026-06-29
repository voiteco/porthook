// SPDX-License-Identifier: AGPL-3.0-only

package controlplane

import "os"

const (
	defaultAddr = ":8082"
)

type Config struct {
	Addr        string
	AdminToken  string
	DatabaseURL string
}

func ConfigFromEnv() Config {
	cfg := Config{
		Addr:        envString("PORTHOOK_CONTROL_ADDR", defaultAddr),
		AdminToken:  os.Getenv("PORTHOOK_CONTROL_ADMIN_TOKEN"),
		DatabaseURL: os.Getenv("PORTHOOK_DATABASE_URL"),
	}
	if cfg.Addr == "" {
		cfg.Addr = defaultAddr
	}
	return cfg
}

func envString(name, fallback string) string {
	value := os.Getenv(name)
	if value == "" {
		return fallback
	}
	return value
}
