// SPDX-License-Identifier: AGPL-3.0-only

package access

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestServiceCreatesAndListsPublicPolicy(t *testing.T) {
	ctx := context.Background()
	service := NewService(NewMemoryStore())
	now := time.Date(2026, 6, 30, 12, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return now }

	created, err := service.CreatePolicy(ctx, CreatePolicyRequest{
		ReservedSubdomainID: "rs_123",
		Mode:                " PUBLIC ",
	})
	if err != nil {
		t.Fatalf("CreatePolicy returned error: %v", err)
	}
	if created.ID == "" {
		t.Fatal("created policy id is empty")
	}
	if created.ReservedSubdomainID != "rs_123" || created.Mode != ModePublic {
		t.Fatalf("created policy = %+v, want rs_123 public", created)
	}
	if created.SecretConfigured {
		t.Fatalf("created policy = %+v, want no configured secret", created)
	}

	listed, err := service.ListPolicies(ctx)
	if err != nil {
		t.Fatalf("ListPolicies returned error: %v", err)
	}
	if len(listed.AccessPolicies) != 1 {
		t.Fatalf("access_policies = %d, want 1", len(listed.AccessPolicies))
	}
	if listed.AccessPolicies[0].ID != created.ID {
		t.Fatalf("listed policy = %+v, want id %q", listed.AccessPolicies[0], created.ID)
	}
}

func TestServiceHashesBasicAuthSecret(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	service := NewService(store)

	created, err := service.CreatePolicy(ctx, CreatePolicyRequest{
		ReservedSubdomainID: "rs_123",
		Mode:                "basic_auth",
		BasicUsername:       "admin",
		BasicPassword:       "secret-password",
	})
	if err != nil {
		t.Fatalf("CreatePolicy returned error: %v", err)
	}
	if !created.SecretConfigured || created.BasicUsername != "admin" {
		t.Fatalf("created policy = %+v, want secret flag and username", created)
	}

	record, ok, err := store.LookupByID(ctx, created.ID)
	if err != nil || !ok {
		t.Fatalf("LookupByID = ok %v err %v, want ok", ok, err)
	}
	if record.SecretHash == "" || record.SecretHash == "secret-password" {
		t.Fatalf("secret hash = %q, want hashed secret", record.SecretHash)
	}
}

func TestServiceNormalizesIPAllowlist(t *testing.T) {
	ctx := context.Background()
	service := NewService(NewMemoryStore())

	created, err := service.CreatePolicy(ctx, CreatePolicyRequest{
		ReservedSubdomainID: "rs_123",
		Mode:                "ip_allowlist",
		IPAllowlist:         []string{" 192.0.2.1 ", "2001:db8::1/64", "192.0.2.1"},
	})
	if err != nil {
		t.Fatalf("CreatePolicy returned error: %v", err)
	}
	want := []string{"192.0.2.1", "2001:db8::/64"}
	if len(created.IPAllowlist) != len(want) {
		t.Fatalf("ip_allowlist = %v, want %v", created.IPAllowlist, want)
	}
	for i := range want {
		if created.IPAllowlist[i] != want[i] {
			t.Fatalf("ip_allowlist = %v, want %v", created.IPAllowlist, want)
		}
	}
}

func TestServiceRejectsInvalidPolicy(t *testing.T) {
	ctx := context.Background()
	service := NewService(NewMemoryStore())

	_, err := service.CreatePolicy(ctx, CreatePolicyRequest{Mode: "public"})
	if !errors.Is(err, ErrPolicyReservedSubdomainIDRequired) {
		t.Fatalf("error = %v, want ErrPolicyReservedSubdomainIDRequired", err)
	}

	_, err = service.CreatePolicy(ctx, CreatePolicyRequest{ReservedSubdomainID: "rs_123", Mode: "unknown"})
	if !errors.Is(err, ErrPolicyModeUnsupported) {
		t.Fatalf("error = %v, want ErrPolicyModeUnsupported", err)
	}

	_, err = service.CreatePolicy(ctx, CreatePolicyRequest{ReservedSubdomainID: "rs_123", Mode: "basic_auth", BasicPassword: "secret"})
	if !errors.Is(err, ErrPolicyBasicUsernameRequired) {
		t.Fatalf("error = %v, want ErrPolicyBasicUsernameRequired", err)
	}

	_, err = service.CreatePolicy(ctx, CreatePolicyRequest{ReservedSubdomainID: "rs_123", Mode: "bearer_token"})
	if !errors.Is(err, ErrPolicySecretRequired) {
		t.Fatalf("error = %v, want ErrPolicySecretRequired", err)
	}

	_, err = service.CreatePolicy(ctx, CreatePolicyRequest{ReservedSubdomainID: "rs_123", Mode: "ip_allowlist", IPAllowlist: []string{"bad"}})
	if err == nil {
		t.Fatal("CreatePolicy returned nil error for invalid IP allowlist")
	}
}

func TestServiceUpdatesAndDeletesPolicy(t *testing.T) {
	ctx := context.Background()
	service := NewService(NewMemoryStore())

	created, err := service.CreatePolicy(ctx, CreatePolicyRequest{
		ReservedSubdomainID: "rs_123",
		Mode:                "public",
	})
	if err != nil {
		t.Fatalf("CreatePolicy returned error: %v", err)
	}

	updated, err := service.UpdatePolicy(ctx, created.ID, UpdatePolicyRequest{
		Mode:        "bearer_token",
		BearerToken: "secret-token",
	})
	if err != nil {
		t.Fatalf("UpdatePolicy returned error: %v", err)
	}
	if updated.Mode != ModeBearerToken || !updated.SecretConfigured {
		t.Fatalf("updated policy = %+v, want bearer token with configured secret", updated)
	}

	updated, err = service.UpdatePolicy(ctx, created.ID, UpdatePolicyRequest{})
	if err != nil {
		t.Fatalf("UpdatePolicy preserving secret returned error: %v", err)
	}
	if updated.Mode != ModeBearerToken || !updated.SecretConfigured {
		t.Fatalf("updated policy = %+v, want preserved bearer token secret", updated)
	}

	if err := service.DeletePolicy(ctx, created.ID); err != nil {
		t.Fatalf("DeletePolicy returned error: %v", err)
	}
	if _, ok, err := service.GetPolicy(ctx, created.ID); err != nil || ok {
		t.Fatalf("GetPolicy after delete = ok %v err %v, want not found", ok, err)
	}
}
