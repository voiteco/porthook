// SPDX-License-Identifier: AGPL-3.0-only

package tokens

import (
	"context"
	"time"
)

const ScopeRegisterTunnel = "register_tunnel"

type TokenRecord struct {
	ID        string
	Name      string
	TokenHash string
	Scopes    []string
	CreatedAt time.Time
	RevokedAt *time.Time
}

type CreateTokenRequest struct {
	Name   string
	Scopes []string
}

type CreatedToken struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Token     string    `json:"token"`
	Scopes    []string  `json:"scopes"`
	CreatedAt time.Time `json:"created_at"`
}

type TokenSummary struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	Scopes    []string   `json:"scopes"`
	CreatedAt time.Time  `json:"created_at"`
	RevokedAt *time.Time `json:"revoked_at,omitempty"`
}

type ListTokensResponse struct {
	Tokens []TokenSummary `json:"tokens"`
}

type ValidationResult struct {
	Valid   bool     `json:"valid"`
	TokenID string   `json:"token_id,omitempty"`
	Scopes  []string `json:"scopes,omitempty"`
}

type Store interface {
	Create(context.Context, TokenRecord) error
	List(context.Context) ([]TokenRecord, error)
	LookupByHash(context.Context, string) (TokenRecord, bool, error)
	LookupByID(context.Context, string) (TokenRecord, bool, error)
	Revoke(context.Context, string, time.Time) error
}
