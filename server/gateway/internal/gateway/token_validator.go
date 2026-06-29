// SPDX-License-Identifier: AGPL-3.0-only

package gateway

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

const scopeRegisterTunnel = "register_tunnel"

type agentTokenValidator interface {
	ValidateAgentToken(context.Context, string) (bool, error)
}

type staticTokenValidator struct {
	token string
}

type errorTokenValidator struct {
	err error
}

func (v errorTokenValidator) ValidateAgentToken(context.Context, string) (bool, error) {
	return false, v.err
}

func (v staticTokenValidator) ValidateAgentToken(_ context.Context, token string) (bool, error) {
	return secretEqual(token, v.token), nil
}

type controlPlaneTokenValidator struct {
	endpoint    string
	bearerToken string
	client      *http.Client
}

type validateTokenRequest struct {
	Token string `json:"token"`
	Scope string `json:"scope"`
}

type validateTokenResponse struct {
	Valid bool `json:"valid"`
}

func newAgentTokenValidator(cfg Config) (agentTokenValidator, error) {
	if strings.TrimSpace(cfg.ControlPlaneURL) == "" {
		return staticTokenValidator{token: cfg.StaticToken}, nil
	}
	controlPlaneToken := strings.TrimSpace(cfg.ControlPlaneToken)
	if controlPlaneToken == "" {
		return nil, fmt.Errorf("control plane token is required when control plane URL is configured")
	}

	parsed, err := url.Parse(cfg.ControlPlaneURL)
	if err != nil {
		return nil, fmt.Errorf("parse control plane URL: %w", err)
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return nil, fmt.Errorf("control plane URL must include scheme and host")
	}
	parsed.Path = joinURLPath(parsed.Path, "/api/v1/tokens/validate")
	parsed.RawQuery = ""
	parsed.Fragment = ""

	return controlPlaneTokenValidator{
		endpoint:    parsed.String(),
		bearerToken: controlPlaneToken,
		client: &http.Client{
			Timeout: cfg.ControlPlaneTimeout,
		},
	}, nil
}

func (v controlPlaneTokenValidator) ValidateAgentToken(ctx context.Context, token string) (bool, error) {
	var body bytes.Buffer
	if err := json.NewEncoder(&body).Encode(validateTokenRequest{
		Token: token,
		Scope: scopeRegisterTunnel,
	}); err != nil {
		return false, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, v.endpoint, &body)
	if err != nil {
		return false, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+v.bearerToken)

	resp, err := v.client.Do(req)
	if err != nil {
		return false, fmt.Errorf("validate token: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("validate token returned status %d", resp.StatusCode)
	}

	var result validateTokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return false, fmt.Errorf("decode token validation: %w", err)
	}
	return result.Valid, nil
}

func secretEqual(got, want string) bool {
	if got == "" || want == "" {
		return false
	}
	gotHash := sha256.Sum256([]byte(got))
	wantHash := sha256.Sum256([]byte(want))
	return subtle.ConstantTimeCompare(gotHash[:], wantHash[:]) == 1
}

func joinURLPath(basePath, requestPath string) string {
	switch {
	case basePath == "" || basePath == "/":
		return requestPath
	case strings.HasSuffix(basePath, "/") && strings.HasPrefix(requestPath, "/"):
		return basePath + strings.TrimPrefix(requestPath, "/")
	case !strings.HasSuffix(basePath, "/") && !strings.HasPrefix(requestPath, "/"):
		return basePath + "/" + requestPath
	default:
		return basePath + requestPath
	}
}
