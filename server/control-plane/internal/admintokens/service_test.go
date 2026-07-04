// SPDX-License-Identifier: AGPL-3.0-only

package admintokens

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestServiceCreatesAndValidatesAdminToken(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	service := NewService(store)
	createdAt := time.Date(2026, 7, 4, 9, 0, 0, 0, time.UTC)
	usedAt := createdAt.Add(15 * time.Minute)
	now := createdAt
	service.now = func() time.Time { return now }

	created, err := service.CreateToken(ctx, CreateTokenRequest{
		Name:   "operator",
		Scopes: []string{ScopeTokens, ScopeAuditHistory},
	})
	if err != nil {
		t.Fatalf("CreateToken returned error: %v", err)
	}
	if created.ID == "" || !strings.HasPrefix(created.ID, "adm_") {
		t.Fatalf("created id = %q, want adm_ prefix", created.ID)
	}
	if !strings.HasPrefix(created.Token, "pat_") {
		t.Fatalf("token = %q, want pat_ prefix", created.Token)
	}

	now = usedAt
	result, err := service.ValidateToken(ctx, created.Token, ScopeTokens)
	if err != nil {
		t.Fatalf("ValidateToken returned error: %v", err)
	}
	if !result.Valid || result.TokenID != created.ID || result.Name != "operator" {
		t.Fatalf("validation result = %+v, want valid created token", result)
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

func TestServiceDefaultsToAllScopes(t *testing.T) {
	ctx := context.Background()
	service := NewService(NewMemoryStore())

	created, err := service.CreateToken(ctx, CreateTokenRequest{Name: "admin"})
	if err != nil {
		t.Fatalf("CreateToken returned error: %v", err)
	}
	if len(created.Scopes) != len(KnownScopes()) {
		t.Fatalf("scopes = %v, want all known scopes", created.Scopes)
	}
	for _, scope := range KnownScopes() {
		result, err := service.ValidateToken(ctx, created.Token, scope)
		if err != nil {
			t.Fatalf("ValidateToken(%q) returned error: %v", scope, err)
		}
		if !result.Valid {
			t.Fatalf("created token did not validate for default scope %q", scope)
		}
	}
}

func TestServiceEnforcesScopes(t *testing.T) {
	ctx := context.Background()
	service := NewService(NewMemoryStore())
	created, err := service.CreateToken(ctx, CreateTokenRequest{
		Name:   "readonly",
		Scopes: []string{ScopeAuditHistory},
	})
	if err != nil {
		t.Fatalf("CreateToken returned error: %v", err)
	}

	result, err := service.ValidateToken(ctx, created.Token, ScopeTokens)
	if err != nil {
		t.Fatalf("ValidateToken returned error: %v", err)
	}
	if result.Valid {
		t.Fatal("token validated for a missing scope")
	}
}

func TestServiceRejectsUnsupportedScope(t *testing.T) {
	ctx := context.Background()
	service := NewService(NewMemoryStore())

	_, err := service.CreateToken(ctx, CreateTokenRequest{
		Name:   "bad",
		Scopes: []string{"billing"},
	})
	if err == nil {
		t.Fatal("CreateToken returned nil error")
	}
	if !strings.Contains(err.Error(), `unsupported admin token scope "billing"`) {
		t.Fatalf("error = %q, want unsupported scope guidance", err.Error())
	}
}

func TestServiceRevokesAdminToken(t *testing.T) {
	ctx := context.Background()
	service := NewService(NewMemoryStore())
	created, err := service.CreateToken(ctx, CreateTokenRequest{Name: "revoked"})
	if err != nil {
		t.Fatalf("CreateToken returned error: %v", err)
	}
	if err := service.RevokeToken(ctx, created.ID); err != nil {
		t.Fatalf("RevokeToken returned error: %v", err)
	}
	result, err := service.ValidateToken(ctx, created.Token, ScopeTokens)
	if err != nil {
		t.Fatalf("ValidateToken returned error: %v", err)
	}
	if result.Valid {
		t.Fatal("revoked admin token validated")
	}
}

func TestMemoryStoreDoesNotExposePlaintextToken(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	service := NewService(store)
	created, err := service.CreateToken(ctx, CreateTokenRequest{Name: "secret"})
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
		t.Fatal("stored hash contains plaintext token")
	}
}
