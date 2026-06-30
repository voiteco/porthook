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
	ValidateAgentToken(context.Context, string) (agentTokenValidation, error)
}

type reservedSubdomainAuthorizer interface {
	AuthorizeReservedSubdomain(context.Context, string, string) (reservedSubdomainAuthorization, error)
}

type accessPolicyEvaluator interface {
	EvaluateAccessPolicy(context.Context, accessPolicyEvaluationRequest) (accessPolicyEvaluation, error)
}

type agentTokenValidation struct {
	Valid   bool
	TokenID string
}

type reservedSubdomainAuthorization struct {
	Allowed bool
	Reason  string
}

type accessPolicyEvaluationRequest struct {
	Subdomain     string
	RemoteIP      string
	BasicUsername string
	BasicPassword string
	BearerToken   string
}

type accessPolicyEvaluation struct {
	Allowed bool
	Mode    string
	Reason  string
}

type staticTokenValidator struct {
	token string
}

type staticReservedSubdomainAuthorizer struct{}
type staticAccessPolicyEvaluator struct{}

type errorTokenValidator struct {
	err error
}

type errorReservedSubdomainAuthorizer struct {
	err error
}

type errorAccessPolicyEvaluator struct {
	err error
}

func (v errorTokenValidator) ValidateAgentToken(context.Context, string) (agentTokenValidation, error) {
	return agentTokenValidation{}, v.err
}

func (v staticTokenValidator) ValidateAgentToken(_ context.Context, token string) (agentTokenValidation, error) {
	return agentTokenValidation{Valid: secretEqual(token, v.token)}, nil
}

func (v staticReservedSubdomainAuthorizer) AuthorizeReservedSubdomain(context.Context, string, string) (reservedSubdomainAuthorization, error) {
	return reservedSubdomainAuthorization{Allowed: true}, nil
}

func (v staticAccessPolicyEvaluator) EvaluateAccessPolicy(context.Context, accessPolicyEvaluationRequest) (accessPolicyEvaluation, error) {
	return accessPolicyEvaluation{Allowed: true, Mode: "public", Reason: "no_control_plane"}, nil
}

func (v errorReservedSubdomainAuthorizer) AuthorizeReservedSubdomain(context.Context, string, string) (reservedSubdomainAuthorization, error) {
	return reservedSubdomainAuthorization{}, v.err
}

func (v errorAccessPolicyEvaluator) EvaluateAccessPolicy(context.Context, accessPolicyEvaluationRequest) (accessPolicyEvaluation, error) {
	return accessPolicyEvaluation{}, v.err
}

type controlPlaneClient struct {
	tokenValidationEndpoint        string
	reservedSubdomainAuthzEndpoint string
	accessPolicyEvaluationEndpoint string
	bearerToken                    string
	client                         *http.Client
}

type validateTokenRequest struct {
	Token string `json:"token"`
	Scope string `json:"scope"`
}

type validateTokenResponse struct {
	Valid   bool   `json:"valid"`
	TokenID string `json:"token_id,omitempty"`
}

type authorizeReservedSubdomainRequest struct {
	TokenID   string `json:"token_id"`
	Subdomain string `json:"subdomain"`
}

type authorizeReservedSubdomainResponse struct {
	Allowed bool   `json:"allowed"`
	Reason  string `json:"reason,omitempty"`
}

type evaluateAccessPolicyRequest struct {
	Subdomain     string `json:"subdomain"`
	RemoteIP      string `json:"remote_ip,omitempty"`
	BasicUsername string `json:"basic_username,omitempty"`
	BasicPassword string `json:"basic_password,omitempty"`
	BearerToken   string `json:"bearer_token,omitempty"`
}

type evaluateAccessPolicyResponse struct {
	Allowed bool   `json:"allowed"`
	Mode    string `json:"mode"`
	Reason  string `json:"reason,omitempty"`
}

func newAgentTokenValidator(cfg Config) (agentTokenValidator, error) {
	if strings.TrimSpace(cfg.ControlPlaneURL) == "" {
		return staticTokenValidator{token: cfg.StaticToken}, nil
	}
	return newControlPlaneClient(cfg)
}

func newReservedSubdomainAuthorizer(cfg Config) (reservedSubdomainAuthorizer, error) {
	if strings.TrimSpace(cfg.ControlPlaneURL) == "" {
		return staticReservedSubdomainAuthorizer{}, nil
	}
	return newControlPlaneClient(cfg)
}

func newAccessPolicyEvaluator(cfg Config) (accessPolicyEvaluator, error) {
	if strings.TrimSpace(cfg.ControlPlaneURL) == "" {
		return staticAccessPolicyEvaluator{}, nil
	}
	return newControlPlaneClient(cfg)
}

func newControlPlaneClient(cfg Config) (controlPlaneClient, error) {
	controlPlaneToken := strings.TrimSpace(cfg.ControlPlaneToken)
	if controlPlaneToken == "" {
		return controlPlaneClient{}, fmt.Errorf("control plane token is required when control plane URL is configured")
	}

	parsed, err := url.Parse(cfg.ControlPlaneURL)
	if err != nil {
		return controlPlaneClient{}, fmt.Errorf("parse control plane URL: %w", err)
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return controlPlaneClient{}, fmt.Errorf("control plane URL must include scheme and host")
	}
	tokenValidationEndpoint := controlPlaneEndpoint(*parsed, "/api/v1/tokens/validate")
	reservedSubdomainAuthzEndpoint := controlPlaneEndpoint(*parsed, "/api/v1/reserved-subdomains/authorize")
	accessPolicyEvaluationEndpoint := controlPlaneEndpoint(*parsed, "/api/v1/access-policies/evaluate")

	return controlPlaneClient{
		tokenValidationEndpoint:        tokenValidationEndpoint,
		reservedSubdomainAuthzEndpoint: reservedSubdomainAuthzEndpoint,
		accessPolicyEvaluationEndpoint: accessPolicyEvaluationEndpoint,
		bearerToken:                    controlPlaneToken,
		client: &http.Client{
			Timeout: cfg.ControlPlaneTimeout,
		},
	}, nil
}

func (v controlPlaneClient) EvaluateAccessPolicy(ctx context.Context, evaluation accessPolicyEvaluationRequest) (accessPolicyEvaluation, error) {
	var body bytes.Buffer
	if err := json.NewEncoder(&body).Encode(evaluateAccessPolicyRequest{
		Subdomain:     evaluation.Subdomain,
		RemoteIP:      evaluation.RemoteIP,
		BasicUsername: evaluation.BasicUsername,
		BasicPassword: evaluation.BasicPassword,
		BearerToken:   evaluation.BearerToken,
	}); err != nil {
		return accessPolicyEvaluation{}, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, v.accessPolicyEvaluationEndpoint, &body)
	if err != nil {
		return accessPolicyEvaluation{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+v.bearerToken)

	resp, err := v.client.Do(req)
	if err != nil {
		return accessPolicyEvaluation{}, fmt.Errorf("evaluate access policy: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return accessPolicyEvaluation{}, fmt.Errorf("evaluate access policy returned status %d", resp.StatusCode)
	}

	var result evaluateAccessPolicyResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return accessPolicyEvaluation{}, fmt.Errorf("decode access policy evaluation: %w", err)
	}
	return accessPolicyEvaluation{
		Allowed: result.Allowed,
		Mode:    result.Mode,
		Reason:  result.Reason,
	}, nil
}

func (v controlPlaneClient) ValidateAgentToken(ctx context.Context, token string) (agentTokenValidation, error) {
	var body bytes.Buffer
	if err := json.NewEncoder(&body).Encode(validateTokenRequest{
		Token: token,
		Scope: scopeRegisterTunnel,
	}); err != nil {
		return agentTokenValidation{}, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, v.tokenValidationEndpoint, &body)
	if err != nil {
		return agentTokenValidation{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+v.bearerToken)

	resp, err := v.client.Do(req)
	if err != nil {
		return agentTokenValidation{}, fmt.Errorf("validate token: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return agentTokenValidation{}, fmt.Errorf("validate token returned status %d", resp.StatusCode)
	}

	var result validateTokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return agentTokenValidation{}, fmt.Errorf("decode token validation: %w", err)
	}
	return agentTokenValidation{
		Valid:   result.Valid,
		TokenID: result.TokenID,
	}, nil
}

func (v controlPlaneClient) AuthorizeReservedSubdomain(ctx context.Context, tokenID, subdomain string) (reservedSubdomainAuthorization, error) {
	if strings.TrimSpace(tokenID) == "" {
		return reservedSubdomainAuthorization{}, fmt.Errorf("control plane token validation did not return token_id; upgrade the control plane or omit requested subdomain")
	}

	var body bytes.Buffer
	if err := json.NewEncoder(&body).Encode(authorizeReservedSubdomainRequest{
		TokenID:   tokenID,
		Subdomain: subdomain,
	}); err != nil {
		return reservedSubdomainAuthorization{}, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, v.reservedSubdomainAuthzEndpoint, &body)
	if err != nil {
		return reservedSubdomainAuthorization{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+v.bearerToken)

	resp, err := v.client.Do(req)
	if err != nil {
		return reservedSubdomainAuthorization{}, fmt.Errorf("authorize reserved subdomain: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return reservedSubdomainAuthorization{}, fmt.Errorf("authorize reserved subdomain returned status %d", resp.StatusCode)
	}

	var result authorizeReservedSubdomainResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return reservedSubdomainAuthorization{}, fmt.Errorf("decode reserved subdomain authorization: %w", err)
	}
	return reservedSubdomainAuthorization{
		Allowed: result.Allowed,
		Reason:  result.Reason,
	}, nil
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

func controlPlaneEndpoint(base url.URL, path string) string {
	base.Path = joinURLPath(base.Path, path)
	base.RawQuery = ""
	base.Fragment = ""
	return base.String()
}
