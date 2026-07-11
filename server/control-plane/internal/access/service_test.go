// SPDX-License-Identifier: AGPL-3.0-only

package access

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
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
	if !IsPasswordHash(record.SecretHash) {
		t.Fatalf("secret hash = %q, want a versioned Argon2id password hash", record.SecretHash)
	}
}

func TestServiceKeepsFastHashForBearerTokens(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	service := NewService(store)

	created, err := service.CreatePolicy(ctx, CreatePolicyRequest{
		ReservedSubdomainID: "rs_bearer",
		Mode:                "bearer_token",
		BearerToken:         "high-entropy-generated-token",
	})
	if err != nil {
		t.Fatalf("CreatePolicy returned error: %v", err)
	}

	record, ok, err := store.LookupByID(ctx, created.ID)
	if err != nil || !ok {
		t.Fatalf("LookupByID = ok %v err %v, want ok", ok, err)
	}
	if record.SecretHash != HashSecret("high-entropy-generated-token") {
		t.Fatalf("secret hash = %q, want fast SHA-256 lookup hash for bearer tokens", record.SecretHash)
	}
	if IsPasswordHash(record.SecretHash) {
		t.Fatal("bearer token hash used the memory-hard Argon2id scheme, want fast SHA-256")
	}
}

func TestServiceUpgradesLegacyBasicAuthHashOnSuccess(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	service := NewService(store)
	now := time.Date(2026, 6, 30, 12, 0, 0, 0, time.UTC)

	legacyRecord := PolicyRecord{
		ID:                  "ap_legacy",
		ReservedSubdomainID: "rs_legacy",
		Mode:                ModeBasicAuth,
		BasicUsername:       "admin",
		SecretHash:          HashSecret("legacy-password"),
		CreatedAt:           now,
		UpdatedAt:           now,
	}
	if err := store.Create(ctx, legacyRecord); err != nil {
		t.Fatalf("store.Create returned error: %v", err)
	}

	wrongResult, err := service.CheckPolicy(ctx, CheckPolicyRequest{
		ReservedSubdomainID: "rs_legacy",
		BasicUsername:       "admin",
		BasicPassword:       "wrong-password",
	})
	if err != nil {
		t.Fatalf("CheckPolicy wrong password returned error: %v", err)
	}
	if wrongResult.Allowed {
		t.Fatal("CheckPolicy allowed an incorrect password against a legacy hash")
	}
	unchanged, _, err := store.LookupByID(ctx, "ap_legacy")
	if err != nil {
		t.Fatalf("LookupByID returned error: %v", err)
	}
	if unchanged.SecretHash != legacyRecord.SecretHash {
		t.Fatal("a failed authentication attempt upgraded the legacy hash")
	}

	allowedResult, err := service.CheckPolicy(ctx, CheckPolicyRequest{
		ReservedSubdomainID: "rs_legacy",
		BasicUsername:       "admin",
		BasicPassword:       "legacy-password",
	})
	if err != nil {
		t.Fatalf("CheckPolicy correct password returned error: %v", err)
	}
	if !allowedResult.Allowed {
		t.Fatal("CheckPolicy denied a correct password against a legacy hash")
	}

	upgraded, ok, err := store.LookupByID(ctx, "ap_legacy")
	if err != nil || !ok {
		t.Fatalf("LookupByID = ok %v err %v, want ok", ok, err)
	}
	if !IsPasswordHash(upgraded.SecretHash) {
		t.Fatalf("secret hash after successful legacy auth = %q, want an upgraded Argon2id hash", upgraded.SecretHash)
	}
	if upgraded.SecretHash == legacyRecord.SecretHash {
		t.Fatal("secret hash was not upgraded after a successful legacy authentication")
	}

	secondResult, err := service.CheckPolicy(ctx, CheckPolicyRequest{
		ReservedSubdomainID: "rs_legacy",
		BasicUsername:       "admin",
		BasicPassword:       "legacy-password",
	})
	if err != nil {
		t.Fatalf("CheckPolicy after upgrade returned error: %v", err)
	}
	if !secondResult.Allowed {
		t.Fatal("CheckPolicy denied a correct password after the hash was upgraded")
	}
}

func TestPolicySummaryNeverExposesSecrets(t *testing.T) {
	ctx := context.Background()
	service := NewService(NewMemoryStore())

	cases := []CreatePolicyRequest{
		{ReservedSubdomainID: "rs_basic", Mode: "basic_auth", BasicUsername: "admin", BasicPassword: "correct-horse-battery-staple"},
		{ReservedSubdomainID: "rs_bearer", Mode: "bearer_token", BearerToken: "highly-secret-bearer-token"},
	}
	for _, req := range cases {
		created, err := service.CreatePolicy(ctx, req)
		if err != nil {
			t.Fatalf("CreatePolicy(%q) returned error: %v", req.Mode, err)
		}

		encoded, err := json.Marshal(created)
		if err != nil {
			t.Fatalf("Marshal returned error: %v", err)
		}
		body := string(encoded)
		if req.BasicPassword != "" && strings.Contains(body, req.BasicPassword) {
			t.Fatalf("policy summary JSON leaked the plaintext password: %s", body)
		}
		if req.BearerToken != "" && strings.Contains(body, req.BearerToken) {
			t.Fatalf("policy summary JSON leaked the plaintext bearer token: %s", body)
		}
		if strings.Contains(body, "secret_hash") || strings.Contains(body, "SecretHash") {
			t.Fatalf("policy summary JSON exposed a secret_hash field: %s", body)
		}

		listed, err := service.ListPolicies(ctx)
		if err != nil {
			t.Fatalf("ListPolicies returned error: %v", err)
		}
		listedEncoded, err := json.Marshal(listed)
		if err != nil {
			t.Fatalf("Marshal returned error: %v", err)
		}
		listedBody := string(listedEncoded)
		if req.BasicPassword != "" && strings.Contains(listedBody, req.BasicPassword) {
			t.Fatalf("policy list JSON leaked the plaintext password: %s", listedBody)
		}
		if req.BearerToken != "" && strings.Contains(listedBody, req.BearerToken) {
			t.Fatalf("policy list JSON leaked the plaintext bearer token: %s", listedBody)
		}
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

func TestServiceChecksAccessPolicies(t *testing.T) {
	ctx := context.Background()
	service := NewService(NewMemoryStore())

	publicResult, err := service.CheckPolicy(ctx, CheckPolicyRequest{ReservedSubdomainID: "rs_missing"})
	if err != nil {
		t.Fatalf("CheckPolicy missing returned error: %v", err)
	}
	if !publicResult.Allowed || publicResult.Mode != ModePublic || publicResult.Reason != "no_policy" {
		t.Fatalf("publicResult = %+v, want public no_policy allow", publicResult)
	}

	basic, err := service.CreatePolicy(ctx, CreatePolicyRequest{
		ReservedSubdomainID: "rs_basic",
		Mode:                "basic_auth",
		BasicUsername:       "admin",
		BasicPassword:       "secret",
	})
	if err != nil {
		t.Fatalf("CreatePolicy basic returned error: %v", err)
	}
	denied, err := service.CheckPolicy(ctx, CheckPolicyRequest{
		ReservedSubdomainID: basic.ReservedSubdomainID,
		BasicUsername:       "admin",
		BasicPassword:       "wrong",
	})
	if err != nil {
		t.Fatalf("CheckPolicy denied returned error: %v", err)
	}
	if denied.Allowed || denied.Reason != "basic_auth_required" {
		t.Fatalf("denied = %+v, want basic_auth_required", denied)
	}
	allowed, err := service.CheckPolicy(ctx, CheckPolicyRequest{
		ReservedSubdomainID: basic.ReservedSubdomainID,
		BasicUsername:       "admin",
		BasicPassword:       "secret",
	})
	if err != nil {
		t.Fatalf("CheckPolicy allowed returned error: %v", err)
	}
	if !allowed.Allowed || allowed.Mode != ModeBasicAuth {
		t.Fatalf("allowed = %+v, want basic auth allow", allowed)
	}

	ipPolicy, err := service.CreatePolicy(ctx, CreatePolicyRequest{
		ReservedSubdomainID: "rs_ip",
		Mode:                "ip_allowlist",
		IPAllowlist:         []string{"192.0.2.0/24"},
	})
	if err != nil {
		t.Fatalf("CreatePolicy ip returned error: %v", err)
	}
	allowed, err = service.CheckPolicy(ctx, CheckPolicyRequest{
		ReservedSubdomainID: ipPolicy.ReservedSubdomainID,
		RemoteIP:            "192.0.2.5",
	})
	if err != nil {
		t.Fatalf("CheckPolicy ip allowed returned error: %v", err)
	}
	if !allowed.Allowed {
		t.Fatalf("allowed = %+v, want IP allow", allowed)
	}
}
