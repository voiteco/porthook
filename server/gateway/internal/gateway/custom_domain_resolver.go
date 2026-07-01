// SPDX-License-Identifier: AGPL-3.0-only

package gateway

import (
	"context"
	"strings"
	"sync"
	"time"
)

type cachedCustomDomainResolver struct {
	next  customDomainResolver
	ttl   time.Duration
	now   func() time.Time
	mu    sync.RWMutex
	cache map[string]customDomainCacheEntry
}

type customDomainCacheEntry struct {
	result  customDomainResolution
	expires time.Time
}

func newCachedCustomDomainResolver(next customDomainResolver, ttl time.Duration) customDomainResolver {
	if ttl == 0 {
		return next
	}
	if ttl < 0 {
		ttl = defaultCustomDomainCacheTTL
	}
	return &cachedCustomDomainResolver{
		next:  next,
		ttl:   ttl,
		now:   func() time.Time { return time.Now().UTC() },
		cache: make(map[string]customDomainCacheEntry),
	}
}

func (r *cachedCustomDomainResolver) ResolveCustomDomain(ctx context.Context, hostname string) (customDomainResolution, error) {
	key := strings.ToLower(strings.TrimSpace(hostname))
	if key == "" {
		return r.next.ResolveCustomDomain(ctx, hostname)
	}
	now := r.now()

	r.mu.RLock()
	entry, ok := r.cache[key]
	r.mu.RUnlock()
	if ok && now.Before(entry.expires) {
		return entry.result, nil
	}

	result, err := r.next.ResolveCustomDomain(ctx, hostname)
	if err != nil {
		return customDomainResolution{}, err
	}

	r.mu.Lock()
	r.cache[key] = customDomainCacheEntry{
		result:  result,
		expires: now.Add(r.ttl),
	}
	r.mu.Unlock()
	return result, nil
}
