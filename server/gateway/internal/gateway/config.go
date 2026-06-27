// SPDX-License-Identifier: AGPL-3.0-only

package gateway

import (
	"os"
	"time"
)

const (
	defaultPublicAddr      = ":8080"
	defaultAgentAddr       = ":8081"
	defaultRootDomain      = "localhost"
	defaultPublicURL       = "http://localhost:8080"
	defaultStaticToken     = "dev-token"
	defaultMaxBodyBytes    = 1 << 20
	defaultStreamTimeout   = 30 * time.Second
	defaultShutdownTimeout = 5 * time.Second
)

type Config struct {
	PublicAddr      string
	AgentAddr       string
	RootDomain      string
	PublicURL       string
	StaticToken     string
	MaxBodyBytes    int64
	StreamTimeout   time.Duration
	ShutdownTimeout time.Duration
}

func ConfigFromEnv() Config {
	return Config{
		PublicAddr:      envString("PORTHOOK_ADDR", defaultPublicAddr),
		AgentAddr:       envString("PORTHOOK_AGENT_ADDR", defaultAgentAddr),
		RootDomain:      envString("PORTHOOK_ROOT_DOMAIN", defaultRootDomain),
		PublicURL:       envString("PORTHOOK_PUBLIC_URL", defaultPublicURL),
		StaticToken:     envString("PORTHOOK_STATIC_TOKEN", defaultStaticToken),
		MaxBodyBytes:    defaultMaxBodyBytes,
		StreamTimeout:   defaultStreamTimeout,
		ShutdownTimeout: defaultShutdownTimeout,
	}
}

func envString(name, fallback string) string {
	value := os.Getenv(name)
	if value == "" {
		return fallback
	}
	return value
}
