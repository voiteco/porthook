// SPDX-License-Identifier: Apache-2.0

package agent

import (
	"os"
	"strconv"
	"time"
)

const (
	defaultServerURL            = "http://localhost:8081"
	defaultToken                = "dev-token"
	defaultRequestTimeout       = 30 * time.Second
	defaultMaxResponseBodyBytes = 1 << 20
)

type Config struct {
	ServerURL            string
	Token                string
	RequestedSubdomain   string
	Port                 int
	LocalTarget          string
	AgentVersion         string
	RequestTimeout       time.Duration
	MaxResponseBodyBytes int64
}

func ConfigFromEnv() Config {
	return Config{
		ServerURL:            envString("PORTHOOK_SERVER_URL", defaultServerURL),
		Token:                envString("PORTHOOK_TOKEN", defaultToken),
		RequestTimeout:       defaultRequestTimeout,
		MaxResponseBodyBytes: envInt64("PORTHOOK_MAX_RESPONSE_BODY_BYTES", defaultMaxResponseBodyBytes),
	}
}

func envString(name, fallback string) string {
	value := os.Getenv(name)
	if value == "" {
		return fallback
	}
	return value
}

func envInt64(name string, fallback int64) int64 {
	value := os.Getenv(name)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}
