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
	"sync"
	"time"
)

// Cache is the interface for introspection result caching.
type Cache interface {
	Get(key string) (*IntrospectionResult, bool)
	Set(key string, result *IntrospectionResult)
}

type cacheEntry struct {
	result    *IntrospectionResult
	expiresAt time.Time
}

type inMemoryCache struct {
	ttl     time.Duration
	entries sync.Map
}

// NewInMemoryCache creates an in-memory cache with the given TTL.
// Expired entries are lazily evicted on access.
func NewInMemoryCache(ttl time.Duration) Cache {
	return &inMemoryCache{ttl: ttl}
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
	c.entries.Store(key, &cacheEntry{
		result:    result,
		expiresAt: time.Now().Add(c.ttl),
	})
}
