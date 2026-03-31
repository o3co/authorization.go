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
	"fmt"
	"testing"
	"time"
)

// Verify basic Set and Get.
func TestInMemoryCache_SetGet(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cache := NewInMemoryCache(ctx, 1*time.Minute)
	result := &IntrospectionResult{Subject: "user-1"}

	cache.Set("key1", result)

	got, ok := cache.Get("key1")
	if !ok {
		t.Fatal("expected cache hit")
	}
	if got.Subject != "user-1" {
		t.Errorf("Subject = %q, want %q", got.Subject, "user-1")
	}
}

// Verify that expired entries return a miss.
func TestInMemoryCache_Expiry(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cache := NewInMemoryCache(ctx, 1*time.Millisecond)
	cache.Set("key1", &IntrospectionResult{Subject: "user-1"})

	time.Sleep(5 * time.Millisecond)

	_, ok := cache.Get("key1")
	if ok {
		t.Error("expected cache miss after expiry")
	}
}

// Verify that Get returns miss for unknown keys.
func TestInMemoryCache_Miss(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cache := NewInMemoryCache(ctx, 1*time.Minute)

	_, ok := cache.Get("nonexistent")
	if ok {
		t.Error("expected cache miss for unknown key")
	}
}

// Verify that ExpiresAt shorter than TTL bounds the cache entry lifetime.
func TestInMemoryCache_ExpiresAtBoundsEntry(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cache := NewInMemoryCache(ctx, 1*time.Minute)
	cache.Set("key1", &IntrospectionResult{
		Subject:   "user-1",
		ExpiresAt: time.Now().Add(1 * time.Millisecond),
	})

	time.Sleep(5 * time.Millisecond)

	_, ok := cache.Get("key1")
	if ok {
		t.Error("expected cache miss after token ExpiresAt")
	}
}

// Verify that keys with different scheme prefixes don't collide.
func TestInMemoryCache_SharedAcrossSchemes(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cache := NewInMemoryCache(ctx, 1*time.Minute)

	cache.Set("bearer:token-abc", &IntrospectionResult{Subject: "bearer-user"})
	cache.Set("basic:token-abc", &IntrospectionResult{Subject: "basic-user"})

	got1, ok := cache.Get("bearer:token-abc")
	if !ok || got1.Subject != "bearer-user" {
		t.Errorf("bearer entry = %v, want bearer-user", got1)
	}

	got2, ok := cache.Get("basic:token-abc")
	if !ok || got2.Subject != "basic-user" {
		t.Errorf("basic entry = %v, want basic-user", got2)
	}
}

// Verify that background sweep removes expired entries.
func TestInMemoryCache_SweepRemovesExpired(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cache := NewInMemoryCache(ctx, 1*time.Millisecond,
		WithSweepInterval(2*time.Millisecond),
	)

	cache.Set("key1", &IntrospectionResult{Subject: "user-1"})

	// Wait for entry to expire and sweep to run
	time.Sleep(10 * time.Millisecond)

	_, ok := cache.Get("key1")
	if ok {
		t.Error("expected cache miss after sweep")
	}
}

// Verify that sweep stops when context is cancelled.
func TestInMemoryCache_SweepStopsOnCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	cache := NewInMemoryCache(ctx, 1*time.Minute,
		WithSweepInterval(1*time.Millisecond),
	)

	cache.Set("key1", &IntrospectionResult{Subject: "user-1"})

	// Cancel context — sweep should stop
	cancel()
	time.Sleep(5 * time.Millisecond)

	// Entry should still be accessible (not expired, sweep stopped)
	got, ok := cache.Get("key1")
	if !ok {
		t.Fatal("expected cache hit — entry is not expired")
	}
	if got.Subject != "user-1" {
		t.Errorf("Subject = %q, want %q", got.Subject, "user-1")
	}
}

// Verify that max entries evicts the entry with earliest expiration.
func TestInMemoryCache_MaxEntries(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cache := NewInMemoryCache(ctx, 1*time.Minute,
		WithMaxEntries(2),
	)

	// Entry 1 expires soonest (short TTL token)
	cache.Set("key1", &IntrospectionResult{
		Subject:   "user-1",
		ExpiresAt: time.Now().Add(1 * time.Second),
	})
	// Entry 2 expires later
	cache.Set("key2", &IntrospectionResult{
		Subject:   "user-2",
		ExpiresAt: time.Now().Add(1 * time.Minute),
	})
	// Entry 3 triggers eviction — key1 should be evicted (earliest expiry)
	cache.Set("key3", &IntrospectionResult{
		Subject:   "user-3",
		ExpiresAt: time.Now().Add(1 * time.Minute),
	})

	_, ok1 := cache.Get("key1")
	if ok1 {
		t.Error("expected key1 to be evicted (earliest expiration)")
	}

	_, ok2 := cache.Get("key2")
	if !ok2 {
		t.Error("expected key2 to still exist")
	}

	_, ok3 := cache.Get("key3")
	if !ok3 {
		t.Error("expected key3 to still exist")
	}
}

// Verify that maxEntries=0 means unlimited (default).
func TestInMemoryCache_MaxEntriesZeroUnlimited(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cache := NewInMemoryCache(ctx, 1*time.Minute)

	for i := 0; i < 100; i++ {
		cache.Set(fmt.Sprintf("key%d", i), &IntrospectionResult{Subject: "user"})
	}

	// All entries should exist
	got, ok := cache.Get("key0")
	if !ok || got.Subject != "user" {
		t.Error("expected all entries to exist with unlimited cache")
	}
}
