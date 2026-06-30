// SPDX-License-Identifier: AGPL-3.0-only

package tokens

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestServiceCreatesAndValidatesToken(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	service := NewService(store)
	createdAt := time.Date(2026, 6, 30, 9, 0, 0, 0, time.UTC)
	usedAt := createdAt.Add(15 * time.Minute)
	now := createdAt
	service.now = func() time.Time { return now }

	created, err := service.CreateToken(ctx, CreateTokenRequest{
		Name:   "local agent",
		Scopes: []string{ScopeRegisterTunnel},
	})
	if err != nil {
		t.Fatalf("CreateToken returned error: %v", err)
	}
	if created.ID == "" {
		t.Fatal("created token id is empty")
	}
	if !strings.HasPrefix(created.Token, "ph_") {
		t.Fatalf("token = %q, want ph_ prefix", created.Token)
	}
	if created.Name != "local agent" {
		t.Fatalf("name = %q, want local agent", created.Name)
	}

	now = usedAt
	result, err := service.ValidateToken(ctx, created.Token, ScopeRegisterTunnel)
	if err != nil {
		t.Fatalf("ValidateToken returned error: %v", err)
	}
	if !result.Valid {
		t.Fatal("created token was not valid")
	}
	if result.TokenID != created.ID {
		t.Fatalf("token id = %q, want %q", result.TokenID, created.ID)
	}

	listed, err := service.ListTokens(ctx)
	if err != nil {
		t.Fatalf("ListTokens returned error: %v", err)
	}
	if len(listed.Tokens) != 1 {
		t.Fatalf("listed tokens = %d, want 1", len(listed.Tokens))
	}
	if listed.Tokens[0].LastUsedAt == nil || !listed.Tokens[0].LastUsedAt.Equal(usedAt) {
		t.Fatalf("last_used_at = %v, want %s", listed.Tokens[0].LastUsedAt, usedAt)
	}
}

func TestServiceRejectsUnknownToken(t *testing.T) {
	ctx := context.Background()
	service := NewService(NewMemoryStore())

	result, err := service.ValidateToken(ctx, "ph_missing", ScopeRegisterTunnel)
	if err != nil {
		t.Fatalf("ValidateToken returned error: %v", err)
	}
	if result.Valid {
		t.Fatal("unknown token validated")
	}
}

func TestServiceEnforcesScopes(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	service := NewService(store)
	token := "ph_limited"

	if err := store.Create(ctx, TokenRecord{
		ID:        "tok_limited",
		Name:      "limited",
		TokenHash: HashToken(token),
		Scopes:    nil,
		CreatedAt: time.Now(),
	}); err != nil {
		t.Fatalf("store Create returned error: %v", err)
	}

	result, err := service.ValidateToken(ctx, token, ScopeRegisterTunnel)
	if err != nil {
		t.Fatalf("ValidateToken returned error: %v", err)
	}
	if result.Valid {
		t.Fatal("token validated for a missing scope")
	}
}

func TestServiceRejectsUnknownCreateScope(t *testing.T) {
	ctx := context.Background()
	service := NewService(NewMemoryStore())

	_, err := service.CreateToken(ctx, CreateTokenRequest{
		Name:   "bad scope",
		Scopes: []string{"typo_scope"},
	})
	if err == nil {
		t.Fatal("CreateToken returned nil error")
	}
	if !strings.Contains(err.Error(), `unsupported token scope "typo_scope"`) {
		t.Fatalf("error = %q, want unsupported scope guidance", err.Error())
	}
}

func TestServiceRejectsUnknownValidationScope(t *testing.T) {
	ctx := context.Background()
	service := NewService(NewMemoryStore())

	created, err := service.CreateToken(ctx, CreateTokenRequest{
		Name:   "known scope",
		Scopes: []string{ScopeRegisterTunnel},
	})
	if err != nil {
		t.Fatalf("CreateToken returned error: %v", err)
	}

	_, err = service.ValidateToken(ctx, created.Token, "typo_scope")
	if err == nil {
		t.Fatal("ValidateToken returned nil error")
	}
	if !strings.Contains(err.Error(), `unsupported token scope "typo_scope"`) {
		t.Fatalf("error = %q, want unsupported scope guidance", err.Error())
	}
}

func TestServiceRevokesToken(t *testing.T) {
	ctx := context.Background()
	service := NewService(NewMemoryStore())

	created, err := service.CreateToken(ctx, CreateTokenRequest{
		Name:   "to revoke",
		Scopes: []string{ScopeRegisterTunnel},
	})
	if err != nil {
		t.Fatalf("CreateToken returned error: %v", err)
	}
	if err := service.RevokeToken(ctx, created.ID); err != nil {
		t.Fatalf("RevokeToken returned error: %v", err)
	}

	result, err := service.ValidateToken(ctx, created.Token, ScopeRegisterTunnel)
	if err != nil {
		t.Fatalf("ValidateToken returned error: %v", err)
	}
	if result.Valid {
		t.Fatal("revoked token validated")
	}
}

func TestServiceListsTokenSummaries(t *testing.T) {
	ctx := context.Background()
	service := NewService(NewMemoryStore())

	created, err := service.CreateToken(ctx, CreateTokenRequest{
		Name:   "listed",
		Scopes: []string{ScopeRegisterTunnel},
	})
	if err != nil {
		t.Fatalf("CreateToken returned error: %v", err)
	}
	if err := service.RevokeToken(ctx, created.ID); err != nil {
		t.Fatalf("RevokeToken returned error: %v", err)
	}

	listed, err := service.ListTokens(ctx)
	if err != nil {
		t.Fatalf("ListTokens returned error: %v", err)
	}
	if len(listed.Tokens) != 1 {
		t.Fatalf("listed tokens = %d, want 1", len(listed.Tokens))
	}
	summary := listed.Tokens[0]
	if summary.ID != created.ID || summary.Name != "listed" {
		t.Fatalf("summary = %+v, want created token summary", summary)
	}
	if summary.LastUsedAt != nil {
		t.Fatalf("summary last_used_at = %v, want nil", summary.LastUsedAt)
	}
	if summary.RevokedAt == nil {
		t.Fatalf("summary revoked_at = nil, want timestamp")
	}
}

func TestMemoryStoreDoesNotExposePlaintextToken(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	service := NewService(store)

	created, err := service.CreateToken(ctx, CreateTokenRequest{
		Name:   "secret",
		Scopes: []string{ScopeRegisterTunnel},
	})
	if err != nil {
		t.Fatalf("CreateToken returned error: %v", err)
	}

	record, ok, err := store.LookupByID(ctx, created.ID)
	if err != nil {
		t.Fatalf("LookupByID returned error: %v", err)
	}
	if !ok {
		t.Fatal("created token record was not stored")
	}
	if strings.Contains(record.TokenHash, created.Token) {
		t.Fatal("stored token hash contains plaintext token")
	}
}
