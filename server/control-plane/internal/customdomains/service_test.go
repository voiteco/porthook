// SPDX-License-Identifier: AGPL-3.0-only

package customdomains

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestServiceCreatesAndListsDomain(t *testing.T) {
	ctx := context.Background()
	service := NewService(NewMemoryStore())
	now := time.Date(2026, 7, 1, 10, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return now }

	created, err := service.CreateDomain(ctx, CreateDomainRequest{
		Hostname:            " Preview.Example.TEST. ",
		ReservedSubdomainID: "rs_123",
	})
	if err != nil {
		t.Fatalf("CreateDomain returned error: %v", err)
	}
	if created.ID == "" {
		t.Fatal("created domain id is empty")
	}
	if created.Hostname != "preview.example.test" {
		t.Fatalf("hostname = %q, want preview.example.test", created.Hostname)
	}
	if created.ReservedSubdomainID != "rs_123" || created.Status != StatusPendingVerification {
		t.Fatalf("created domain = %+v, want rs_123 pending verification", created)
	}
	if created.VerificationToken == "" {
		t.Fatal("verification token is empty")
	}
	if created.VerificationName != "_porthook.preview.example.test" {
		t.Fatalf("verification name = %q, want _porthook.preview.example.test", created.VerificationName)
	}
	if !created.CreatedAt.Equal(now) || !created.UpdatedAt.Equal(now) {
		t.Fatalf("created timestamps = %s/%s, want %s", created.CreatedAt, created.UpdatedAt, now)
	}

	listed, err := service.ListDomains(ctx)
	if err != nil {
		t.Fatalf("ListDomains returned error: %v", err)
	}
	if len(listed.CustomDomains) != 1 {
		t.Fatalf("custom_domains = %d, want 1", len(listed.CustomDomains))
	}
	if listed.CustomDomains[0].Hostname != "preview.example.test" {
		t.Fatalf("listed domain = %+v, want preview.example.test", listed.CustomDomains[0])
	}
	if listed.CustomDomains[0].VerificationToken != created.VerificationToken {
		t.Fatalf("listed verification token = %q, want %q", listed.CustomDomains[0].VerificationToken, created.VerificationToken)
	}
}

func TestServiceVerifiesDomainWithMatchingTXT(t *testing.T) {
	ctx := context.Background()
	resolver := fakeTXTResolver{values: map[string][]string{}}
	service := NewServiceWithResolver(NewMemoryStore(), resolver)
	createdAt := time.Date(2026, 7, 1, 10, 0, 0, 0, time.UTC)
	verifiedAt := time.Date(2026, 7, 1, 10, 5, 0, 0, time.UTC)
	service.now = func() time.Time { return createdAt }

	created, err := service.CreateDomain(ctx, CreateDomainRequest{
		Hostname:            "demo.example.test",
		ReservedSubdomainID: "rs_123",
	})
	if err != nil {
		t.Fatalf("CreateDomain returned error: %v", err)
	}
	resolver.values[created.VerificationName] = []string{"porthook-domain-verification=" + created.VerificationToken}
	service.now = func() time.Time { return verifiedAt }

	verified, err := service.VerifyDomain(ctx, created.ID)
	if err != nil {
		t.Fatalf("VerifyDomain returned error: %v", err)
	}
	if verified.Status != StatusActive {
		t.Fatalf("verified status = %q, want active", verified.Status)
	}
	if verified.VerifiedAt == nil || !verified.VerifiedAt.Equal(verifiedAt) {
		t.Fatalf("verified_at = %v, want %s", verified.VerifiedAt, verifiedAt)
	}
	if !verified.UpdatedAt.Equal(verifiedAt) {
		t.Fatalf("updated_at = %s, want %s", verified.UpdatedAt, verifiedAt)
	}
}

func TestServiceMarksVerificationFailedWithoutMatchingTXT(t *testing.T) {
	ctx := context.Background()
	resolver := fakeTXTResolver{values: map[string][]string{}}
	service := NewServiceWithResolver(NewMemoryStore(), resolver)
	created, err := service.CreateDomain(ctx, CreateDomainRequest{
		Hostname:            "demo.example.test",
		ReservedSubdomainID: "rs_123",
	})
	if err != nil {
		t.Fatalf("CreateDomain returned error: %v", err)
	}
	resolver.values[created.VerificationName] = []string{"wrong-token"}

	verified, err := service.VerifyDomain(ctx, created.ID)
	if err != nil {
		t.Fatalf("VerifyDomain returned error: %v", err)
	}
	if verified.Status != StatusVerificationFailed {
		t.Fatalf("verified status = %q, want verification_failed", verified.Status)
	}
	if verified.VerifiedAt != nil {
		t.Fatalf("verified_at = %v, want nil", verified.VerifiedAt)
	}
}

func TestServiceRejectsInvalidDomains(t *testing.T) {
	ctx := context.Background()
	service := NewService(NewMemoryStore())

	_, err := service.CreateDomain(ctx, CreateDomainRequest{Hostname: "demo.example.test"})
	if !errors.Is(err, ErrDomainReservedSubdomainIDRequired) {
		t.Fatalf("error = %v, want ErrDomainReservedSubdomainIDRequired", err)
	}

	for _, hostname := range []string{"", "localhost", "bad_name.example.test", "-bad.example.test", "bad-.example.test", "bad..example.test", "*.example.test", "example.test:443"} {
		t.Run(hostname, func(t *testing.T) {
			_, err := service.CreateDomain(ctx, CreateDomainRequest{
				Hostname:            hostname,
				ReservedSubdomainID: "rs_123",
			})
			if err == nil {
				t.Fatal("CreateDomain returned nil error")
			}
		})
	}
}

func TestServiceRejectsDuplicateDomain(t *testing.T) {
	ctx := context.Background()
	service := NewService(NewMemoryStore())

	_, err := service.CreateDomain(ctx, CreateDomainRequest{
		Hostname:            "demo.example.test",
		ReservedSubdomainID: "rs_123",
	})
	if err != nil {
		t.Fatalf("CreateDomain returned error: %v", err)
	}
	_, err = service.CreateDomain(ctx, CreateDomainRequest{
		Hostname:            "DEMO.EXAMPLE.TEST",
		ReservedSubdomainID: "rs_456",
	})
	if !errors.Is(err, ErrDomainAlreadyExists) {
		t.Fatalf("error = %v, want ErrDomainAlreadyExists", err)
	}
}

func TestServiceGetsDomainByIDAndHostname(t *testing.T) {
	ctx := context.Background()
	service := NewService(NewMemoryStore())

	created, err := service.CreateDomain(ctx, CreateDomainRequest{
		Hostname:            "demo.example.test",
		ReservedSubdomainID: "rs_123",
	})
	if err != nil {
		t.Fatalf("CreateDomain returned error: %v", err)
	}

	byID, ok, err := service.GetDomain(ctx, created.ID)
	if err != nil || !ok {
		t.Fatalf("GetDomain = ok %v err %v, want ok", ok, err)
	}
	if byID.Hostname != "demo.example.test" {
		t.Fatalf("byID = %+v, want demo.example.test", byID)
	}

	byHostname, ok, err := service.GetDomainByHostname(ctx, " Demo.Example.Test. ")
	if err != nil || !ok {
		t.Fatalf("GetDomainByHostname = ok %v err %v, want ok", ok, err)
	}
	if byHostname.ID != created.ID {
		t.Fatalf("byHostname.ID = %q, want %q", byHostname.ID, created.ID)
	}
}

func TestServiceDeletesDomain(t *testing.T) {
	ctx := context.Background()
	service := NewService(NewMemoryStore())

	created, err := service.CreateDomain(ctx, CreateDomainRequest{
		Hostname:            "demo.example.test",
		ReservedSubdomainID: "rs_123",
	})
	if err != nil {
		t.Fatalf("CreateDomain returned error: %v", err)
	}
	if err := service.DeleteDomain(ctx, created.ID); err != nil {
		t.Fatalf("DeleteDomain returned error: %v", err)
	}
	if _, ok, err := service.GetDomain(ctx, created.ID); err != nil || ok {
		t.Fatalf("GetDomain after delete = ok %v err %v, want not found", ok, err)
	}
	if err := service.DeleteDomain(ctx, created.ID); !errors.Is(err, ErrDomainNotFound) {
		t.Fatalf("DeleteDomain second error = %v, want ErrDomainNotFound", err)
	}
}

func TestNormalizeHostnameGuidance(t *testing.T) {
	_, err := NormalizeHostname("bad_name.example.test")
	if err == nil {
		t.Fatal("NormalizeHostname returned nil error")
	}
	if !strings.Contains(err.Error(), "lowercase ASCII") {
		t.Fatalf("error = %q, want hostname validation guidance", err.Error())
	}
}

type fakeTXTResolver struct {
	values map[string][]string
	err    error
}

func (r fakeTXTResolver) LookupTXT(_ context.Context, name string) ([]string, error) {
	if r.err != nil {
		return nil, r.err
	}
	return r.values[name], nil
}
