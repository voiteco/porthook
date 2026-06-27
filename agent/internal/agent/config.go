// SPDX-License-Identifier: Apache-2.0

package agent

import (
	"os"
	"time"
)

const (
	defaultServerURL      = "http://localhost:8081"
	defaultToken          = "dev-token"
	defaultRequestTimeout = 30 * time.Second
)

type Config struct {
	ServerURL          string
	Token              string
	RequestedSubdomain string
	Port               int
	LocalTarget        string
	AgentVersion       string
	RequestTimeout     time.Duration
}

func ConfigFromEnv() Config {
	return Config{
		ServerURL:      envString("PORTHOOK_SERVER_URL", defaultServerURL),
		Token:          envString("PORTHOOK_TOKEN", defaultToken),
		RequestTimeout: defaultRequestTimeout,
	}
}

func envString(name, fallback string) string {
	value := os.Getenv(name)
	if value == "" {
		return fallback
	}
	return value
}
