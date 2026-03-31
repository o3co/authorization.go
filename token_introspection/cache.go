// Copyright 2026 1o1 Co. Ltd.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package tokenintrospection

import (
	"context"
	"sync"
	"time"
)

// Cache is the interface for introspection result caching.
// Implementations must be safe for concurrent use by multiple goroutines,
// as interceptors are invoked concurrently across RPCs.
type Cache interface {
	Get(key string) (*IntrospectionResult, bool)
	Set(key string, result *IntrospectionResult)
}

type cacheEntry struct {
	result    *IntrospectionResult
	expiresAt time.Time
}

type inMemoryCacheConfig struct {
	maxEntries    int
	sweepInterval time.Duration
}

// InMemoryCacheOption configures the in-memory cache.
type InMemoryCacheOption func(*inMemoryCacheConfig)

// WithMaxEntries sets the maximum number of entries in the cache.
// When exceeded on Set, the entry with the earliest expiration is evicted.
// Default: 0 (unlimited).
func WithMaxEntries(n int) InMemoryCacheOption {
	if n < 0 {
		n = 0
	}
	return func(c *inMemoryCacheConfig) {
		c.maxEntries = n
	}
}

// WithSweepInterval sets the interval for the background goroutine that
// removes expired entries. Default: same as TTL.
func WithSweepInterval(d time.Duration) InMemoryCacheOption {
	return func(c *inMemoryCacheConfig) {
		c.sweepInterval = d
	}
}

type inMemoryCache struct {
	ttl        time.Duration
	maxEntries int
	entries    sync.Map
}

// NewInMemoryCache creates an in-memory cache with the given TTL.
// A background goroutine periodically removes expired entries. It stops
// when ctx is cancelled.
func NewInMemoryCache(ctx context.Context, ttl time.Duration, opts ...InMemoryCacheOption) Cache {
	cfg := &inMemoryCacheConfig{
		sweepInterval: ttl,
	}
	for _, opt := range opts {
		opt(cfg)
	}

	c := &inMemoryCache{
		ttl:        ttl,
		maxEntries: cfg.maxEntries,
	}

	if cfg.sweepInterval > 0 {
		go c.sweepLoop(ctx, cfg.sweepInterval)
	}

	return c
}

func (c *inMemoryCache) sweepLoop(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			now := time.Now()
			c.entries.Range(func(key, value any) bool {
				entry := value.(*cacheEntry)
				if now.After(entry.expiresAt) {
					c.entries.Delete(key)
				}
				return true
			})
		}
	}
}

func (c *inMemoryCache) Get(key string) (*IntrospectionResult, bool) {
	v, ok := c.entries.Load(key)
	if !ok {
		return nil, false
	}
	entry := v.(*cacheEntry)
	if time.Now().After(entry.expiresAt) {
		c.entries.Delete(key)
		return nil, false
	}
	return entry.result, true
}

func (c *inMemoryCache) Set(key string, result *IntrospectionResult) {
	expiry := time.Now().Add(c.ttl)
	// If the token has an explicit expiration earlier than the cache TTL,
	// use the token expiration to avoid caching beyond token validity.
	if !result.ExpiresAt.IsZero() && result.ExpiresAt.Before(expiry) {
		expiry = result.ExpiresAt
	}

	c.entries.Store(key, &cacheEntry{
		result:    result,
		expiresAt: expiry,
	})

	// Evict if over max entries
	if c.maxEntries > 0 {
		c.evictIfNeeded()
	}
}

func (c *inMemoryCache) evictIfNeeded() {
	var count int
	c.entries.Range(func(_, _ any) bool {
		count++
		return true
	})

	for count > c.maxEntries {
		var oldestKey any
		var oldestExpiry time.Time

		c.entries.Range(func(key, value any) bool {
			entry := value.(*cacheEntry)
			if oldestKey == nil || entry.expiresAt.Before(oldestExpiry) {
				oldestKey = key
				oldestExpiry = entry.expiresAt
			}
			return true
		})

		if oldestKey != nil {
			c.entries.Delete(oldestKey)
		}
		count--
	}
}
