// SPDX-License-Identifier: AGPL-3.0-only

package tokens

import (
	"context"
	"strings"
	"testing"
)

func TestServiceCreatesAndValidatesToken(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	service := NewService(store)

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
	service := NewService(NewMemoryStore())

	created, err := service.CreateToken(ctx, CreateTokenRequest{
		Name:   "limited",
		Scopes: []string{ScopeRegisterTunnel},
	})
	if err != nil {
		t.Fatalf("CreateToken returned error: %v", err)
	}

	result, err := service.ValidateToken(ctx, created.Token, "reserve_subdomain")
	if err != nil {
		t.Fatalf("ValidateToken returned error: %v", err)
	}
	if result.Valid {
		t.Fatal("token validated for a missing scope")
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
