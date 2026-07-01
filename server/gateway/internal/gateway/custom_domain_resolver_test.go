// SPDX-License-Identifier: AGPL-3.0-only

package gateway

import (
	"context"
	"testing"
	"time"
)

type countingCustomDomainResolver struct {
	calls  int
	result customDomainResolution
}

func (r *countingCustomDomainResolver) ResolveCustomDomain(context.Context, string) (customDomainResolution, error) {
	r.calls++
	return r.result, nil
}

func TestCachedCustomDomainResolverCachesResultsUntilTTL(t *testing.T) {
	next := &countingCustomDomainResolver{
		result: customDomainResolution{
			Found:     true,
			Hostname:  "preview.example.test",
			Subdomain: "demo",
		},
	}
	resolver := newCachedCustomDomainResolver(next, time.Minute)
	cached, ok := resolver.(*cachedCustomDomainResolver)
	if !ok {
		t.Fatal("newCachedCustomDomainResolver returned non-cached resolver")
	}
	now := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	cached.now = func() time.Time { return now }

	first, err := cached.ResolveCustomDomain(context.Background(), "preview.example.test")
	if err != nil {
		t.Fatalf("first ResolveCustomDomain returned error: %v", err)
	}
	second, err := cached.ResolveCustomDomain(context.Background(), "PREVIEW.EXAMPLE.TEST")
	if err != nil {
		t.Fatalf("second ResolveCustomDomain returned error: %v", err)
	}
	if next.calls != 1 {
		t.Fatalf("calls = %d, want 1", next.calls)
	}
	if first.Subdomain != "demo" || second.Subdomain != "demo" {
		t.Fatalf("results = %+v / %+v, want demo", first, second)
	}

	now = now.Add(2 * time.Minute)
	_, err = cached.ResolveCustomDomain(context.Background(), "preview.example.test")
	if err != nil {
		t.Fatalf("expired ResolveCustomDomain returned error: %v", err)
	}
	if next.calls != 2 {
		t.Fatalf("calls after expiry = %d, want 2", next.calls)
	}
}

func TestCachedCustomDomainResolverCanBeDisabled(t *testing.T) {
	next := &countingCustomDomainResolver{}
	resolver := newCachedCustomDomainResolver(next, 0)
	if resolver != next {
		t.Fatalf("resolver = %#v, want original resolver when ttl is zero", resolver)
	}
}
