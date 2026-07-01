// SPDX-License-Identifier: AGPL-3.0-only

package healthcheck

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

const (
	defaultTimeout = 2 * time.Second
)

// TimeoutFromEnv returns the healthcheck timeout. Invalid values fall back to
// the default so a typo does not prevent a container from starting.
func TimeoutFromEnv() time.Duration {
	value := strings.TrimSpace(os.Getenv("PORTHOOK_HEALTHCHECK_TIMEOUT"))
	if value == "" {
		return defaultTimeout
	}
	parsed, err := time.ParseDuration(value)
	if err != nil || parsed <= 0 {
		return defaultTimeout
	}
	return parsed
}

func URLFromEnvOrListenAddr(listenAddr string, path string) (string, error) {
	if rawURL := strings.TrimSpace(os.Getenv("PORTHOOK_HEALTHCHECK_URL")); rawURL != "" {
		parsed, err := url.Parse(rawURL)
		if err != nil {
			return "", fmt.Errorf("invalid PORTHOOK_HEALTHCHECK_URL %q: %w", rawURL, err)
		}
		if parsed.Scheme == "" || parsed.Host == "" {
			return "", fmt.Errorf("invalid PORTHOOK_HEALTHCHECK_URL %q: scheme and host are required", rawURL)
		}
		return rawURL, nil
	}
	return URLForListenAddr(listenAddr, path)
}

func URLForListenAddr(listenAddr string, path string) (string, error) {
	host, port, err := net.SplitHostPort(strings.TrimSpace(listenAddr))
	if err != nil {
		return "", fmt.Errorf("invalid listen address %q: %w", listenAddr, err)
	}
	if port == "" {
		return "", fmt.Errorf("invalid listen address %q: port is required", listenAddr)
	}
	if host == "" || host == "0.0.0.0" || host == "::" || host == "[::]" {
		host = "127.0.0.1"
	}
	if strings.HasPrefix(host, "[") && strings.HasSuffix(host, "]") {
		host = strings.TrimPrefix(strings.TrimSuffix(host, "]"), "[")
	}
	target := url.URL{
		Scheme: "http",
		Host:   net.JoinHostPort(host, port),
		Path:   path,
	}
	return target.String(), nil
}

func HTTP(ctx context.Context, rawURL string, timeout time.Duration) error {
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	client := &http.Client{Timeout: timeout}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return fmt.Errorf("build healthcheck request: %w", err)
	}
	req.Header.Set("Accept", "text/plain, application/json;q=0.9, */*;q=0.1")

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("healthcheck request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		message := strings.Join(strings.Fields(string(body)), " ")
		if message == "" {
			message = http.StatusText(resp.StatusCode)
		}
		return fmt.Errorf("healthcheck returned %d: %s", resp.StatusCode, message)
	}
	return nil
}
